package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/protos"
)

const MaxFileSizeForDiff = 1 * 1024 * 1024

type FileState struct {
	Path        string
	ContentHash []byte
	Executable  bool
}

type FileOperation int

const (
	FileAdded FileOperation = iota
	FileDeleted
	FileModified
)

type FileDiff struct {
	Path      string
	Operation FileOperation
	OldState  *FileState
	NewState  *FileState
}

func collectFileStates(ctx context.Context, changeId int64) (map[string]FileState, error) {
	files, err := db.Q.GetFilesForChange(ctx, changeId)
	if err != nil {
		return nil, fmt.Errorf("get files for change: %w", err)
	}

	result := make(map[string]FileState)
	for _, f := range files {
		result[f.Name] = FileState{
			Path:        f.Name,
			ContentHash: f.ContentHash,
			Executable:  f.Executable,
		}
	}

	return result, nil
}

func determineFileOperations(oldFiles, newFiles map[string]FileState) []FileDiff {
	allPaths := make(map[string]bool)
	for path := range oldFiles {
		allPaths[path] = true
	}
	for path := range newFiles {
		allPaths[path] = true
	}

	var diffs []FileDiff
	for path := range allPaths {
		oldFile, existsOld := oldFiles[path]
		newFile, existsNew := newFiles[path]

		if !existsOld && existsNew {
			diffs = append(diffs, FileDiff{
				Path:      path,
				Operation: FileAdded,
				OldState:  nil,
				NewState:  &newFile,
			})
		} else if existsOld && !existsNew {
			diffs = append(diffs, FileDiff{
				Path:      path,
				Operation: FileDeleted,
				OldState:  &oldFile,
				NewState:  nil,
			})
		} else if existsOld && existsNew {
			if !bytes.Equal(oldFile.ContentHash, newFile.ContentHash) || oldFile.Executable != newFile.Executable {
				diffs = append(diffs, FileDiff{
					Path:      path,
					Operation: FileModified,
					OldState:  &oldFile,
					NewState:  &newFile,
				})
			}
		}
	}

	return diffs
}

func readFileContentAsString(hash []byte) (string, error) {
	hashStr := base64.URLEncoding.EncodeToString(hash)
	f, err := filecontents.OpenFileByHash(hashStr)
	if err != nil {
		return "", fmt.Errorf("open file by hash: %w", err)
	}
	defer f.Close()

	content := make([]byte, 0, 1024*1024)
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			content = append(content, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return "", fmt.Errorf("read file: %w", err)
		}
	}
	return string(content), nil
}

func isBinaryFile(hash []byte) (bool, error) {
	hashStr := base64.URLEncoding.EncodeToString(hash)
	f, fileType, err := filecontents.OpenFileByHashWithType(hashStr)
	if err != nil {
		return false, fmt.Errorf("open file by hash: %w", err)
	}
	defer f.Close()
	return fileType.Binary, nil
}

func getFileSize(hash []byte) (int64, error) {
	hashStr := base64.URLEncoding.EncodeToString(hash)
	filePath := filecontents.GetFilePathFromHash(hashStr)
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, fmt.Errorf("stat file: %w", err)
	}
	return info.Size(), nil
}

func generateFullFileDiffBlocks(oldContent, newContent string, usePatience bool) []*protos.DiffBlock {
	var diffLines []DiffLine
	if usePatience {
		diffLines = PatienceDiff(oldContent, newContent)
	} else {
		diffLines = MyersDiff(oldContent, newContent)
	}

	var blocks []*protos.DiffBlock
	var currentBlock *protos.DiffBlock
	var currentType LineType = -1

	for _, line := range diffLines {
		if line.Type != currentType {
			if currentBlock != nil && len(currentBlock.Lines) > 0 {
				blocks = append(blocks, currentBlock)
			}

			var blockType protos.DiffBlockType
			switch line.Type {
			case LineUnchanged:
				blockType = protos.DiffBlockType_DIFF_BLOCK_TYPE_UNCHANGED
			case LineAdded:
				blockType = protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED
			case LineRemoved:
				blockType = protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED
			}

			currentBlock = &protos.DiffBlock{
				Type:  blockType,
				Lines: []string{},
			}
			currentType = line.Type
		}

		currentBlock.Lines = append(currentBlock.Lines, line.Content)
	}

	if currentBlock != nil && len(currentBlock.Lines) > 0 {
		blocks = append(blocks, currentBlock)
	}

	return blocks
}

