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
	"sort"
	"time"

	"github.com/pogo-vcs/pogo/client/difftui"
	"github.com/pogo-vcs/pogo/colors"
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

	// Note: RepoStore will be created by caller (cmd/init.go)
	repo = initResponse.RepoId
	change = initResponse.ChangeId
	return
}

func (c *Client) DeleteRepo() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	request := &protos.DeleteRepositoryRequest{
		Auth:   c.GetAuth(),
		RepoId: c.getRepoId(),
	}

	_, err := c.Pogo.DeleteRepository(ctx, request)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) PushFull(force bool) error {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Minute)
	defer cancel()

	// First, collect all file hashes
	fmt.Fprintln(c.VerboseOut, "Collecting file hashes...")
	type fileInfo struct {
		file          LocalFile
		hash          []byte
		executable    *bool
		isSymlink     bool
		symlinkTarget string
	}
	var files []fileInfo
	var allHashes [][]byte

	for file := range c.UnignoredFiles {
		// check if context is canceled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if file is a symlink
		isSymlink, target, err := c.IsSymlink(file.AbsPath)
		if err != nil {
			return errors.Join(fmt.Errorf("check symlink %s", file.Name), err)
		}

		var hash []byte
		if isSymlink {
			// No caching for symlinks - target path is small
			// Validate and normalize symlink target
			normalizedTarget, err := c.ValidateAndNormalizeSymlink(file.AbsPath, target)
			if err != nil {
				return errors.Join(fmt.Errorf("validate symlink %s", file.Name), err)
			}
			// Hash the symlink target path
			hash = GetSymlinkHash(normalizedTarget)
			files = append(files, fileInfo{
				file:          file,
				hash:          hash,
				executable:    nil,
				isSymlink:     true,
				symlinkTarget: normalizedTarget,
			})
		} else {
			// Regular file - use cache
			info, err := os.Lstat(file.AbsPath)
			if err != nil {
				return errors.Join(fmt.Errorf("stat file %s", file.Name), err)
			}

			inode := getInode(info)
			mtimeSec := info.ModTime().Unix()
			mtimeNsec := info.ModTime().UnixNano() % 1e9

			if c.repoStore != nil {
				cachedHash, cacheHit := c.repoStore.GetFileHash(
					file.Name,
					info.Size(),
					mtimeSec,
					mtimeNsec,
					inode,
				)

				if cacheHit {
					hash = cachedHash
					fmt.Fprintf(c.VerboseOut, "Cache hit: %s\n", file.Name)
				}
			}

			if hash == nil {
				hash, err = filecontents.HashFile(file.AbsPath)
				if err != nil {
					return errors.Join(fmt.Errorf("hash file %s", file.Name), err)
				}

				if c.repoStore != nil {
					if err := c.repoStore.SetFileHash(file.Name, info.Size(), mtimeSec, mtimeNsec, inode, hash); err != nil {
						fmt.Fprintf(c.VerboseOut, "Warning: failed to cache hash for %s: %v\n", file.Name, err)
					}
				}
				fmt.Fprintf(c.VerboseOut, "Cache miss: %s\n", file.Name)
			}

			files = append(files, fileInfo{
				file:       file,
				hash:       hash,
				executable: IsExecutable(file.AbsPath),
				isSymlink:  false,
			})
		}
		allHashes = append(allHashes, hash)
	}

	// Check which files are needed by the server
	fmt.Fprintf(c.VerboseOut, "Checking which of %d files need to be uploaded...\n", len(files))
	checkResp, err := c.Pogo.CheckNeededFiles(ctx, &protos.CheckNeededFilesRequest{
		Auth:       c.GetAuth(),
		RepoId:     c.getRepoId(),
		FileHashes: allHashes,
	})
	if err != nil {
		return errors.Join(errors.New("check needed files"), err)
	}

	// Create a set of needed hashes for quick lookup
	neededHashes := make(map[string]bool)
	for _, hash := range checkResp.NeededHashes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
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
			ChangeId: c.getChangeId(),
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		hashStr := base64.URLEncoding.EncodeToString(fileInfo.hash)
		needsContent := neededHashes[hashStr]

		if fileInfo.isSymlink {
			fmt.Fprintf(c.VerboseOut, "Pushing symlink: %s -> %s\n", fileInfo.file.Name, fileInfo.symlinkTarget)
		} else if needsContent {
			fmt.Fprintf(c.VerboseOut, "Pushing new file: %s\n", fileInfo.file.Name)
		} else {
			fmt.Fprintf(c.VerboseOut, "Skipping existing file: %s\n", fileInfo.file.Name)
		}

		// Send file header with hash
		fileHeader := &protos.FileHeader{
			Name:        fileInfo.file.Name,
			Executable:  fileInfo.executable,
			ContentHash: fileInfo.hash,
		}
		if fileInfo.isSymlink {
			fileHeader.SymlinkTarget = &fileInfo.symlinkTarget
		}

		if err := stream.Send(&protos.PushFullRequest{
			Payload: &protos.PushFullRequest_FileHeader{
				fileHeader,
			},
		}); err != nil {
			return errors.Join(fmt.Errorf("send file %s header", fileInfo.file.Name), err)
		}

		// For symlinks, don't send content
		if fileInfo.isSymlink {
			// Send has_content = false
			if err := stream.Send(&protos.PushFullRequest{
				Payload: &protos.PushFullRequest_HasContent{
					HasContent: false,
				},
			}); err != nil {
				return errors.Join(fmt.Errorf("send file %s has_content", fileInfo.file.Name), err)
			}
			continue
		}

		// Send has_content flag for regular files
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

