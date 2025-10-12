package client

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/protos"
)

func (c *Client) Init(name string, public bool) (repo int32, change int64, err error) {
	initRequest := &protos.InitRequest{
		Auth:     c.GetAuth(),
		RepoName: name,
		Public:   public,
	}
	initResponse, e := c.Pogo.Init(
		c.ctx,
		initRequest,
	)
	if e != nil {
		err = errors.Join(errors.New("open init stream"), e)
		return
	}

	c.repoId = initResponse.RepoId
	c.changeId = initResponse.ChangeId

	repo = initResponse.RepoId
	change = initResponse.ChangeId
	return
}

func (c *Client) PushFull(force bool) error {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	// First, collect all file hashes
	fmt.Fprintln(c.VerboseOut, "Collecting file hashes...")
	type fileInfo struct {
		file       LocalFile
		hash       []byte
		executable *bool
	}
	var files []fileInfo
	var allHashes [][]byte

	for file := range c.UnignoredFiles {
		hash, err := filecontents.HashFile(file.AbsPath)
		if err != nil {
			return errors.Join(fmt.Errorf("hash file %s", file.Name), err)
		}
		files = append(files, fileInfo{
			file:       file,
			hash:       hash,
			executable: IsExecutable(file.AbsPath),
		})
		allHashes = append(allHashes, hash)
	}

	// Check which files are needed by the server
	fmt.Fprintf(c.VerboseOut, "Checking which of %d files need to be uploaded...\n", len(files))
	checkResp, err := c.Pogo.CheckNeededFiles(ctx, &protos.CheckNeededFilesRequest{
		Auth:       c.GetAuth(),
		RepoId:     c.repoId,
		FileHashes: allHashes,
	})
	if err != nil {
		return errors.Join(errors.New("check needed files"), err)
	}

	// Create a set of needed hashes for quick lookup
	neededHashes := make(map[string]bool)
	for _, hash := range checkResp.NeededHashes {
		hashStr := base64.URLEncoding.EncodeToString(hash)
		neededHashes[hashStr] = true
	}
	fmt.Fprintf(c.VerboseOut, "Server needs %d new files\n", len(neededHashes))

	// Now push with the optimized protocol
	stream, err := c.Pogo.PushFull(ctx)
	if err != nil {
		return errors.Join(errors.New("open push full stream"), err)
	}
	defer stream.CloseSend()

	if err := stream.Send(&protos.PushFullRequest{
		Payload: &protos.PushFullRequest_Auth{
			Auth: c.GetAuth(),
		},
	}); err != nil {
		return errors.Join(errors.New("send auth"), err)
	}

	if err := stream.Send(&protos.PushFullRequest{
		Payload: &protos.PushFullRequest_ChangeId{
			ChangeId: c.changeId,
		},
	}); err != nil {
		return errors.Join(errors.New("send change id"), err)
	}

	if err := stream.Send(&protos.PushFullRequest{
		Payload: &protos.PushFullRequest_Force{
			Force: force,
		},
	}); err != nil {
		return errors.Join(errors.New("send force flag"), err)
	}

	// Send all files with their metadata
	for _, fileInfo := range files {
		hashStr := base64.URLEncoding.EncodeToString(fileInfo.hash)
		needsContent := neededHashes[hashStr]

		if needsContent {
			fmt.Fprintf(c.VerboseOut, "Pushing new file: %s\n", fileInfo.file.Name)
		} else {
			fmt.Fprintf(c.VerboseOut, "Skipping existing file: %s\n", fileInfo.file.Name)
		}

		// Send file header with hash
		if err := stream.Send(&protos.PushFullRequest{
			Payload: &protos.PushFullRequest_FileHeader{
				&protos.FileHeader{
					Name:        fileInfo.file.Name,
					Executable:  fileInfo.executable,
					ContentHash: fileInfo.hash,
				},
			},
		}); err != nil {
			return errors.Join(fmt.Errorf("send file %s header", fileInfo.file.Name), err)
		}

		// Send has_content flag
		if err := stream.Send(&protos.PushFullRequest{
			Payload: &protos.PushFullRequest_HasContent{
				HasContent: needsContent,
			},
		}); err != nil {
			return errors.Join(fmt.Errorf("send file %s has_content", fileInfo.file.Name), err)
		}

		// Only send content if needed
		if needsContent {
			f, err := fileInfo.file.Open()
			if err != nil {
				return errors.Join(fmt.Errorf("open file %s", fileInfo.file.Name), err)
			}
			defer f.Close()

			if _, err := io.Copy(&PushFull_StreamWriter{stream}, f); err != nil {
				return errors.Join(fmt.Errorf("send file %s content", fileInfo.file.Name), err)
			}

			// Send EOF after content
			if err := stream.Send(&protos.PushFullRequest{
				Payload: &protos.PushFullRequest_Eof{
					&protos.EOF{},
				},
			}); err != nil {
				return errors.Join(fmt.Errorf("send file %s eof", fileInfo.file.Name), err)
			}
		}
	}

	// Send end of files
	if err := stream.Send(&protos.PushFullRequest{
		Payload: &protos.PushFullRequest_EndOfFiles{
			EndOfFiles: &protos.EndOfFiles{},
		},
	}); err != nil {
		return errors.Join(errors.New("send end of files"), err)
	}

	// Wait for response
	fmt.Fprintln(c.VerboseOut, "Waiting for response")
	_, err = stream.CloseAndRecv()
	if err != nil {
		return errors.Join(errors.New("recv response"), err)
	}

	return nil
}