func (s *Server) Diff(req *protos.DiffRequest, stream protos.Pogo_DiffServer) error {
	ctx := stream.Context()

	gcMutex.RLock()
	defer gcMutex.RUnlock()

	repo, err := db.Q.GetRepository(ctx, req.RepoId)
	if err != nil {
		return fmt.Errorf("get repository: %w", err)
	}

	_, err = checkRepositoryAccessOrPublic(ctx, req.Auth, req.RepoId, repo.Public)
	if err != nil {
		return fmt.Errorf("check repository access: %w", err)
	}

	var change1Id, change2Id int64
	var change1Name, change2Name string

	if req.Rev2 != nil {
		if req.Rev1 == nil {
			return fmt.Errorf("rev1 is required when rev2 is provided")
		}
		change1Id, err = db.Q.FindChangeByNameFuzzy(ctx, req.RepoId, *req.Rev1)
		if err != nil {
			return fmt.Errorf("resolve rev1 %q: %w", *req.Rev1, err)
		}
		change2Id, err = db.Q.FindChangeByNameFuzzy(ctx, req.RepoId, *req.Rev2)
		if err != nil {
			return fmt.Errorf("resolve rev2 %q: %w", *req.Rev2, err)
		}
	} else if req.Rev1 != nil {
		if req.CheckedOutChangeId == nil {
			return fmt.Errorf("current change id is required when only rev1 is provided")
		}
		change1Id, err = db.Q.FindChangeByNameFuzzy(ctx, req.RepoId, *req.Rev1)
		if err != nil {
			return fmt.Errorf("resolve rev1 %q: %w", *req.Rev1, err)
		}
		change2Id = *req.CheckedOutChangeId
	} else {
		if req.CheckedOutChangeId == nil {
			return fmt.Errorf("current change id is required when no revisions are provided")
		}
		parents, err := db.Q.GetChangeParents(ctx, *req.CheckedOutChangeId)
		if err != nil {
			return fmt.Errorf("get parents for current change: %w", err)
		}
		if len(parents) == 0 {
			return fmt.Errorf("current change has no parents")
		}
		if len(parents) > 1 {
			return fmt.Errorf("current change has multiple parents, please specify which one to diff against")
		}
		change1Id = parents[0].ID
		change2Id = *req.CheckedOutChangeId
	}

	change1, err := db.Q.GetChange(ctx, change1Id)
	if err != nil {
		return fmt.Errorf("get change1: %w", err)
	}
	change2, err := db.Q.GetChange(ctx, change2Id)
	if err != nil {
		return fmt.Errorf("get change2: %w", err)
	}

	change1Name = change1.Name
	change2Name = change2.Name

	oldFiles, err := collectFileStates(ctx, change1Id)
	if err != nil {
		return fmt.Errorf("collect old file states: %w", err)
	}

	newFiles, err := collectFileStates(ctx, change2Id)
	if err != nil {
		return fmt.Errorf("collect new file states: %w", err)
	}

	fileDiffs := determineFileOperations(oldFiles, newFiles)

	usePatience := false
	if req.UsePatience != nil {
		usePatience = *req.UsePatience
	}
	includeLargeFiles := false
	if req.IncludeLargeFiles != nil {
		includeLargeFiles = *req.IncludeLargeFiles
	}

	for _, fileDiff := range fileDiffs {
		if err := s.streamFileDiff(stream, fileDiff, change1Name, change2Name, usePatience, includeLargeFiles); err != nil {
			return fmt.Errorf("stream file diff for %s: %w", fileDiff.Path, err)
		}
	}

	return nil
}