func (c *Client) DiffLocal() ([]DiffFileInfo, error) {
	stream, err := c.Pogo.DiffLocal(c.ctx)
	if err != nil {
		return nil, errors.Join(errors.New("open diff local stream"), err)
	}
	defer stream.CloseSend()

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_Auth{
			Auth: c.GetAuth(),
		},
	}); err != nil {
		return nil, errors.Join(errors.New("send auth"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_RepoId{
			RepoId: c.getRepoId(),
		},
	}); err != nil {
		return nil, errors.Join(errors.New("send repo id"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_CheckedOutChangeId{
			CheckedOutChangeId: c.getChangeId(),
		},
	}); err != nil {
		return nil, errors.Join(errors.New("send change id"), err)
	}

	type fileInfo struct {
		file       LocalFile
		hash       []byte
		executable *bool
	}
	var files []fileInfo

	for file := range c.UnignoredFiles {
		// Get file stats for cache lookup
		info, err := os.Lstat(file.AbsPath)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("stat file %s", file.Name), err)
		}

		// Skip symlinks for this operation (DiffLocal doesn't handle them)
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		inode := getInode(info)
		mtimeSec := info.ModTime().Unix()
		mtimeNsec := info.ModTime().UnixNano() % 1e9

		cachedHash, cacheHit := c.repoStore.GetFileHash(
			file.Name,
			info.Size(),
			mtimeSec,
			mtimeNsec,
			inode,
		)

		var hash []byte
		if cacheHit {
			hash = cachedHash
		} else {
			// Cache miss - compute and update immediately
			hash, err = filecontents.HashFile(file.AbsPath)
			if err != nil {
				return nil, errors.Join(fmt.Errorf("hash file %s", file.Name), err)
			}

			if err := c.repoStore.SetFileHash(file.Name, info.Size(), mtimeSec, mtimeNsec, inode, hash); err != nil {
				// Log warning but continue (not critical for diff operation)
			}
		}

		files = append(files, fileInfo{
			file:       file,
			hash:       hash,
			executable: IsExecutable(file.AbsPath),
		})
	}

	for _, fileInfo := range files {
		if err := stream.Send(&protos.DiffLocalRequest{
			Payload: &protos.DiffLocalRequest_FileMetadata{
				FileMetadata: &protos.LocalFileMetadata{
					Path:        fileInfo.file.Name,
					ContentHash: fileInfo.hash,
					Executable:  fileInfo.executable,
				},
			},
		}); err != nil {
			return nil, errors.Join(fmt.Errorf("send file metadata %s", fileInfo.file.Name), err)
		}
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_EndOfMetadata{
			EndOfMetadata: &protos.EndOfMetadata{},
		},
	}); err != nil {
		return nil, errors.Join(errors.New("send end of metadata"), err)
	}

	contentRequests := make(map[string]bool)
	var diffs []DiffFileInfo

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.Join(errors.New("receive diff local response"), err)
		}

		switch payload := msg.Payload.(type) {
		case *protos.DiffLocalResponse_ContentRequest:
			contentRequests[payload.ContentRequest.Path] = true

		case *protos.DiffLocalResponse_FileHeader:
			diffs = append(diffs, DiffFileInfo{
				Path:   payload.FileHeader.Path,
				Status: payload.FileHeader.Status,
				Blocks: []*protos.DiffBlock{},
			})

		case *protos.DiffLocalResponse_DiffBlock:
			if len(diffs) > 0 {
				diffs[len(diffs)-1].Blocks = append(diffs[len(diffs)-1].Blocks, payload.DiffBlock)
			}
		case *protos.DiffLocalResponse_EndOfFile:
		}

		if len(contentRequests) > 0 {
			for path := range contentRequests {
				delete(contentRequests, path)

				var found bool
				var fileToSend LocalFile
				for _, f := range files {
					if f.file.Name == path {
						fileToSend = f.file
						found = true
						break
					}
				}

				if !found {
					return nil, fmt.Errorf("content requested for unknown file: %s", path)
				}

				file, err := fileToSend.Open()
				if err != nil {
					return nil, errors.Join(fmt.Errorf("open file %s", path), err)
				}

				buffer := make([]byte, 32*1024)
				for {
					n, err := file.Read(buffer)
					if n > 0 {
						if err := stream.Send(&protos.DiffLocalRequest{
							Payload: &protos.DiffLocalRequest_FileContent{
								FileContent: buffer[:n],
							},
						}); err != nil {
							file.Close()
							return nil, errors.Join(fmt.Errorf("send file content %s", path), err)
						}
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						file.Close()
						return nil, errors.Join(fmt.Errorf("read file %s", path), err)
					}
				}
				file.Close()

				if err := stream.Send(&protos.DiffLocalRequest{
					Payload: &protos.DiffLocalRequest_Eof{
						Eof: &protos.EOF{},
					},
				}); err != nil {
					return nil, errors.Join(fmt.Errorf("send eof for %s", path), err)
				}
			}
		}
	}

	return diffs, nil
}

