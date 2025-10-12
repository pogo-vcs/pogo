package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/protos"
	diffmp "github.com/sergi/go-diff/diffmatchpatch"
)

type DiffFileInfo struct {
	Path           string
	OldChangeName  string
	NewChangeName  string
	Status         protos.DiffFileStatus
	OldContentHash []byte
	NewContentHash []byte
	OldExecutable  bool
	NewExecutable  bool
}

func ResolveDiff(ctx context.Context, repoId int32, rev1Opt, rev2Opt *string, currentChangeId *int64) ([]DiffFileInfo, error) {
	var change1Id, change2Id int64
	var change1Name, change2Name string
	var err error

	if rev2Opt != nil {
		if rev1Opt == nil {
			return nil, fmt.Errorf("rev1 is required when rev2 is provided")
		}
		change1Id, err = db.Q.FindChangeByNameFuzzy(ctx, repoId, *rev1Opt)
		if err != nil {
			return nil, fmt.Errorf("resolve rev1 %q: %w", *rev1Opt, err)
		}
		change2Id, err = db.Q.FindChangeByNameFuzzy(ctx, repoId, *rev2Opt)
		if err != nil {
			return nil, fmt.Errorf("resolve rev2 %q: %w", *rev2Opt, err)
		}
	} else if rev1Opt != nil {
		if currentChangeId == nil {
			return nil, fmt.Errorf("current change id is required when only rev1 is provided")
		}
		change1Id, err = db.Q.FindChangeByNameFuzzy(ctx, repoId, *rev1Opt)
		if err != nil {
			return nil, fmt.Errorf("resolve rev1 %q: %w", *rev1Opt, err)
		}
		change2Id = *currentChangeId
	} else {
		if currentChangeId == nil {
			return nil, fmt.Errorf("current change id is required when no revisions are provided")
		}
		parents, err := db.Q.GetChangeParents(ctx, *currentChangeId)
		if err != nil {
			return nil, fmt.Errorf("get parents for current change: %w", err)
		}
		if len(parents) == 0 {
			return nil, fmt.Errorf("current change has no parents")
		}
		if len(parents) > 1 {
			return nil, fmt.Errorf("current change has multiple parents, please specify which one to diff against")
		}
		change1Id = parents[0].ID
		change2Id = *currentChangeId
	}

	change1, err := db.Q.GetChange(ctx, change1Id)
	if err != nil {
		return nil, fmt.Errorf("get change1: %w", err)
	}
	change2, err := db.Q.GetChange(ctx, change2Id)
	if err != nil {
		return nil, fmt.Errorf("get change2: %w", err)
	}

	change1Name = change1.Name
	change2Name = change2.Name

	files1, err := db.Q.GetFilesForChange(ctx, change1Id)
	if err != nil {
		return nil, fmt.Errorf("get files for change1: %w", err)
	}

	files2, err := db.Q.GetFilesForChange(ctx, change2Id)
	if err != nil {
		return nil, fmt.Errorf("get files for change2: %w", err)
	}

	fileMap1 := make(map[string]db.GetFilesForChangeRow)
	for _, f := range files1 {
		fileMap1[f.Name] = f
	}

	fileMap2 := make(map[string]db.GetFilesForChangeRow)
	for _, f := range files2 {
		fileMap2[f.Name] = f
	}

	allPaths := make(map[string]bool)
	for path := range fileMap1 {
		allPaths[path] = true
	}
	for path := range fileMap2 {
		allPaths[path] = true
	}

	var diffs []DiffFileInfo

	for path := range allPaths {
		file1, exists1 := fileMap1[path]
		file2, exists2 := fileMap2[path]

		if !exists1 {
			diffs = append(diffs, DiffFileInfo{
				Path:           path,
				OldChangeName:  change1Name,
				NewChangeName:  change2Name,
				Status:         protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED,
				NewContentHash: file2.ContentHash,
				NewExecutable:  file2.Executable,
			})
		} else if !exists2 {
			diffs = append(diffs, DiffFileInfo{
				Path:           path,
				OldChangeName:  change1Name,
				NewChangeName:  change2Name,
				Status:         protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED,
				OldContentHash: file1.ContentHash,
				OldExecutable:  file1.Executable,
			})
		} else {
			if !bytes.Equal(file1.ContentHash, file2.ContentHash) || file1.Executable != file2.Executable {
				hash1 := base64.URLEncoding.EncodeToString(file1.ContentHash)
				hash2 := base64.URLEncoding.EncodeToString(file2.ContentHash)

				isBinary1, err := isBinaryFile(hash1)
				if err != nil {
					return nil, fmt.Errorf("check if file %q is binary: %w", path, err)
				}
				isBinary2, err := isBinaryFile(hash2)
				if err != nil {
					return nil, fmt.Errorf("check if file %q is binary: %w", path, err)
				}

				if isBinary1 || isBinary2 {
					diffs = append(diffs, DiffFileInfo{
						Path:           path,
						OldChangeName:  change1Name,
						NewChangeName:  change2Name,
						Status:         protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY,
						OldContentHash: file1.ContentHash,
						NewContentHash: file2.ContentHash,
						OldExecutable:  file1.Executable,
						NewExecutable:  file2.Executable,
					})
				} else {
					diffs = append(diffs, DiffFileInfo{
						Path:           path,
						OldChangeName:  change1Name,
						NewChangeName:  change2Name,
						Status:         protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED,
						OldContentHash: file1.ContentHash,
						NewContentHash: file2.ContentHash,
						OldExecutable:  file1.Executable,
						NewExecutable:  file2.Executable,
					})
				}
			}
		}
	}

	return diffs, nil
}