func (s *Server) streamFileDiff(stream protos.Pogo_DiffServer, fileDiff FileDiff, oldChangeName, newChangeName string, usePatience, includeLargeFiles bool) error {
	var oldHash, newHash string
	var oldLineCount, newLineCount int32

	if fileDiff.OldState != nil {
		oldHash = base64.URLEncoding.EncodeToString(fileDiff.OldState.ContentHash)
		if len(oldHash) > 7 {
			oldHash = oldHash[:7]
		}
	}

	if fileDiff.NewState != nil {
		newHash = base64.URLEncoding.EncodeToString(fileDiff.NewState.ContentHash)
		if len(newHash) > 7 {
			newHash = newHash[:7]
		}
	}

	var status protos.DiffFileStatus
	var blocks []*protos.DiffBlock

	switch fileDiff.Operation {
	case FileAdded:
		status = protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED

		size, err := getFileSize(fileDiff.NewState.ContentHash)
		if err != nil {
			return fmt.Errorf("get file size: %w", err)
		}

		if size > MaxFileSizeForDiff && !includeLargeFiles {
			blocks = []*protos.DiffBlock{{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
				Lines: []string{fmt.Sprintf("File too large (%d bytes). Use --include-large-files to show diff.", size)},
			}}
		} else {
			isBinary, err := isBinaryFile(fileDiff.NewState.ContentHash)
			if err != nil {
				return fmt.Errorf("check if binary: %w", err)
			}

			if isBinary {
				status = protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY
				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
					Lines: []string{"Binary file"},
				}}
			} else {
				content, err := readFileContentAsString(fileDiff.NewState.ContentHash)
				if err != nil {
					return fmt.Errorf("read new file content: %w", err)
				}

				lines := strings.Split(content, "\n")
				newLineCount = int32(len(lines))

				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED,
					Lines: lines,
				}}
			}
		}

	case FileDeleted:
		status = protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED

		size, err := getFileSize(fileDiff.OldState.ContentHash)
		if err != nil {
			return fmt.Errorf("get file size: %w", err)
		}

		if size > MaxFileSizeForDiff && !includeLargeFiles {
			blocks = []*protos.DiffBlock{{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
				Lines: []string{fmt.Sprintf("File too large (%d bytes). Use --include-large-files to show diff.", size)},
			}}
		} else {
			isBinary, err := isBinaryFile(fileDiff.OldState.ContentHash)
			if err != nil {
				return fmt.Errorf("check if binary: %w", err)
			}

			if isBinary {
				status = protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY
				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
					Lines: []string{"Binary file"},
				}}
			} else {
				content, err := readFileContentAsString(fileDiff.OldState.ContentHash)
				if err != nil {
					return fmt.Errorf("read old file content: %w", err)
				}

				lines := strings.Split(content, "\n")
				oldLineCount = int32(len(lines))

				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED,
					Lines: lines,
				}}
			}
		}

	case FileModified:
		status = protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED

		oldSize, err := getFileSize(fileDiff.OldState.ContentHash)
		if err != nil {
			return fmt.Errorf("get old file size: %w", err)
		}
		newSize, err := getFileSize(fileDiff.NewState.ContentHash)
		if err != nil {
			return fmt.Errorf("get new file size: %w", err)
		}

		maxSize := oldSize
		if newSize > maxSize {
			maxSize = newSize
		}

		if maxSize > MaxFileSizeForDiff && !includeLargeFiles {
			blocks = []*protos.DiffBlock{{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
				Lines: []string{fmt.Sprintf("File too large (%d bytes). Use --include-large-files to show diff.", maxSize)},
			}}
		} else {
			isBinary1, err := isBinaryFile(fileDiff.OldState.ContentHash)
			if err != nil {
				return fmt.Errorf("check if old file is binary: %w", err)
			}
			isBinary2, err := isBinaryFile(fileDiff.NewState.ContentHash)
			if err != nil {
				return fmt.Errorf("check if new file is binary: %w", err)
			}

			if isBinary1 || isBinary2 {
				status = protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY
				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
					Lines: []string{"Binary files differ"},
				}}
			} else {
				oldContent, err := readFileContentAsString(fileDiff.OldState.ContentHash)
				if err != nil {
					return fmt.Errorf("read old file content: %w", err)
				}
				newContent, err := readFileContentAsString(fileDiff.NewState.ContentHash)
				if err != nil {
					return fmt.Errorf("read new file content: %w", err)
				}

				oldLines := strings.Split(oldContent, "\n")
				newLines := strings.Split(newContent, "\n")
				oldLineCount = int32(len(oldLines))
				newLineCount = int32(len(newLines))

				if fileDiff.OldState.Executable != fileDiff.NewState.Executable {
					oldMode := "100644"
					if fileDiff.OldState.Executable {
						oldMode = "100755"
					}
					newMode := "100644"
					if fileDiff.NewState.Executable {
						newMode = "100755"
					}

					blocks = append(blocks, &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{fmt.Sprintf("old mode %s", oldMode)},
					})
					blocks = append(blocks, &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{fmt.Sprintf("new mode %s", newMode)},
					})
				}

				blocks = append(blocks, generateFullFileDiffBlocks(oldContent, newContent, usePatience)...)
			}
		}
	}

	if err := stream.Send(&protos.DiffResponse{
		Payload: &protos.DiffResponse_FileHeader{
			FileHeader: &protos.DiffFileHeader{
				Path:          fileDiff.Path,
				OldChangeName: oldChangeName,
				NewChangeName: newChangeName,
				Status:        status,
				OldHash:       oldHash,
				NewHash:       newHash,
				OldLineCount:  oldLineCount,
				NewLineCount:  newLineCount,
			},
		},
	}); err != nil {
		return fmt.Errorf("send file header: %w", err)
	}

	for _, block := range blocks {
		if err := stream.Send(&protos.DiffResponse{
			Payload: &protos.DiffResponse_DiffBlock{
				DiffBlock: block,
			},
		}); err != nil {
			return fmt.Errorf("send diff block: %w", err)
		}
	}

	if err := stream.Send(&protos.DiffResponse{
		Payload: &protos.DiffResponse_EndOfFile{
			EndOfFile: &protos.EndOfFile{},
		},
	}); err != nil {
		return fmt.Errorf("send end of file: %w", err)
	}

	return nil
}