type DiffFileInfo struct {
	Path   string
	Status protos.DiffFileStatus
	Blocks []*protos.DiffBlock
}

func (c *Client) CollectDiffLocal(usePatience, includeLargeFiles bool) (difftui.DiffData, error) {
	stream, err := c.Pogo.DiffLocal(c.ctx)
	if err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("open diff local stream"), err)
	}
	defer stream.CloseSend()

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_Auth{
			Auth: c.GetAuth(),
		},
	}); err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("send auth"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_RepoId{
			RepoId: c.getRepoId(),
		},
	}); err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("send repo id"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_CheckedOutChangeId{
			CheckedOutChangeId: c.getChangeId(),
		},
	}); err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("send change id"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_UsePatience{
			UsePatience: usePatience,
		},
	}); err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("send use patience"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_IncludeLargeFiles{
			IncludeLargeFiles: includeLargeFiles,
		},
	}); err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("send include large files"), err)
	}

	type fileInfo struct {
		file          LocalFile
		hash          []byte
		executable    *bool
		isSymlink     bool
		symlinkTarget string
	}
	var files []fileInfo

	for file := range c.UnignoredFiles {
		// Check if file is a symlink
		isSymlink, target, err := c.IsSymlink(file.AbsPath)
		if err != nil {
			return difftui.DiffData{}, errors.Join(fmt.Errorf("check symlink %s", file.Name), err)
		}

		var hash []byte
		if isSymlink {
			// No caching for symlinks
			// Validate and normalize symlink target
			normalizedTarget, err := c.ValidateAndNormalizeSymlink(file.AbsPath, target)
			if err != nil {
				return difftui.DiffData{}, errors.Join(fmt.Errorf("validate symlink %s", file.Name), err)
			}
			// Hash the symlink target path
			hash = GetSymlinkHash(normalizedTarget)
			files = append(files, fileInfo{
				file:          file,
				hash:          hash,
				executable:    nil,
				isSymlink:     true,
				symlinkTarget: normalizedTarget,
			})
		} else {
			// Regular file - use cache
			info, err := os.Lstat(file.AbsPath)
			if err != nil {
				return difftui.DiffData{}, errors.Join(fmt.Errorf("stat file %s", file.Name), err)
			}

			inode := getInode(info)
			mtimeSec := info.ModTime().Unix()
			mtimeNsec := info.ModTime().UnixNano() % 1e9

			cachedHash, cacheHit := c.repoStore.GetFileHash(
				file.Name,
				info.Size(),
				mtimeSec,
				mtimeNsec,
				inode,
			)

			if cacheHit {
				hash = cachedHash
			} else {
				// Cache miss - compute and update immediately
				hash, err = filecontents.HashFile(file.AbsPath)
				if err != nil {
					return difftui.DiffData{}, errors.Join(fmt.Errorf("hash file %s", file.Name), err)
				}

				if err := c.repoStore.SetFileHash(file.Name, info.Size(), mtimeSec, mtimeNsec, inode, hash); err != nil {
					// Continue on cache update error
				}
			}

			files = append(files, fileInfo{
				file:       file,
				hash:       hash,
				executable: IsExecutable(file.AbsPath),
				isSymlink:  false,
			})
		}
	}

	for _, fileInfo := range files {
		if err := stream.Send(&protos.DiffLocalRequest{
			Payload: &protos.DiffLocalRequest_FileMetadata{
				FileMetadata: &protos.LocalFileMetadata{
					Path:        fileInfo.file.Name,
					ContentHash: fileInfo.hash,
					Executable:  fileInfo.executable,
				},
			},
		}); err != nil {
			return difftui.DiffData{}, errors.Join(fmt.Errorf("send file metadata %s", fileInfo.file.Name), err)
		}
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_EndOfMetadata{
			EndOfMetadata: &protos.EndOfMetadata{},
		},
	}); err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("send end of metadata"), err)
	}

	contentRequests := make(map[string]bool)
	var data difftui.DiffData
	var currentFile *difftui.DiffFile

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return difftui.DiffData{}, errors.Join(errors.New("receive diff local response"), err)
		}

		switch payload := msg.Payload.(type) {
		case *protos.DiffLocalResponse_ContentRequest:
			contentRequests[payload.ContentRequest.Path] = true

		case *protos.DiffLocalResponse_FileHeader:
			if currentFile != nil {
				data.Files = append(data.Files, *currentFile)
			}
			currentFile = &difftui.DiffFile{
				Header: payload.FileHeader,
				Blocks: []*protos.DiffBlock{},
			}

		case *protos.DiffLocalResponse_DiffBlock:
			if currentFile != nil {
				currentFile.Blocks = append(currentFile.Blocks, payload.DiffBlock)
			}

		case *protos.DiffLocalResponse_EndOfFile:
			if currentFile != nil {
				data.Files = append(data.Files, *currentFile)
				currentFile = nil
			}
		}

		if len(contentRequests) > 0 {
			for path := range contentRequests {
				delete(contentRequests, path)

				var found bool
				var fileToSend LocalFile
				for _, f := range files {
					if f.file.Name == path {
						fileToSend = f.file
						found = true
						break
					}
				}

				if !found {
					return difftui.DiffData{}, fmt.Errorf("content requested for unknown file: %s", path)
				}

				file, err := fileToSend.Open()
				if err != nil {
					return difftui.DiffData{}, errors.Join(fmt.Errorf("open file %s", path), err)
				}

				buffer := make([]byte, 32*1024)
				for {
					n, err := file.Read(buffer)
					if n > 0 {
						if err := stream.Send(&protos.DiffLocalRequest{
							Payload: &protos.DiffLocalRequest_FileContent{
								FileContent: buffer[:n],
							},
						}); err != nil {
							file.Close()
							return difftui.DiffData{}, errors.Join(fmt.Errorf("send file content %s", path), err)
						}
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						file.Close()
						return difftui.DiffData{}, errors.Join(fmt.Errorf("read file %s", path), err)
					}
				}
				file.Close()

				if err := stream.Send(&protos.DiffLocalRequest{
					Payload: &protos.DiffLocalRequest_Eof{
						Eof: &protos.EOF{},
					},
				}); err != nil {
					return difftui.DiffData{}, errors.Join(fmt.Errorf("send eof for %s", path), err)
				}
			}
		}
	}

	if currentFile != nil {
		data.Files = append(data.Files, *currentFile)
	}

	// remove all files that have 0 blocks
	newFilesList := make([]difftui.DiffFile, 0, len(data.Files))
	for _, file := range data.Files {
		if len(file.Blocks) > 0 {
			newFilesList = append(newFilesList, file)
		}
	}
	data.Files = newFilesList

	// Sort files lexically by path
	sort.Slice(data.Files, func(i, j int) bool {
		return data.Files[i].Header.Path < data.Files[j].Header.Path
	})

	return data, nil
}