func isBinaryFile(hash string) (bool, error) {
	f, fileType, err := filecontents.OpenFileByHashWithType(hash)
	if err != nil {
		return false, fmt.Errorf("open file by hash: %w", err)
	}
	defer f.Close()

	return fileType.Binary, nil
}

func GenerateUnifiedDiff(oldContent, newContent, oldHash, newHash, path string) (string, error) {
	oldLines := strings.Split(oldContent, "\n")

	dmp := diffmp.New()
	chars1, chars2, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	lineDiffs := dmp.DiffMain(chars1, chars2, false)
	lineDiffs = dmp.DiffCharsToLines(lineDiffs, lineArray)

	var result strings.Builder
	result.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))

	oldHashShort := oldHash
	if len(oldHash) > 7 {
		oldHashShort = oldHash[:7]
	}
	newHashShort := newHash
	if len(newHash) > 7 {
		newHashShort = newHash[:7]
	}

	result.WriteString(fmt.Sprintf("index %s..%s\n", oldHashShort, newHashShort))
	result.WriteString(fmt.Sprintf("--- a/%s\n", path))
	result.WriteString(fmt.Sprintf("+++ b/%s\n", path))

	contextLines := 3
	oldLineNum := 0
	newLineNum := 0

	i := 0
	for i < len(lineDiffs) {
		for i < len(lineDiffs) && lineDiffs[i].Type == diffmp.DiffEqual {
			equalLines := strings.Split(lineDiffs[i].Text, "\n")
			if len(equalLines) > 0 && equalLines[len(equalLines)-1] == "" {
				equalLines = equalLines[:len(equalLines)-1]
			}
			oldLineNum += len(equalLines)
			newLineNum += len(equalLines)
			i++
		}

		if i >= len(lineDiffs) {
			break
		}

		hunkOldStart := oldLineNum - contextLines
		if hunkOldStart < 0 {
			hunkOldStart = 0
		}
		hunkNewStart := newLineNum - contextLines
		if hunkNewStart < 0 {
			hunkNewStart = 0
		}

		var hunkLines []string
		for j := hunkOldStart; j < oldLineNum; j++ {
			hunkLines = append(hunkLines, " "+oldLines[j])
		}

		hunkOldLineCount := oldLineNum - hunkOldStart
		hunkNewLineCount := newLineNum - hunkNewStart

		for i < len(lineDiffs) && lineDiffs[i].Type != diffmp.DiffEqual {
			switch lineDiffs[i].Type {
			case diffmp.DiffDelete:
				deleteLines := strings.Split(lineDiffs[i].Text, "\n")
				if len(deleteLines) > 0 && deleteLines[len(deleteLines)-1] == "" {
					deleteLines = deleteLines[:len(deleteLines)-1]
				}
				for _, line := range deleteLines {
					hunkLines = append(hunkLines, "-"+line)
					hunkOldLineCount++
				}
				oldLineNum += len(deleteLines)
			case diffmp.DiffInsert:
				insertLines := strings.Split(lineDiffs[i].Text, "\n")
				if len(insertLines) > 0 && insertLines[len(insertLines)-1] == "" {
					insertLines = insertLines[:len(insertLines)-1]
				}
				for _, line := range insertLines {
					hunkLines = append(hunkLines, "+"+line)
					hunkNewLineCount++
				}
				newLineNum += len(insertLines)
			}
			i++
		}

		contextEnd := oldLineNum + contextLines
		if contextEnd > len(oldLines) {
			contextEnd = len(oldLines)
		}
		for j := oldLineNum; j < contextEnd; j++ {
			hunkLines = append(hunkLines, " "+oldLines[j])
			hunkOldLineCount++
			hunkNewLineCount++
		}

		if i < len(lineDiffs) && lineDiffs[i].Type == diffmp.DiffEqual {
			equalLines := strings.Split(lineDiffs[i].Text, "\n")
			if len(equalLines) > 0 && equalLines[len(equalLines)-1] == "" {
				equalLines = equalLines[:len(equalLines)-1]
			}
			addedContext := 0
			for addedContext < contextLines && addedContext < len(equalLines) {
				oldLineNum++
				newLineNum++
				addedContext++
			}
		}

		result.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunkOldStart+1, hunkOldLineCount, hunkNewStart+1, hunkNewLineCount))
		for _, line := range hunkLines {
			result.WriteString(line + "\n")
		}
	}

	return result.String(), nil
}