func (s *Server) DiffLocal(stream protos.Pogo_DiffLocalServer) error {
	ctx := stream.Context()

	gcMutex.RLock()
	defer gcMutex.RUnlock()

	var auth *protos.Auth
	var repoId int32
	var changeId int64

	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("receive auth: %w", err)
	}
	authPayload, ok := msg.Payload.(*protos.DiffLocalRequest_Auth)
	if !ok {
		return fmt.Errorf("expected auth, got %T", msg.Payload)
	}
	auth = authPayload.Auth

	msg, err = stream.Recv()
	if err != nil {
		return fmt.Errorf("receive repo_id: %w", err)
	}
	repoIdPayload, ok := msg.Payload.(*protos.DiffLocalRequest_RepoId)
	if !ok {
		return fmt.Errorf("expected repo_id, got %T", msg.Payload)
	}
	repoId = repoIdPayload.RepoId

	msg, err = stream.Recv()
	if err != nil {
		return fmt.Errorf("receive checked_out_change_id: %w", err)
	}
	changeIdPayload, ok := msg.Payload.(*protos.DiffLocalRequest_CheckedOutChangeId)
	if !ok {
		return fmt.Errorf("expected checked_out_change_id, got %T", msg.Payload)
	}
	changeId = changeIdPayload.CheckedOutChangeId

	msg, err = stream.Recv()
	if err != nil {
		return fmt.Errorf("receive use_patience: %w", err)
	}
	usePatiencePayload, ok := msg.Payload.(*protos.DiffLocalRequest_UsePatience)
	if !ok {
		return fmt.Errorf("expected use_patience, got %T", msg.Payload)
	}
	usePatience := usePatiencePayload.UsePatience

	msg, err = stream.Recv()
	if err != nil {
		return fmt.Errorf("receive include_large_files: %w", err)
	}
	includeLargeFilesPayload, ok := msg.Payload.(*protos.DiffLocalRequest_IncludeLargeFiles)
	if !ok {
		return fmt.Errorf("expected include_large_files, got %T", msg.Payload)
	}
	includeLargeFiles := includeLargeFilesPayload.IncludeLargeFiles

	repo, err := db.Q.GetRepository(ctx, repoId)
	if err != nil {
		return fmt.Errorf("get repository: %w", err)
	}

	_, err = checkRepositoryAccessOrPublic(ctx, auth, repoId, repo.Public)
	if err != nil {
		return fmt.Errorf("check repository access: %w", err)
	}

	localFiles := make(map[string]*protos.LocalFileMetadata)
	for {
		msg, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("receive metadata: %w", err)
		}

		switch payload := msg.Payload.(type) {
		case *protos.DiffLocalRequest_FileMetadata:
			localFiles[payload.FileMetadata.Path] = payload.FileMetadata
		case *protos.DiffLocalRequest_EndOfMetadata:
			goto MetadataComplete
		default:
			return fmt.Errorf("unexpected payload type: %T", msg.Payload)
		}
	}