func (c *Client) DiffLocalWithOutput(out io.Writer, colored, usePatience, includeLargeFiles bool) error {
	stream, err := c.Pogo.DiffLocal(c.ctx)
	if err != nil {
		return errors.Join(errors.New("open diff local stream"), err)
	}
	defer stream.CloseSend()

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_Auth{
			Auth: c.GetAuth(),
		},
	}); err != nil {
		return errors.Join(errors.New("send auth"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_RepoId{
			RepoId: c.getRepoId(),
		},
	}); err != nil {
		return errors.Join(errors.New("send repo id"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_UsePatience{
			UsePatience: usePatience,
		},
	}); err != nil {
		return errors.Join(errors.New("send use patience"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_IncludeLargeFiles{
			IncludeLargeFiles: includeLargeFiles,
		},
	}); err != nil {
		return errors.Join(errors.New("send include large files"), err)
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_CheckedOutChangeId{
			CheckedOutChangeId: c.getChangeId(),
		},
	}); err != nil {
		return errors.Join(errors.New("send change id"), err)
	}

	type fileInfo struct {
		file          LocalFile
		hash          []byte
		executable    *bool
		isSymlink     bool
		symlinkTarget string
	}
	var files []fileInfo

	for file := range c.UnignoredFiles {
		// Check if file is a symlink
		isSymlink, target, err := c.IsSymlink(file.AbsPath)
		if err != nil {
			return errors.Join(fmt.Errorf("check symlink %s", file.Name), err)
		}

		var hash []byte
		if isSymlink {
			// No caching for symlinks
			// Validate and normalize symlink target
			normalizedTarget, err := c.ValidateAndNormalizeSymlink(file.AbsPath, target)
			if err != nil {
				return errors.Join(fmt.Errorf("validate symlink %s", file.Name), err)
			}
			// Hash the symlink target path
			hash = GetSymlinkHash(normalizedTarget)
			files = append(files, fileInfo{
				file:          file,
				hash:          hash,
				executable:    nil,
				isSymlink:     true,
				symlinkTarget: normalizedTarget,
			})
		} else {
			// Regular file - use cache
			info, err := os.Lstat(file.AbsPath)
			if err != nil {
				return errors.Join(fmt.Errorf("stat file %s", file.Name), err)
			}

			inode := getInode(info)
			mtimeSec := info.ModTime().Unix()
			mtimeNsec := info.ModTime().UnixNano() % 1e9

			cachedHash, cacheHit := c.repoStore.GetFileHash(
				file.Name,
				info.Size(),
				mtimeSec,
				mtimeNsec,
				inode,
			)

			if cacheHit {
				hash = cachedHash
			} else {
				// Cache miss - compute and update immediately
				hash, err = filecontents.HashFile(file.AbsPath)
				if err != nil {
					return errors.Join(fmt.Errorf("hash file %s", file.Name), err)
				}

				if err := c.repoStore.SetFileHash(file.Name, info.Size(), mtimeSec, mtimeNsec, inode, hash); err != nil {
					// Continue on cache update error
				}
			}

			files = append(files, fileInfo{
				file:       file,
				hash:       hash,
				executable: IsExecutable(file.AbsPath),
				isSymlink:  false,
			})
		}
	}

	for _, fileInfo := range files {
		if err := stream.Send(&protos.DiffLocalRequest{
			Payload: &protos.DiffLocalRequest_FileMetadata{
				FileMetadata: &protos.LocalFileMetadata{
					Path:        fileInfo.file.Name,
					ContentHash: fileInfo.hash,
					Executable:  fileInfo.executable,
				},
			},
		}); err != nil {
			return errors.Join(fmt.Errorf("send file metadata %s", fileInfo.file.Name), err)
		}
	}

	if err := stream.Send(&protos.DiffLocalRequest{
		Payload: &protos.DiffLocalRequest_EndOfMetadata{
			EndOfMetadata: &protos.EndOfMetadata{},
		},
	}); err != nil {
		return errors.Join(errors.New("send end of metadata"), err)
	}

	contentRequests := make(map[string]bool)
	var currentHeader *protos.DiffFileHeader

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Join(errors.New("receive diff local response"), err)
		}

		switch payload := msg.Payload.(type) {
		case *protos.DiffLocalResponse_ContentRequest:
			contentRequests[payload.ContentRequest.Path] = true

		case *protos.DiffLocalResponse_FileHeader:
			currentHeader = payload.FileHeader
			c.renderDiffHeader(out, currentHeader, colored)

		case *protos.DiffLocalResponse_DiffBlock:
			c.renderDiffBlock(out, payload.DiffBlock, colored)

		case *protos.DiffLocalResponse_EndOfFile:
		}

		if len(contentRequests) > 0 {
			for path := range contentRequests {
				delete(contentRequests, path)

				var found bool
				var fileToSend LocalFile
				for _, f := range files {
					if f.file.Name == path {
						fileToSend = f.file
						found = true
						break
					}
				}

				if !found {
					return fmt.Errorf("content requested for unknown file: %s", path)
				}

				file, err := fileToSend.Open()
				if err != nil {
					return errors.Join(fmt.Errorf("open file %s", path), err)
				}

				buffer := make([]byte, 32*1024)
				for {
					n, err := file.Read(buffer)
					if n > 0 {
						if err := stream.Send(&protos.DiffLocalRequest{
							Payload: &protos.DiffLocalRequest_FileContent{
								FileContent: buffer[:n],
							},
						}); err != nil {
							file.Close()
							return errors.Join(fmt.Errorf("send file content %s", path), err)
						}
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						file.Close()
						return errors.Join(fmt.Errorf("read file %s", path), err)
					}
				}
				file.Close()

				if err := stream.Send(&protos.DiffLocalRequest{
					Payload: &protos.DiffLocalRequest_Eof{
						Eof: &protos.EOF{},
					},
				}); err != nil {
					return errors.Join(fmt.Errorf("send eof for %s", path), err)
				}
			}
		}
	}

	return nil
}