func (c *Client) SetBookmark(bookmarkName string, changeName *string) error {
	request := &protos.SetBookmarkRequest{
		Auth:         c.GetAuth(),
		RepoId:       c.repoId,
		BookmarkName: bookmarkName,
		ChangeName:   changeName,
	}
	if changeName == nil {
		request.CheckedOutChangeId = &c.changeId
	}

	_, err := c.Pogo.SetBookmark(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("set bookmark"), err)
	}

	return nil
}

func (c *Client) RemoveBookmark(bookmarkName string) error {
	request := &protos.RemoveBookmarkRequest{
		Auth:         c.GetAuth(),
		RepoId:       c.repoId,
		BookmarkName: bookmarkName,
	}

	_, err := c.Pogo.RemoveBookmark(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("remove bookmark"), err)
	}

	return nil
}

func (c *Client) GetBookmarks() ([]*protos.Bookmark, error) {
	request := &protos.GetBookmarksRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
	}

	response, err := c.Pogo.GetBookmarks(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get bookmarks"), err)
	}

	return response.Bookmarks, nil
}

func (c *Client) NewChange(description *string, parentChangeNames []string) (changeId int64, changeName string, err error) {
	request := &protos.NewChangeRequest{
		Auth:              c.GetAuth(),
		RepoId:            c.repoId,
		Description:       description,
		ParentChangeNames: parentChangeNames,
	}

	// If no parent change names provided, use current checked out change
	if len(parentChangeNames) == 0 {
		request.CheckedOutChangeId = &c.changeId
	}

	response, e := c.Pogo.NewChange(c.ctx, request)
	if e != nil {
		err = errors.Join(errors.New("new change"), e)
		return
	}

	changeId = response.ChangeId
	changeName = response.ChangeName
	return
}

func (c *Client) GetDescription() (*string, error) {
	request := &protos.GetDescriptionRequest{
		Auth:     c.GetAuth(),
		ChangeId: c.changeId,
	}

	response, err := c.Pogo.GetDescription(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get description"), err)
	}
	return response.Description, nil
}

func (c *Client) SetDescription(description string) error {
	request := &protos.SetDescriptionRequest{
		Auth:        c.GetAuth(),
		ChangeId:    c.changeId,
		Description: description,
	}

	_, err := c.Pogo.SetDescription(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("set description"), err)
	}

	return nil
}

func (c *Client) Log(maxChanges int32, coloredOutput bool) (string, error) {
	request := &protos.LogRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.repoId,
		CheckedOutChangeId: c.changeId,
		MaxChanges:         maxChanges,
	}

	response, err := c.Pogo.Log(c.ctx, request)
	if err != nil {
		return "", errors.Join(errors.New("get log"), err)
	}

	return RenderLog(response, coloredOutput), nil
}

func (c *Client) LogJSON(maxChanges int32) (string, error) {
	request := &protos.LogRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.repoId,
		CheckedOutChangeId: c.changeId,
		MaxChanges:         maxChanges,
	}

	response, err := c.Pogo.Log(c.ctx, request)
	if err != nil {
		return "", errors.Join(errors.New("get log"), err)
	}

	return RenderLogAsJSON(response)
}

func (c *Client) Info() (*protos.InfoResponse, error) {
	request := &protos.InfoRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.repoId,
		CheckedOutChangeId: c.changeId,
	}

	response, err := c.Pogo.Info(c.ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get info: %w", err)
	}

	return response, nil
}