func readFileContentForDiff(hash string) (string, error) {
	f, err := filecontents.OpenFileByHash(hash)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	content := buf.String()
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	return content, nil
}

func generateDiffBlocks(oldContent, newContent string) ([]protos.DiffBlock, error) {
	oldLines := strings.Split(oldContent, "\n")

	dmp := diffmp.New()
	chars1, chars2, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	lineDiffs := dmp.DiffMain(chars1, chars2, false)
	lineDiffs = dmp.DiffCharsToLines(lineDiffs, lineArray)

	var blocks []protos.DiffBlock
	contextLines := 3
	oldLineNum := 0
	newLineNum := 0

	i := 0
	for i < len(lineDiffs) {
		for i < len(lineDiffs) && lineDiffs[i].Type == diffmp.DiffEqual {
			equalLines := strings.Split(lineDiffs[i].Text, "\n")
			if len(equalLines) > 0 && equalLines[len(equalLines)-1] == "" {
				equalLines = equalLines[:len(equalLines)-1]
			}
			oldLineNum += len(equalLines)
			newLineNum += len(equalLines)
			i++
		}

		if i >= len(lineDiffs) {
			break
		}

		hunkOldStart := oldLineNum - contextLines
		if hunkOldStart < 0 {
			hunkOldStart = 0
		}
		hunkNewStart := newLineNum - contextLines
		if hunkNewStart < 0 {
			hunkNewStart = 0
		}

		var contextBefore []string
		for j := hunkOldStart; j < oldLineNum && j < len(oldLines); j++ {
			contextBefore = append(contextBefore, oldLines[j])
		}

		hunkOldLineCount := oldLineNum - hunkOldStart
		hunkNewLineCount := newLineNum - hunkNewStart

		var removed, added []string

		for i < len(lineDiffs) && lineDiffs[i].Type != diffmp.DiffEqual {
			switch lineDiffs[i].Type {
			case diffmp.DiffDelete:
				deleteLines := strings.Split(lineDiffs[i].Text, "\n")
				if len(deleteLines) > 0 && deleteLines[len(deleteLines)-1] == "" {
					deleteLines = deleteLines[:len(deleteLines)-1]
				}
				removed = append(removed, deleteLines...)
				hunkOldLineCount += len(deleteLines)
				oldLineNum += len(deleteLines)
			case diffmp.DiffInsert:
				insertLines := strings.Split(lineDiffs[i].Text, "\n")
				if len(insertLines) > 0 && insertLines[len(insertLines)-1] == "" {
					insertLines = insertLines[:len(insertLines)-1]
				}
				added = append(added, insertLines...)
				hunkNewLineCount += len(insertLines)
				newLineNum += len(insertLines)
			}
			i++
		}

		startContextAfter := oldLineNum
		if startContextAfter > len(oldLines) {
			startContextAfter = len(oldLines)
		}
		contextEnd := startContextAfter + contextLines
		if contextEnd > len(oldLines) {
			contextEnd = len(oldLines)
		}
		var contextAfter []string
		for j := startContextAfter; j < contextEnd; j++ {
			contextAfter = append(contextAfter, oldLines[j])
			hunkOldLineCount++
			hunkNewLineCount++
		}

		if i < len(lineDiffs) && lineDiffs[i].Type == diffmp.DiffEqual {
			equalLines := strings.Split(lineDiffs[i].Text, "\n")
			if len(equalLines) > 0 && equalLines[len(equalLines)-1] == "" {
				equalLines = equalLines[:len(equalLines)-1]
			}
			addedContext := 0
			for addedContext < contextLines && addedContext < len(equalLines) {
				oldLineNum++
				newLineNum++
				addedContext++
			}
		}

		blocks = append(blocks, protos.DiffBlock{
			Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
			Lines: []string{fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunkOldStart+1, hunkOldLineCount, hunkNewStart+1, hunkNewLineCount)},
		})

		if len(contextBefore) > 0 {
			blocks = append(blocks, protos.DiffBlock{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_UNCHANGED,
				Lines: contextBefore,
			})
		}

		if len(removed) > 0 {
			blocks = append(blocks, protos.DiffBlock{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED,
				Lines: removed,
			})
		}

		if len(added) > 0 {
			blocks = append(blocks, protos.DiffBlock{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED,
				Lines: added,
			})
		}

		if len(contextAfter) > 0 {
			blocks = append(blocks, protos.DiffBlock{
				Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_UNCHANGED,
				Lines: contextAfter,
			})
		}
	}

	return blocks, nil
}