func (c *Client) renderDiffHeader(out io.Writer, header *protos.DiffFileHeader, colored bool) {
	gray := ""
	reset := ""
	if colored {
		gray = colors.BrightBlack
		reset = colors.Reset
	}

	fmt.Fprintf(out, "%sdiff --git a/%s b/%s%s\n", gray, header.Path, header.Path, reset)

	switch header.Status {
	case protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED:
		fmt.Fprintf(out, "%snew file mode 100644%s\n", gray, reset)
		fmt.Fprintf(out, "%s--- /dev/null%s\n", gray, reset)
		fmt.Fprintf(out, "%s+++ b/%s%s\n", gray, header.Path, reset)
		fmt.Fprintf(out, "%s@@ -0,0 +1,%d @@%s\n", gray, header.NewLineCount, reset)

	case protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED:
		fmt.Fprintf(out, "%sdeleted file mode 100644%s\n", gray, reset)
		fmt.Fprintf(out, "%s--- a/%s%s\n", gray, header.Path, reset)
		fmt.Fprintf(out, "%s+++ /dev/null%s\n", gray, reset)
		fmt.Fprintf(out, "%s@@ -1,%d +0,0 @@%s\n", gray, header.OldLineCount, reset)

	case protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY:
		fmt.Fprintf(out, "%sindex %s..%s%s\n", gray, header.OldHash, header.NewHash, reset)

	case protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED:
		fmt.Fprintf(out, "%sindex %s..%s%s\n", gray, header.OldHash, header.NewHash, reset)
		fmt.Fprintf(out, "%s--- a/%s%s\n", gray, header.Path, reset)
		fmt.Fprintf(out, "%s+++ b/%s%s\n", gray, header.Path, reset)
	}
}