func (c *Client) Edit(revision string) error {

	// Collect client files
	var clientFiles []string
	for file := range c.UnignoredFiles {
		clientFiles = append(clientFiles, file.Name)
	}

	request := &protos.EditRequest{
		Auth:        c.GetAuth(),
		RepoId:      c.repoId,
		Revision:    revision,
		ClientFiles: clientFiles,
	}

	stream, err := c.Pogo.Edit(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("open edit stream"), err)
	}

	var changeId int64
	var currentFile *os.File
	var currentFileName string
	var currentFileExecutable bool
	var filesDeleted int
	var filesCreated int

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Join(errors.New("recv edit response"), err)
		}

		switch payload := resp.Payload.(type) {
		case *protos.EditResponse_FileToDelete:
			// Delete file from client
			fileName := payload.FileToDelete.Name
			absPath := filepath.Join(c.Location, filepath.FromSlash(fileName))
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				return errors.Join(fmt.Errorf("delete file %s", fileName), err)
			}
			filesDeleted++

		case *protos.EditResponse_FileHeader:
			// Close previous file if open
			if currentFile != nil {
				currentFile.Close()
				currentFile = nil
			}

			// Create new file
			currentFileName = payload.FileHeader.Name
			currentFileExecutable = payload.FileHeader.Executable != nil && *payload.FileHeader.Executable
			absPath := filepath.Join(c.Location, filepath.FromSlash(currentFileName))
			_ = os.MkdirAll(filepath.Dir(absPath), 0755)

			currentFile, err = os.Create(absPath)
			if err != nil {
				return errors.Join(fmt.Errorf("create file %s", currentFileName), err)
			}

		case *protos.EditResponse_FileContent:
			// Write file content
			if currentFile == nil {
				return errors.New("received file content without file header")
			}
			if _, err := currentFile.Write(payload.FileContent); err != nil {
				return errors.Join(fmt.Errorf("write file content %s", currentFileName), err)
			}

		case *protos.EditResponse_Eof:
			// End of current file
			if currentFile != nil {
				currentFile.Close()

				// Set executable permission on UNIX if needed
				if runtime.GOOS != "windows" && currentFileExecutable {
					absPath := filepath.Join(c.Location, filepath.FromSlash(currentFileName))
					if err := os.Chmod(absPath, 0755); err != nil {
						return errors.Join(fmt.Errorf("set executable permission %s", currentFileName), err)
					}
				}

				currentFile = nil
				filesCreated++
			}

		case *protos.EditResponse_EndOfFiles:
			// All files processed
			continue

		case *protos.EditResponse_ChangeId:
			// Store the change ID to update client config
			changeId = payload.ChangeId

		default:
			return fmt.Errorf("unknown edit response payload type: %T", payload)
		}
	}

	// Close any remaining open file
	if currentFile != nil {
		currentFile.Close()
	}

	// Update client config with new change ID
	c.ConfigSetChangeId(changeId)

	return nil
}

func (c *Client) RemoveChange(changeName string, keepChildren bool) error {
	request := &protos.RemoveChangeRequest{
		Auth:         c.GetAuth(),
		RepoId:       c.repoId,
		ChangeName:   changeName,
		KeepChildren: keepChildren,
	}

	_, err := c.Pogo.RemoveChange(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("remove change"), err)
	}

	return nil
}

func (c *Client) GetRepositoryInfo(repoName string) (*protos.GetRepositoryInfoResponse, error) {
	request := &protos.GetRepositoryInfoRequest{
		Auth:     c.GetAuth(),
		RepoName: repoName,
	}

	response, err := c.Pogo.GetRepositoryInfo(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get repository info"), err)
	}

	return response, nil
}

func (c *Client) SetRepositoryVisibility(public bool) error {
	request := &protos.SetRepositoryVisibilityRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
		Public: public,
	}

	_, err := c.Pogo.SetRepositoryVisibility(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("set repository visibility"), err)
	}

	return nil
}

func (c *Client) SetSecret(key, value string) error {
	request := &protos.SetSecretRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
		Key:    key,
		Value:  value,
	}

	_, err := c.Pogo.SetSecret(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("set secret"), err)
	}

	return nil
}

func (c *Client) GetSecret(key string) (string, error) {
	request := &protos.GetSecretRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
		Key:    key,
	}

	response, err := c.Pogo.GetSecret(c.ctx, request)
	if err != nil {
		return "", errors.Join(errors.New("get secret"), err)
	}

	return response.Value, nil
}

func (c *Client) GetAllSecrets() ([]*protos.Secret, error) {
	request := &protos.GetAllSecretsRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
	}

	response, err := c.Pogo.GetAllSecrets(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get all secrets"), err)
	}

	return response.Secrets, nil
}

func (c *Client) DeleteSecret(key string) error {
	request := &protos.DeleteSecretRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
		Key:    key,
	}

	_, err := c.Pogo.DeleteSecret(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("delete secret"), err)
	}

	return nil
}

func (c *Client) ListCIRuns() ([]*protos.CIRunSummary, error) {
	request := &protos.ListCIRunsRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
	}

	resp, err := c.Pogo.ListCIRuns(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("list CI runs"), err)
	}

	return resp.Runs, nil
}

func (c *Client) GetCIRun(runID int64) (*protos.GetCIRunResponse, error) {
	request := &protos.GetCIRunRequest{
		Auth:   c.GetAuth(),
		RepoId: c.repoId,
		RunId:  runID,
	}

	resp, err := c.Pogo.GetCIRun(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get CI run"), err)
	}

	return resp, nil
}

func (c *Client) Diff(rev1, rev2 *string, out io.Writer) error {
	request := &protos.DiffRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.repoId,
		CheckedOutChangeId: &c.changeId,
	}

	if rev1 != nil {
		request.Rev1 = rev1
	}
	if rev2 != nil {
		request.Rev2 = rev2
	}

	stream, err := c.Pogo.Diff(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("call diff"), err)
	}

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Join(errors.New("receive diff response"), err)
		}

		switch payload := msg.Payload.(type) {
		case *protos.DiffResponse_FileHeader:
		case *protos.DiffResponse_DiffChunk:
			if _, err := fmt.Fprint(out, payload.DiffChunk); err != nil {
				return errors.Join(errors.New("write diff chunk"), err)
			}
		case *protos.DiffResponse_EndOfFile:
		}
	}

	return nil
}