func GenerateDiffBlocks(oldContent, newContent string) ([]protos.DiffBlock, error) {
	return generateDiffBlocks(oldContent, newContent)
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

	var rev1Opt, rev2Opt *string
	if req.Rev1 != nil {
		rev1Opt = req.Rev1
	}
	if req.Rev2 != nil {
		rev2Opt = req.Rev2
	}

	var currentChangeId *int64
	if req.CheckedOutChangeId != nil {
		currentChangeId = req.CheckedOutChangeId
	}

	diffs, err := ResolveDiff(ctx, req.RepoId, rev1Opt, rev2Opt, currentChangeId)
	if err != nil {
		return fmt.Errorf("resolve diff: %w", err)
	}

	for _, diff := range diffs {
		oldHash := base64.URLEncoding.EncodeToString(diff.OldContentHash)
		newHash := base64.URLEncoding.EncodeToString(diff.NewContentHash)

		oldHashShort := oldHash
		if len(oldHash) > 7 {
			oldHashShort = oldHash[:7]
		}
		newHashShort := newHash
		if len(newHash) > 7 {
			newHashShort = newHash[:7]
		}

		var oldLineCount, newLineCount int32

		switch diff.Status {
		case protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED:
			content, err := readFileContentForDiff(newHash)
			if err != nil {
				return fmt.Errorf("read new file content: %w", err)
			}
			lines := strings.Split(content, "\n")
			newLineCount = int32(len(lines))

		case protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED:
			content, err := readFileContentForDiff(oldHash)
			if err != nil {
				return fmt.Errorf("read old file content: %w", err)
			}
			lines := strings.Split(content, "\n")
			oldLineCount = int32(len(lines))

		case protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED:
			oldContent, err := readFileContentForDiff(oldHash)
			if err != nil {
				return fmt.Errorf("read old file content: %w", err)
			}
			newContent, err := readFileContentForDiff(newHash)
			if err != nil {
				return fmt.Errorf("read new file content: %w", err)
			}
			oldLines := strings.Split(oldContent, "\n")
			newLines := strings.Split(newContent, "\n")
			oldLineCount = int32(len(oldLines))
			newLineCount = int32(len(newLines))
		}

		if err := stream.Send(&protos.DiffResponse{
			Payload: &protos.DiffResponse_FileHeader{
				FileHeader: &protos.DiffFileHeader{
					Path:          diff.Path,
					OldChangeName: diff.OldChangeName,
					NewChangeName: diff.NewChangeName,
					Status:        diff.Status,
					OldHash:       oldHashShort,
					NewHash:       newHashShort,
					OldLineCount:  oldLineCount,
					NewLineCount:  newLineCount,
				},
			},
		}); err != nil {
			return fmt.Errorf("send file header: %w", err)
		}

		switch diff.Status {
		case protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED:
			content, err := readFileContentForDiff(newHash)
			if err != nil {
				return fmt.Errorf("read new file content: %w", err)
			}
			lines := strings.Split(content, "\n")

			if err := stream.Send(&protos.DiffResponse{
				Payload: &protos.DiffResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{"new file mode 100644"},
					},
				},
			}); err != nil {
				return fmt.Errorf("send metadata block: %w", err)
			}

			if err := stream.Send(&protos.DiffResponse{
				Payload: &protos.DiffResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED,
						Lines: lines,
					},
				},
			}); err != nil {
				return fmt.Errorf("send added block: %w", err)
			}

		case protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED:
			content, err := readFileContentForDiff(oldHash)
			if err != nil {
				return fmt.Errorf("read old file content: %w", err)
			}
			lines := strings.Split(content, "\n")

			if err := stream.Send(&protos.DiffResponse{
				Payload: &protos.DiffResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{"deleted file mode 100644"},
					},
				},
			}); err != nil {
				return fmt.Errorf("send metadata block: %w", err)
			}

			if err := stream.Send(&protos.DiffResponse{
				Payload: &protos.DiffResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED,
						Lines: lines,
					},
				},
			}); err != nil {
				return fmt.Errorf("send removed block: %w", err)
			}

		case protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY:
			if err := stream.Send(&protos.DiffResponse{
				Payload: &protos.DiffResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{"Binary files differ"},
					},
				},
			}); err != nil {
				return fmt.Errorf("send binary metadata: %w", err)
			}

		case protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED:
			oldContent, err := readFileContentForDiff(oldHash)
			if err != nil {
				return fmt.Errorf("read old file content: %w", err)
			}
			newContent, err := readFileContentForDiff(newHash)
			if err != nil {
				return fmt.Errorf("read new file content: %w", err)
			}

			blocks, err := generateDiffBlocks(oldContent, newContent)
			if err != nil {
				return fmt.Errorf("generate diff blocks: %w", err)
			}

			for _, block := range blocks {
				if err := stream.Send(&protos.DiffResponse{
					Payload: &protos.DiffResponse_DiffBlock{
						DiffBlock: &block,
					},
				}); err != nil {
					return fmt.Errorf("send diff block: %w", err)
				}
			}
		}

		if err := stream.Send(&protos.DiffResponse{
			Payload: &protos.DiffResponse_EndOfFile{
				EndOfFile: &protos.EndOfFile{},
			},
		}); err != nil {
			return fmt.Errorf("send end of file: %w", err)
		}
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
	remoteFiles, err := db.Q.GetFilesForChange(ctx, changeId)
	if err != nil {
		return fmt.Errorf("get files for change: %w", err)
	}

	remoteFileMap := make(map[string]db.GetFilesForChangeRow)
	for _, f := range remoteFiles {
		remoteFileMap[f.Name] = f
	}

	allPaths := make(map[string]bool)
	for path := range localFiles {
		allPaths[path] = true
	}
	for path := range remoteFileMap {
		allPaths[path] = true
	}

	change, err := db.Q.GetChange(ctx, changeId)
	if err != nil {
		return fmt.Errorf("get change: %w", err)
	}

	contentRequests := make(map[string]bool)

	for path := range allPaths {
		localFile, existsLocal := localFiles[path]
		remoteFile, existsRemote := remoteFileMap[path]

		if !existsRemote {
			contentRequests[path] = true
		} else if !existsLocal {
			continue
		} else {
			if !bytes.Equal(localFile.ContentHash, remoteFile.ContentHash) {
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
		remoteFile, existsRemote := remoteFileMap[path]

		if !existsRemote {
			newHash := base64.URLEncoding.EncodeToString(localFile.ContentHash)
			oldHashShort := ""
			newHashShort := newHash
			if len(newHash) > 7 {
				newHashShort = newHash[:7]
			}

			content := localFileContents[path]
			lines := strings.Split(content, "\n")
			newLineCount := int32(len(lines))

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_FileHeader{
					FileHeader: &protos.DiffFileHeader{
						Path:          path,
						OldChangeName: change.Name,
						NewChangeName: "local",
						Status:        protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED,
						OldHash:       oldHashShort,
						NewHash:       newHashShort,
						OldLineCount:  0,
						NewLineCount:  newLineCount,
					},
				},
			}); err != nil {
				return fmt.Errorf("send file header: %w", err)
			}

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{"new file mode 100644"},
					},
				},
			}); err != nil {
				return fmt.Errorf("send metadata block: %w", err)
			}

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED,
						Lines: lines,
					},
				},
			}); err != nil {
				return fmt.Errorf("send added block: %w", err)
			}

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_EndOfFile{
					EndOfFile: &protos.EndOfFile{},
				},
			}); err != nil {
				return fmt.Errorf("send end of file: %w", err)
			}

		} else if !existsLocal {
			oldHash := base64.URLEncoding.EncodeToString(remoteFile.ContentHash)
			oldHashShort := oldHash
			if len(oldHash) > 7 {
				oldHashShort = oldHash[:7]
			}
			newHashShort := ""

			content, err := readFileContentForDiff(oldHash)
			if err != nil {
				return fmt.Errorf("read old file content: %w", err)
			}
			lines := strings.Split(content, "\n")
			oldLineCount := int32(len(lines))

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_FileHeader{
					FileHeader: &protos.DiffFileHeader{
						Path:          path,
						OldChangeName: change.Name,
						NewChangeName: "local",
						Status:        protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED,
						OldHash:       oldHashShort,
						NewHash:       newHashShort,
						OldLineCount:  oldLineCount,
						NewLineCount:  0,
					},
				},
			}); err != nil {
				return fmt.Errorf("send file header: %w", err)
			}

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
						Lines: []string{"deleted file mode 100644"},
					},
				},
			}); err != nil {
				return fmt.Errorf("send metadata block: %w", err)
			}

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_DiffBlock{
					DiffBlock: &protos.DiffBlock{
						Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED,
						Lines: lines,
					},
				},
			}); err != nil {
				return fmt.Errorf("send removed block: %w", err)
			}

			if err := stream.Send(&protos.DiffLocalResponse{
				Payload: &protos.DiffLocalResponse_EndOfFile{
					EndOfFile: &protos.EndOfFile{},
				},
			}); err != nil {
				return fmt.Errorf("send end of file: %w", err)
			}

		} else {
			if !bytes.Equal(localFile.ContentHash, remoteFile.ContentHash) || (localFile.Executable != nil && *localFile.Executable != remoteFile.Executable) {
				oldHash := base64.URLEncoding.EncodeToString(remoteFile.ContentHash)
				newHash := base64.URLEncoding.EncodeToString(localFile.ContentHash)

				oldHashShort := oldHash
				if len(oldHash) > 7 {
					oldHashShort = oldHash[:7]
				}
				newHashShort := newHash
				if len(newHash) > 7 {
					newHashShort = newHash[:7]
				}

				isBinary1, err := isBinaryFile(oldHash)
				if err != nil {
					return fmt.Errorf("check if file %q is binary: %w", path, err)
				}
				isBinary2 := false
				if bytes.Equal(localFile.ContentHash, remoteFile.ContentHash) {
					isBinary2 = isBinary1
				} else {
					content := localFileContents[path]
					for i := 0; i < len(content) && i < 8192; i++ {
						if content[i] == 0 {
							isBinary2 = true
							break
						}
					}
				}

				if isBinary1 || isBinary2 {
					if err := stream.Send(&protos.DiffLocalResponse{
						Payload: &protos.DiffLocalResponse_FileHeader{
							FileHeader: &protos.DiffFileHeader{
								Path:          path,
								OldChangeName: change.Name,
								NewChangeName: "local",
								Status:        protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY,
								OldHash:       oldHashShort,
								NewHash:       newHashShort,
							},
						},
					}); err != nil {
						return fmt.Errorf("send file header: %w", err)
					}

					if err := stream.Send(&protos.DiffLocalResponse{
						Payload: &protos.DiffLocalResponse_DiffBlock{
							DiffBlock: &protos.DiffBlock{
								Type:  protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA,
								Lines: []string{"Binary files differ"},
							},
						},
					}); err != nil {
						return fmt.Errorf("send binary metadata: %w", err)
					}

					if err := stream.Send(&protos.DiffLocalResponse{
						Payload: &protos.DiffLocalResponse_EndOfFile{
							EndOfFile: &protos.EndOfFile{},
						},
					}); err != nil {
						return fmt.Errorf("send end of file: %w", err)
					}

				} else {
					oldContent, err := readFileContentForDiff(oldHash)
					if err != nil {
						return fmt.Errorf("read old file content: %w", err)
					}
					newContent := localFileContents[path]

					oldLines := strings.Split(oldContent, "\n")
					newLines := strings.Split(newContent, "\n")
					oldLineCount := int32(len(oldLines))
					newLineCount := int32(len(newLines))

					if err := stream.Send(&protos.DiffLocalResponse{
						Payload: &protos.DiffLocalResponse_FileHeader{
							FileHeader: &protos.DiffFileHeader{
								Path:          path,
								OldChangeName: change.Name,
								NewChangeName: "local",
								Status:        protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED,
								OldHash:       oldHashShort,
								NewHash:       newHashShort,
								OldLineCount:  oldLineCount,
								NewLineCount:  newLineCount,
							},
						},
					}); err != nil {
						return fmt.Errorf("send file header: %w", err)
					}

					blocks, err := generateDiffBlocks(oldContent, newContent)
					if err != nil {
						return fmt.Errorf("generate diff blocks: %w", err)
					}

					for i := range blocks {
						if err := stream.Send(&protos.DiffLocalResponse{
							Payload: &protos.DiffLocalResponse_DiffBlock{
								DiffBlock: &blocks[i],
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
				}
			}
		}
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