func (c *Client) renderDiffBlock(out io.Writer, block *protos.DiffBlock, colored bool) {
	gray := ""
	green := ""
	red := ""
	reset := ""
	if colored {
		gray = colors.BrightBlack
		green = colors.Green
		red = colors.Red
		reset = colors.Reset
	}

	switch block.Type {
	case protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA:
		for _, line := range block.Lines {
			fmt.Fprintf(out, "%s%s%s\n", gray, line, reset)
		}

	case protos.DiffBlockType_DIFF_BLOCK_TYPE_UNCHANGED:
		for _, line := range block.Lines {
			fmt.Fprintf(out, " %s\n", line)
		}

	case protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED:
		for _, line := range block.Lines {
			fmt.Fprintf(out, "%s-%s%s\n", red, line, reset)
		}

	case protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED:
		for _, line := range block.Lines {
			fmt.Fprintf(out, "%s+%s%s\n", green, line, reset)
		}
	}
}

func (c *Client) SetBookmark(bookmarkName string, changeName *string) error {
	request := &protos.SetBookmarkRequest{
		Auth:         c.GetAuth(),
		RepoId:       c.getRepoId(),
		BookmarkName: bookmarkName,
		ChangeName:   changeName,
	}

	if changeName == nil {
		changeId := c.getChangeId()
		request.CheckedOutChangeId = &changeId
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
		RepoId:       c.getRepoId(),
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
		RepoId: c.getRepoId(),
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
		RepoId:            c.getRepoId(),
		Description:       description,
		ParentChangeNames: parentChangeNames,
	}

	// If no parent change names provided, use current checked out change
	if len(parentChangeNames) == 0 {
		changeId := c.getChangeId()
		request.CheckedOutChangeId = &changeId
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
		ChangeId: c.getChangeId(),
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
		ChangeId:    c.getChangeId(),
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
		RepoId:             c.getRepoId(),
		CheckedOutChangeId: c.getChangeId(),
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
		RepoId:             c.getRepoId(),
		CheckedOutChangeId: c.getChangeId(),
		MaxChanges:         maxChanges,
	}

	response, err := c.Pogo.Log(c.ctx, request)
	if err != nil {
		return "", errors.Join(errors.New("get log"), err)
	}

	return RenderLogAsJSON(response)
}