MetadataComplete:
	remoteFiles, err := collectFileStates(ctx, changeId)
	if err != nil {
		return fmt.Errorf("get remote files: %w", err)
	}

	change, err := db.Q.GetChange(ctx, changeId)
	if err != nil {
		return fmt.Errorf("get change: %w", err)
	}

	allPaths := make(map[string]bool)
	for path := range localFiles {
		allPaths[path] = true
	}
	for path := range remoteFiles {
		allPaths[path] = true
	}

	contentRequests := make(map[string]bool)

	for path := range allPaths {
		localFile, existsLocal := localFiles[path]
		remoteFile, existsRemote := remoteFiles[path]

		if !existsRemote {
			contentRequests[path] = true
		} else if !existsLocal {
			continue
		} else {
			if !bytes.Equal(localFile.ContentHash, remoteFile.ContentHash) || (localFile.Executable != nil && *localFile.Executable != remoteFile.Executable) {
				contentRequests[path] = true
			}
		}
	}

	for path := range contentRequests {
		if err := stream.Send(&protos.DiffLocalResponse{
			Payload: &protos.DiffLocalResponse_ContentRequest{
				ContentRequest: &protos.ContentRequest{Path: path},
			},
		}); err != nil {
			return fmt.Errorf("send content request: %w", err)
		}
	}

	localFileContents := make(map[string]string)
	for path := range contentRequests {
		var content bytes.Buffer
		for {
			msg, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("receive file content: %w", err)
			}

			switch payload := msg.Payload.(type) {
			case *protos.DiffLocalRequest_FileContent:
				content.Write(payload.FileContent)
			case *protos.DiffLocalRequest_Eof:
				localFileContents[path] = content.String()
				goto ContentReceived
			default:
				return fmt.Errorf("unexpected payload type while receiving content: %T", msg.Payload)
			}
		}
	ContentReceived:
	}

	for path := range allPaths {
		localFile, existsLocal := localFiles[path]
		remoteFile, existsRemote := remoteFiles[path]

		var fileDiff FileDiff
		if !existsRemote && existsLocal {
			fileDiff = FileDiff{
				Path:      path,
				Operation: FileAdded,
				OldState:  nil,
				NewState: &FileState{
					Path:        path,
					ContentHash: localFile.ContentHash,
					Executable:  localFile.Executable != nil && *localFile.Executable,
				},
			}
		} else if existsRemote && !existsLocal {
			fileDiff = FileDiff{
				Path:      path,
				Operation: FileDeleted,
				OldState:  &remoteFile,
				NewState:  nil,
			}
		} else if existsLocal && existsRemote {
			if !bytes.Equal(localFile.ContentHash, remoteFile.ContentHash) || (localFile.Executable != nil && *localFile.Executable != remoteFile.Executable) {
				fileDiff = FileDiff{
					Path:      path,
					Operation: FileModified,
					OldState:  &remoteFile,
					NewState: &FileState{
						Path:        path,
						ContentHash: localFile.ContentHash,
						Executable:  localFile.Executable != nil && *localFile.Executable,
					},
				}
			} else {
				continue
			}
		} else {
			continue
		}

		if err := s.streamFileDiffLocal(stream, fileDiff, change.Name, localFileContents, usePatience, includeLargeFiles); err != nil {
			return fmt.Errorf("stream file diff local for %s: %w", path, err)
		}
	}

	return nil
}