func (c *Client) GetLogData(maxChanges int32) (*LogData, error) {
	request := &protos.LogRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.getRepoId(),
		CheckedOutChangeId: c.getChangeId(),
		MaxChanges:         maxChanges,
	}

	response, err := c.Pogo.Log(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get log"), err)
	}

	return ExtractLogData(response), nil
}

func (c *Client) GetRawData() (server string, repoId int32, changeId int64) {
	server = c.getServer()
	repoId = c.getRepoId()
	changeId = c.getChangeId()
	return
}

func (c *Client) Info() (*protos.InfoResponse, error) {
	request := &protos.InfoRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.getRepoId(),
		CheckedOutChangeId: c.getChangeId(),
	}

	response, err := c.Pogo.Info(c.ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get info: %w", err)
	}

	return response, nil
}

func (c *Client) Checkout(repoId int32, changeId int64) error {
	// Collect client files
	var clientFiles []string
	for file := range c.UnignoredFiles {
		clientFiles = append(clientFiles, file.Name)
	}

	request := &protos.EditRequest{
		Auth:        c.GetAuth(),
		RepoId:      c.getRepoId(),
		ChangeId:    changeId,
		ClientFiles: clientFiles,
	}

	return c.plainEditRequest(request)
}

func (c *Client) Edit(revision string) error {
	// Collect client files
	var clientFiles []string
	for file := range c.UnignoredFiles {
		clientFiles = append(clientFiles, file.Name)
	}

	request := &protos.EditRequest{
		Auth:        c.GetAuth(),
		RepoId:      c.getRepoId(),
		Revision:    revision,
		ClientFiles: clientFiles,
	}

	return c.plainEditRequest(request)
}

func (c *Client) plainEditRequest(request *protos.EditRequest) error {
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

			// Remove from cache
			if err := c.repoStore.DeleteFileHash(fileName); err != nil {
				fmt.Fprintf(c.VerboseOut, "Warning: failed to remove cache entry for %s: %v\n", fileName, err)
			}

			filesDeleted++

		case *protos.EditResponse_FileHeader:
			// Close previous file if open
			if currentFile != nil {
				currentFile.Close()
				currentFile = nil
			}

			// Create new file or symlink
			currentFileName = payload.FileHeader.Name
			currentFileExecutable = payload.FileHeader.Executable != nil && *payload.FileHeader.Executable
			absPath := filepath.Join(c.Location, filepath.FromSlash(currentFileName))
			_ = os.MkdirAll(filepath.Dir(absPath), 0755)

			// Check if this is a symlink
			if payload.FileHeader.SymlinkTarget != nil {
				// Create symlink
				target := *payload.FileHeader.SymlinkTarget
				// Remove existing file/symlink if it exists
				_ = os.Remove(absPath)

				// Convert forward slashes to OS-specific separators
				targetPath := filepath.FromSlash(target)

				if err := CreateSymlink(targetPath, absPath); err != nil {
					return errors.Join(fmt.Errorf("create symlink %s -> %s", currentFileName, target), err)
				}
				filesCreated++
				// No file content will follow for symlinks, so reset currentFile
				currentFile = nil
				currentFileName = ""
			} else {
				// Regular file - create it
				currentFile, err = os.Create(absPath)
				if err != nil {
					return errors.Join(fmt.Errorf("create file %s", currentFileName), err)
				}
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
		RepoId:       c.getRepoId(),
		ChangeName:   changeName,
		KeepChildren: keepChildren,
	}

	_, err := c.Pogo.RemoveChange(c.ctx, request)
	if err != nil {
		return errors.Join(errors.New("remove change"), err)
	}

	return nil
}

func (c *Client) GetCurrentChangeId() int64 {
	return c.getChangeId()
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
		RepoId: c.getRepoId(),
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
		RepoId: c.getRepoId(),
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
		RepoId: c.getRepoId(),
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
		RepoId: c.getRepoId(),
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
		RepoId: c.getRepoId(),
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
		RepoId: c.getRepoId(),
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
		RepoId: c.getRepoId(),
		RunId:  runID,
	}

	resp, err := c.Pogo.GetCIRun(c.ctx, request)
	if err != nil {
		return nil, errors.Join(errors.New("get CI run"), err)
	}

	return resp, nil
}

func (c *Client) CollectDiff(rev1, rev2 *string, usePatience, includeLargeFiles bool) (difftui.DiffData, error) {
	changeId := c.getChangeId()
	request := &protos.DiffRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.getRepoId(),
		CheckedOutChangeId: &changeId,
		UsePatience:        &usePatience,
		IncludeLargeFiles:  &includeLargeFiles,
	}

	if rev1 != nil {
		request.Rev1 = rev1
	}
	if rev2 != nil {
		request.Rev2 = rev2
	}

	stream, err := c.Pogo.Diff(c.ctx, request)
	if err != nil {
		return difftui.DiffData{}, errors.Join(errors.New("call diff"), err)
	}

	var data difftui.DiffData
	var currentFile *difftui.DiffFile

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return difftui.DiffData{}, errors.Join(errors.New("receive diff response"), err)
		}

		switch payload := msg.Payload.(type) {
		case *protos.DiffResponse_FileHeader:
			if currentFile != nil && len(currentFile.Blocks) > 0 {
				data.Files = append(data.Files, *currentFile)
			}
			currentFile = &difftui.DiffFile{
				Header: payload.FileHeader,
				Blocks: []*protos.DiffBlock{},
			}

		case *protos.DiffResponse_DiffBlock:
			if currentFile != nil {
				currentFile.Blocks = append(currentFile.Blocks, payload.DiffBlock)
			}

		case *protos.DiffResponse_EndOfFile:
			if currentFile != nil && len(currentFile.Blocks) > 0 {
				data.Files = append(data.Files, *currentFile)
			}
			currentFile = nil
		}
	}

	if currentFile != nil && len(currentFile.Blocks) > 0 {
		data.Files = append(data.Files, *currentFile)
	}

	// Sort files lexically by path
	sort.Slice(data.Files, func(i, j int) bool {
		return data.Files[i].Header.Path < data.Files[j].Header.Path
	})

	return data, nil
}

func (c *Client) Diff(rev1, rev2 *string, out io.Writer, colored, usePatience, includeLargeFiles bool) error {
	changeId := c.getChangeId()
	request := &protos.DiffRequest{
		Auth:               c.GetAuth(),
		RepoId:             c.getRepoId(),
		CheckedOutChangeId: &changeId,
		UsePatience:        &usePatience,
		IncludeLargeFiles:  &includeLargeFiles,
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

	var currentHeader *protos.DiffFileHeader

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
			currentHeader = payload.FileHeader
			c.renderDiffHeader(out, currentHeader, colored)

		case *protos.DiffResponse_DiffBlock:
			c.renderDiffBlock(out, payload.DiffBlock, colored)

		case *protos.DiffResponse_EndOfFile:
		}
	}

	return nil
}