func (s *Server) streamFileDiffLocal(stream protos.Pogo_DiffLocalServer, fileDiff FileDiff, oldChangeName string, localContents map[string]string, usePatience, includeLargeFiles bool) error {
	var oldHash, newHash string
	var oldLineCount, newLineCount int32

	if fileDiff.OldState != nil {
		oldHash = base64.URLEncoding.EncodeToString(fileDiff.OldState.ContentHash)
		if len(oldHash) > 7 {
			oldHash = oldHash[:7]
		}
	}

	if fileDiff.NewState != nil {
		newHash = base64.URLEncoding.EncodeToString(fileDiff.NewState.ContentHash)
		if len(newHash) > 7 {
			newHash = newHash[:7]
		}
	}

	var status protos.DiffFileStatus
	var blocks []*protos.DiffBlock

	switch fileDiff.Operation {
	case FileAdded:
		status = protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED

		content := localContents[fileDiff.Path]
		size := int64(len(content))

		if size > MaxFileSizeForDiff && !includeLargeFiles {
			blocks = []*protos.DiffBlock{{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
				Lines: []string{fmt.Sprintf("File too large (%d bytes). Use --include-large-files to show diff.", size)},
			}}
		} else {
			isBinary := false
			for i := 0; i < len(content) && i < 8192; i++ {
				if content[i] == 0 {
					isBinary = true
					break
				}
			}

			if isBinary {
				status = protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY
				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
					Lines: []string{"Binary file"},
				}}
			} else {
				lines := strings.Split(content, "\n")
				newLineCount = int32(len(lines))

				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED,
					Lines: lines,
				}}
			}
		}

	case FileDeleted:
		status = protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED

		size, err := getFileSize(fileDiff.OldState.ContentHash)
		if err != nil {
			return fmt.Errorf("get file size: %w", err)
		}

		if size > MaxFileSizeForDiff && !includeLargeFiles {
			blocks = []*protos.DiffBlock{{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
				Lines: []string{fmt.Sprintf("File too large (%d bytes). Use --include-large-files to show diff.", size)},
			}}
		} else {
			isBinary, err := isBinaryFile(fileDiff.OldState.ContentHash)
			if err != nil {
				return fmt.Errorf("check if binary: %w", err)
			}

			if isBinary {
				status = protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY
				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
					Lines: []string{"Binary file"},
				}}
			} else {
				content, err := readFileContentAsString(fileDiff.OldState.ContentHash)
				if err != nil {
					return fmt.Errorf("read old file content: %w", err)
				}

				lines := strings.Split(content, "\n")
				oldLineCount = int32(len(lines))

				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED,
					Lines: lines,
				}}
			}
		}

	case FileModified:
		status = protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED

		newContent := localContents[fileDiff.Path]
		newSize := int64(len(newContent))

		oldSize, err := getFileSize(fileDiff.OldState.ContentHash)
		if err != nil {
			return fmt.Errorf("get old file size: %w", err)
		}

		maxSize := oldSize
		if newSize > maxSize {
			maxSize = newSize
		}

		if maxSize > MaxFileSizeForDiff && !includeLargeFiles {
			blocks = []*protos.DiffBlock{{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
				Lines: []string{fmt.Sprintf("File too large (%d bytes). Use --include-large-files to show diff.", maxSize)},
			}}
		} else {
			isBinary1, err := isBinaryFile(fileDiff.OldState.ContentHash)
			if err != nil {
				return fmt.Errorf("check if old file is binary: %w", err)
			}

			isBinary2 := false
			for i := 0; i < len(newContent) && i < 8192; i++ {
				if newContent[i] == 0 {
					isBinary2 = true
					break
				}
			}

			if isBinary1 || isBinary2 {
				status = protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY
				blocks = []*protos.DiffBlock{{
					Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
					Lines: []string{"Binary files differ"},
				}}
			} else {
				oldContent, err := readFileContentAsString(fileDiff.OldState.ContentHash)
				if err != nil {
					return fmt.Errorf("read old file content: %w", err)
				}

				oldLines := strings.Split(oldContent, "\n")
				newLines := strings.Split(newContent, "\n")
				oldLineCount = int32(len(oldLines))
				newLineCount = int32(len(newLines))

				if fileDiff.OldState.Executable != fileDiff.NewState.Executable {
					oldMode := "100644"
					if fileDiff.OldState.Executable {
						oldMode = "100755"
					}
					newMode := "100644"
					if fileDiff.NewState.Executable {
						newMode = "100755"
					}

					blocks = append(blocks, &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{fmt.Sprintf("old mode %s", oldMode)},
					})
					blocks = append(blocks, &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{fmt.Sprintf("new mode %s", newMode)},
					})
				}

				blocks = append(blocks, generateFullFileDiffBlocks(oldContent, newContent, usePatience)...)
			}
		}
	}

	if err := stream.Send(&protos.DiffLocalResponse{
		Payload: &protos.DiffLocalResponse_FileHeader{
			FileHeader: &protos.DiffFileHeader{
				Path:          fileDiff.Path,
				OldChangeName: oldChangeName,
				NewChangeName: "local",
				Status:        status,
				OldHash:       oldHash,
				NewHash:       newHash,
				OldLineCount:  oldLineCount,
				NewLineCount:  newLineCount,
			},
		},
	}); err != nil {
		return fmt.Errorf("send file header: %w", err)
	}

	for _, block := range blocks {
		if err := stream.Send(&protos.DiffLocalResponse{
			Payload: &protos.DiffLocalResponse_DiffBlock{
				DiffBlock: block,
			},
		}); err != nil {
			return fmt.Errorf("send diff block: %w", err)
		}
	}

	if err := stream.Send(&protos.DiffLocalResponse{
		Payload: &protos.DiffLocalResponse_EndOfFile{
			EndOfFile: &protos.EndOfFile{},
		},
	}); err != nil {
		return fmt.Errorf("send end of file: %w", err)
	}

	return nil
}

func checkRepositoryAccessOrPublic(ctx context.Context, auth *protos.Auth, repoId int32, isPublic bool) (*int32, error) {
	if isPublic {
		return nil, nil
	}

	userId, err := getUserIdFromAuth(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("get user from auth: %w", err)
	}

	if userId == nil {
		return nil, fmt.Errorf("user not authenticated and repository is not public")
	}

	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, repoId, *userId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	if !hasAccess {
		return nil, fmt.Errorf("user does not have access to repository")
	}

	return userId, nil
}
