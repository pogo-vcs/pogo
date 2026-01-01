package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devsisters/go-diff3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pogo-vcs/pogo/compressions"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/pogo-vcs/pogo/server/ci"
	"github.com/pogo-vcs/pogo/server/env"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func (a *Server) CheckNeededFiles(ctx context.Context, req *protos.CheckNeededFilesRequest) (*protos.CheckNeededFilesResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	// Check which file hashes don't exist in storage
	var neededHashes [][]byte
	for _, hash := range req.FileHashes {
		hashStr := base64.URLEncoding.EncodeToString(hash)
		filePath := filecontents.GetFilePathFromHash(hashStr)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			neededHashes = append(neededHashes, hash)
		}
	}

	return &protos.CheckNeededFilesResponse{
		NeededHashes: neededHashes,
	}, nil
}

func (a *Server) Init(ctx context.Context, req *protos.InitRequest) (*protos.InitResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Get or create user from auth token
	userId, err := getUserIdFromAuth(ctx, req.Auth)
	if err != nil {
		return nil, fmt.Errorf("authenticate user: %w", err)
	}

	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("open db transaction: %w", err)
	}
	defer tx.Close()

	repoId, err := tx.CreateRepository(ctx, req.RepoName, req.Public)
	if err != nil {
		return nil, fmt.Errorf("create repository: %w", err)
	}

	// Grant access to the repository creator
	if userId != nil {
		if err := tx.GrantRepositoryAccess(ctx, repoId, *userId); err != nil {
			return nil, fmt.Errorf("grant repository access: %w", err)
		}
	}

	changeName, err := tx.GenerateChangeName(ctx, repoId)
	if err != nil {
		return nil, fmt.Errorf("generate change name: %w", err)
	}

	changeId, err := tx.CreateInitChange(ctx,
		repoId, changeName, userId)
	if err != nil {
		return nil, fmt.Errorf("create init change: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &protos.InitResponse{
		RepoId:   repoId,
		ChangeId: changeId,
	}, nil
}

// func (a *Server) PushFull(stream protos.Pogo_PushFullServer) error {
func (a *Server) PushFull(stream grpc.ClientStreamingServer[protos.PushFullRequest, protos.PushFullResponse]) error {
	var previousFiles []db.GetChangeFilesRow

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	if err := func() error {
		gcMutex.RLock()
		defer gcMutex.RUnlock()

		tx, err := db.Q.Begin(ctx)
		if err != nil {
			return fmt.Errorf("open db transaction: %w", err)
		}
		defer tx.Close()

		// read auth
		req, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("recv request auth: %w", err)
		}
		auth, ok := req.Payload.(*protos.PushFullRequest_Auth)
		if !ok || auth == nil || auth.Auth == nil {
			return errors.New("invalid request auth payload")
		}

		// read change id
		req, err = stream.Recv()
		if err != nil {
			return fmt.Errorf("recv request change id: %w", err)
		}
		changeId, ok := req.Payload.(*protos.PushFullRequest_ChangeId)
		if !ok || changeId == nil {
			return errors.New("invalid request change id payload")
		}

		// read force flag
		req, err = stream.Recv()
		if err != nil {
			return fmt.Errorf("recv request force flag: %w", err)
		}
		forceFlag, ok := req.Payload.(*protos.PushFullRequest_Force)
		if !ok || forceFlag == nil {
			return errors.New("invalid request force flag payload")
		}

		// Get the repository ID from the change
		change, err := tx.GetChange(ctx, changeId.ChangeId)
		if err != nil {
			return fmt.Errorf("get change: %w", err)
		}

		// Check repository access
		userId, err := checkRepositoryAccessFromAuth(ctx, auth.Auth, change.RepositoryID)
		if err != nil {
			return fmt.Errorf("check repository access: %w", err)
		}

		// Check if change is readonly
		var shouldRejectModifications bool
		if !forceFlag.Force {
			isReadonly, err := tx.IsReadonly(ctx, changeId.ChangeId, userId)
			if err != nil {
				return fmt.Errorf("check readonly: %w", err)
			}
			shouldRejectModifications = isReadonly
		}

		// Get files currently in the change before clearing them
		// These are potential candidates for garbage collection
		previousFiles, err = tx.GetChangeFiles(ctx, changeId.ChangeId)
		if err != nil {
			return fmt.Errorf("get current change files: %w", err)
		}

		if err := tx.ClearChangeFiles(ctx, changeId.ChangeId); err != nil {
			return fmt.Errorf("clear change files: %w", err)
		}

		tempDir, err := os.MkdirTemp("", "pogo-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tempDir)

		fileMeta := make(map[string]*protos.FileHeader)
		filesWithContent := make(map[string]bool)
	files_loop:
		for {
			req, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("recv request file header: %w", err)
			}
			var fileHeader *protos.FileHeader
			var hasContent bool
			// next file or end of files?
			switch v := req.Payload.(type) {
			case *protos.PushFullRequest_FileHeader:
				if v == nil || v.FileHeader == nil {
					return errors.New("invalid request file header payload")
				}
				fileHeader = v.FileHeader
			case *protos.PushFullRequest_EndOfFiles:
				break files_loop
			default:
				return fmt.Errorf("invalid request payload %T", v)
			}

			relPath := filepath.FromSlash(fileHeader.Name)
			fileMeta[relPath] = fileHeader

			// Check if content follows
			req, err = stream.Recv()
			if err != nil {
				return fmt.Errorf("recv has_content flag: %w", err)
			}
			switch v := req.Payload.(type) {
			case *protos.PushFullRequest_HasContent:
				hasContent = v.HasContent
			default:
				return fmt.Errorf("expected has_content flag, got %T", v)
			}

			if hasContent {
				// Reject if this is a readonly change and modifications are being made
				if shouldRejectModifications {
					return errors.New("cannot push to readonly change (has bookmarks, children, or different author). Use --force to override")
				}

				// File content follows, read and store it
				absPath := filepath.Join(tempDir, relPath)
				_ = os.MkdirAll(filepath.Dir(absPath), 0755)
				f, err := os.Create(absPath)
				if err != nil {
					return fmt.Errorf("create file %s: %w", absPath, err)
				}

				if _, err := io.Copy(f, PushFull_StreamReader{stream}); err != nil {
					f.Close()
					return fmt.Errorf("copy file %s: %w", absPath, err)
				}
				// Close file immediately after writing to avoid Windows file locking issues
				if err := f.Close(); err != nil {
					return fmt.Errorf("close file %s: %w", absPath, err)
				}
				filesWithContent[relPath] = true
			}
		}

		// Move new files to permanent store
		moved, err := filecontents.MoveAllFiles(tempDir)
		if err != nil {
			return fmt.Errorf("move files to permanent store: %w", err)
		}

		// Process all file metadata
		for relPath, header := range fileMeta {
			exec := false
			if header.Executable != nil {
				exec = *header.Executable
			}

			var hash []byte
			if filesWithContent[relPath] {
				// Use hash from newly stored file
				hash = moved[relPath]
			} else {
				// File already exists, use hash from metadata
				hash = header.ContentHash
			}

			// Check for conflicts
			hasConflicts := filecontents.IsBinaryConflictFileName(relPath)
			if !hasConflicts {
				hashStr := base64.URLEncoding.EncodeToString(hash)
				filePath := filecontents.GetFilePathFromHash(hashStr)
				if hasConflicts = filecontents.IsBinaryConflictFileName(relPath); !hasConflicts {
					hasConflicts, err = filecontents.HasConflictMarkers(filePath)
					if err != nil {
						return fmt.Errorf("check conflict markers for file %s: %w", relPath, err)
					}
				}
			}

			fmt.Printf("AddFileToChange ChangeId: %d, relPath: %s, exec: %t, hash: %x, hasConflicts: %t, hadContent: %t\n", changeId.ChangeId, relPath, exec, hash, hasConflicts, filesWithContent[relPath])
			if err := tx.AddFileToChange(ctx, changeId.ChangeId, relPath, exec, hash, hasConflicts); err != nil {
				return fmt.Errorf("add file %s to change: %w", relPath, err)
			}
		}

		if err := stream.SendAndClose(&protos.PushFullResponse{}); err != nil {
			return fmt.Errorf("send response: %w", err)
		}

		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}

		return nil
	}(); err != nil {
		return err
	}
	// After successful commit, perform garbage collection on potentially orphaned files
	// Release read lock and acquire write lock for GC operations
	func() {
		gcMutex.Lock()
		defer gcMutex.Unlock()

		// Clean up orphaned files from the previous change state
		if len(previousFiles) > 0 {
			if err := cleanupOrphanedFiles(ctx, previousFiles); err != nil {
				// Log error but don't fail the push operation
				fmt.Printf("warning: failed to cleanup orphaned files after push: %v\n", err)
			}
		}
	}()

	return nil
}

func (a *Server) SetBookmark(ctx context.Context, req *protos.SetBookmarkRequest) (*protos.SetBookmarkResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("open db transaction: %w", err)
	}
	defer tx.Close()

	var changeId int64
	if req.ChangeName != nil {
		changeId, err = tx.FindChangeByNameFuzzyUnique(ctx, req.RepoId, *req.ChangeName)
		if err != nil {
			return nil, fmt.Errorf("find change by name: %w", err)
		}
	} else if req.CheckedOutChangeId != nil {
		changeId = *req.CheckedOutChangeId
	} else {
		return nil, errors.New("either change_name or checked_out_change_id must be provided")
	}

	if err := tx.SetBookmark(ctx, req.RepoId, req.BookmarkName, changeId); err != nil {
		return nil, fmt.Errorf("set bookmark: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Execute CI for bookmark push event
	executeCIForBookmarkEvent(ctx, changeId, req.BookmarkName, ci.EventTypePush)

	return &protos.SetBookmarkResponse{}, nil
}

func (a *Server) RemoveBookmark(ctx context.Context, req *protos.RemoveBookmarkRequest) (*protos.RemoveBookmarkResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("open db transaction: %w", err)
	}
	defer tx.Close()

	if err := tx.RemoveBookmark(ctx, req.RepoId, req.BookmarkName); err != nil {
		return nil, fmt.Errorf("remove bookmark: %w", err)
	}

	// Note: We need to execute CI before the bookmark is removed, so we need to get the change ID
	// For now, let's use a simple approach and execute CI with the current repository state
	changeId, err := db.Q.GetBookmark(ctx, req.RepoId, req.BookmarkName)
	if err == nil {
		executeCIForBookmarkEvent(ctx, changeId, req.BookmarkName, ci.EventTypeRemove)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &protos.RemoveBookmarkResponse{}, nil
}

func (a *Server) GetBookmarks(ctx context.Context, req *protos.GetBookmarksRequest) (*protos.GetBookmarksResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	bookmarks, err := db.Q.GetBookmarks(ctx, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("get bookmarks: %w", err)
	}

	protoBookmarks := make([]*protos.Bookmark, len(bookmarks))
	for i, bookmark := range bookmarks {
		protoBookmarks[i] = &protos.Bookmark{
			Name:       bookmark.Bookmark,
			ChangeName: bookmark.ChangeName,
		}
	}

	return &protos.GetBookmarksResponse{
		Bookmarks: protoBookmarks,
	}, nil
}

func (a *Server) createSingleParentChange(ctx context.Context, tx *db.TxQueries, req *protos.NewChangeRequest, parentChangeId int64, userId *int32) (*protos.NewChangeResponse, error) {
	// Generate new change name
	changeName, err := tx.GenerateChangeName(ctx, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("generate change name: %w", err)
	}

	// Create the new change
	changeId, err := tx.CreateChange(ctx,
		req.RepoId,
		changeName,
		req.Description,
		userId)
	if err != nil {
		return nil, fmt.Errorf("create change: %w", err)
	}

	// Set parent relationship
	if err := tx.SetParent(ctx, changeId, &parentChangeId); err != nil {
		return nil, fmt.Errorf("set parent: %w", err)
	}

	// Set depth from parent
	if err := tx.SetDepthFromParent(ctx, changeId, parentChangeId); err != nil {
		return nil, fmt.Errorf("set depth from parent: %w", err)
	}

	// Copy files from parent change
	if err := tx.CopyChangeFiles(ctx, changeId, parentChangeId); err != nil {
		return nil, fmt.Errorf("copy change files: %w", err)
	}

	return &protos.NewChangeResponse{
		ChangeId:   changeId,
		ChangeName: changeName,
	}, nil
}

type emptyReader struct{}

func (emptyReader) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (a *Server) createMergeChange(ctx context.Context, tx *db.TxQueries, req *protos.NewChangeRequest, parentChangeIds []int64, userId *int32) (*protos.NewChangeResponse, error) {
	if len(parentChangeIds) != 2 {
		return nil, errors.New("merge commits must have exactly two parents, octopus merge is not supported")
	}

	lcaChangeId, err := tx.FindLCA(ctx, parentChangeIds[0], parentChangeIds[1])
	if err != nil {
		return nil, fmt.Errorf("find lca of %d and %d: %w", parentChangeIds[0], parentChangeIds[1], err)
	}

	fmt.Printf("A: %d B: %d LCA: %d\n", parentChangeIds[0], parentChangeIds[1], lcaChangeId)

	newChangeId, changeName, err := a.createMergeChangeRecord(ctx, tx, req, parentChangeIds, userId)
	if err != nil {
		return nil, err
	}

	changeA, changeB, err := a.getParentChanges(ctx, tx, parentChangeIds)
	if err != nil {
		return nil, err
	}

	mergeFiles, err := tx.GetThreeWayMergeFiles(ctx, lcaChangeId, parentChangeIds[0], parentChangeIds[1])
	if err != nil {
		return nil, fmt.Errorf("get three way merge files: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "pogo-merge-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir for merge: %w", err)
	}
	defer os.RemoveAll(tempDir)

	for _, mergeFile := range mergeFiles {
		fmt.Printf("mergeFile: %+v\n", mergeFile)
		if err := a.processMergeFile(ctx, tx, newChangeId, mergeFile, changeA, changeB, tempDir); err != nil {
			return nil, err
		}
	}

	return &protos.NewChangeResponse{
		ChangeId:   newChangeId,
		ChangeName: changeName,
	}, nil
}

func (a *Server) createMergeChangeRecord(ctx context.Context, tx *db.TxQueries, req *protos.NewChangeRequest, parentChangeIds []int64, userId *int32) (int64, string, error) {
	changeName, err := tx.GenerateChangeName(ctx, req.RepoId)
	if err != nil {
		return 0, "", fmt.Errorf("generate change name: %w", err)
	}

	newChangeId, err := tx.CreateChange(ctx,
		req.RepoId,
		changeName,
		req.Description,
		userId)
	if err != nil {
		return 0, "", fmt.Errorf("create change: %w", err)
	}

	if err := tx.SetParent(ctx, newChangeId, &parentChangeIds[0]); err != nil {
		return 0, "", fmt.Errorf("set parent: %w", err)
	}
	if err := tx.SetParent(ctx, newChangeId, &parentChangeIds[1]); err != nil {
		return 0, "", fmt.Errorf("set parent: %w", err)
	}

	if err := tx.SetDepthFromParent(ctx, newChangeId, parentChangeIds[0]); err != nil {
		return 0, "", fmt.Errorf("set depth from parent: %w", err)
	}
	if err := tx.SetDepthFromParent(ctx, newChangeId, parentChangeIds[1]); err != nil {
		return 0, "", fmt.Errorf("set depth from parent: %w", err)
	}

	return newChangeId, changeName, nil
}

func (a *Server) getParentChanges(ctx context.Context, tx *db.TxQueries, parentChangeIds []int64) (*db.GetChangeRow, *db.GetChangeRow, error) {
	changeA, err := tx.GetChange(ctx, parentChangeIds[0])
	if err != nil {
		return nil, nil, fmt.Errorf("get change A: %w", err)
	}
	changeB, err := tx.GetChange(ctx, parentChangeIds[1])
	if err != nil {
		return nil, nil, fmt.Errorf("get change B: %w", err)
	}
	return &changeA, &changeB, nil
}

func (a *Server) processMergeFile(ctx context.Context, tx *db.TxQueries, newChangeId int64, mergeFile db.GetThreeWayMergeFilesRow, changeA, changeB *db.GetChangeRow, tempDir string) error {
	aExists := mergeFile.AContentHash != nil
	oExists := mergeFile.LcaContentHash != nil
	bExists := mergeFile.BContentHash != nil

	if a.shouldSkipFile(mergeFile, aExists, oExists, bExists) {
		return nil
	}

	if a.isSimpleCase(aExists, oExists, bExists) {
		return a.handleSimpleCase(ctx, tx, newChangeId, mergeFile, aExists, bExists)
	}

	return a.handleThreeWayMerge(ctx, tx, newChangeId, mergeFile, changeA, changeB, tempDir, aExists, oExists, bExists)
}

func (a *Server) shouldSkipFile(mergeFile db.GetThreeWayMergeFilesRow, aExists, oExists, bExists bool) bool {
	if oExists && aExists && !bExists && bytes.Equal(mergeFile.AContentHash, mergeFile.LcaContentHash) {
		return true
	}
	if oExists && !aExists && bExists && bytes.Equal(mergeFile.BContentHash, mergeFile.LcaContentHash) {
		return true
	}
	if !aExists && !bExists {
		return true
	}
	return false
}

func (a *Server) isSimpleCase(aExists, oExists, bExists bool) bool {
	return (!oExists && aExists && !bExists) || (!oExists && !aExists && bExists)
}

func (a *Server) handleSimpleCase(ctx context.Context, tx *db.TxQueries, newChangeId int64, mergeFile db.GetThreeWayMergeFilesRow, aExists, bExists bool) error {
	var hash []byte
	var executable bool

	if aExists {
		hash = mergeFile.AContentHash
		executable = *mergeFile.AExecutable
	} else {
		hash = mergeFile.BContentHash
		executable = *mergeFile.BExecutable
	}

	hashStr := base64.URLEncoding.EncodeToString(hash)
	filePath := filecontents.GetFilePathFromHash(hashStr)
	var (
		hasConflicts bool
		err          error
	)
	if hasConflicts = filecontents.IsBinaryConflictFileName(mergeFile.FileName); !hasConflicts {
		hasConflicts, err = filecontents.HasConflictMarkers(filePath)
		if err != nil {
			return fmt.Errorf("check conflict markers for file %s: %w", filePath, err)
		}
	}

	return tx.AddFileToChange(ctx, newChangeId, mergeFile.FileName, executable, hash, hasConflicts)
}

func (a *Server) handleThreeWayMerge(ctx context.Context, tx *db.TxQueries, newChangeId int64, mergeFile db.GetThreeWayMergeFilesRow, changeA, changeB *db.GetChangeRow, tempDir string, aExists, oExists, bExists bool) error {
	aReader, aType, err := a.getFileReader(mergeFile.AContentHash, aExists)
	if err != nil {
		return fmt.Errorf("get file reader A %s: %w", mergeFile.FileName, err)
	}
	defer a.closeReader(aReader)

	oReader, oType, err := a.getFileReader(mergeFile.LcaContentHash, oExists)
	if err != nil {
		return fmt.Errorf("get file reader O %s: %w", mergeFile.FileName, err)
	}
	defer a.closeReader(oReader)

	bReader, bType, err := a.getFileReader(mergeFile.BContentHash, bExists)
	if err != nil {
		return fmt.Errorf("get file reader B %s: %w", mergeFile.FileName, err)
	}
	defer a.closeReader(bReader)

	executable := threeWayMergeExecutable(mergeFile.AExecutable, mergeFile.LcaExecutable, mergeFile.BExecutable)
	mType := filecontents.ThreeWayMergeResultType(aType, oType, bType)

	if mType.Binary {
		return a.handleBinaryConflict(ctx, tx, newChangeId, mergeFile, changeA, changeB, aExists, oExists, bExists, executable)
	}

	return a.handleTextMerge(ctx, tx, newChangeId, mergeFile, changeA, changeB, tempDir, aReader, oReader, bReader, mType, executable)
}

func (a *Server) getFileReader(contentHash []byte, exists bool) (io.Reader, filecontents.FileType, error) {
	if !exists {
		return emptyReader{}, filecontents.FileType{}, nil
	}

	hashStr := base64.URLEncoding.EncodeToString(contentHash)
	file, fileType, err := filecontents.OpenFileByHashWithType(hashStr)
	if err != nil {
		return nil, filecontents.FileType{}, err
	}

	return fileType.CanonicalizeReader(file), fileType, nil
}

func (a *Server) closeReader(reader io.Reader) {
	if closer, ok := reader.(io.Closer); ok {
		closer.Close()
	}
}

func (a *Server) handleBinaryConflict(ctx context.Context, tx *db.TxQueries, newChangeId int64, mergeFile db.GetThreeWayMergeFilesRow, changeA, changeB *db.GetChangeRow, aExists, oExists, bExists bool, executable bool) error {
	if oExists {
		if err := tx.AddFileToChange(ctx, newChangeId, mergeFile.FileName, executable, mergeFile.LcaContentHash, true); err != nil {
			return fmt.Errorf("add LCA file %s to change: %w", mergeFile.FileName, err)
		}
	}

	if aExists {
		aFileName := fmt.Sprintf("%s.%s", mergeFile.FileName, changeA.Name)
		if err := tx.AddFileToChange(ctx, newChangeId, aFileName, executable, mergeFile.AContentHash, true); err != nil {
			return fmt.Errorf("add A file %s to change: %w", aFileName, err)
		}
	}

	if bExists {
		bFileName := fmt.Sprintf("%s.%s", mergeFile.FileName, changeB.Name)
		if err := tx.AddFileToChange(ctx, newChangeId, bFileName, executable, mergeFile.BContentHash, true); err != nil {
			return fmt.Errorf("add B file %s to change: %w", bFileName, err)
		}
	}

	return nil
}

func (a *Server) handleTextMerge(ctx context.Context, tx *db.TxQueries, newChangeId int64, mergeFile db.GetThreeWayMergeFilesRow, changeA, changeB *db.GetChangeRow, tempDir string, aReader, oReader, bReader io.Reader, mType filecontents.FileType, executable bool) error {
	mergeResult, err := diff3.Merge(aReader, oReader, bReader, true, changeA.Name, changeB.Name)
	if err != nil {
		return fmt.Errorf("merge file %s: %w", mergeFile.FileName, err)
	}

	absPath := filepath.Join(tempDir, filepath.FromSlash(mergeFile.FileName))
	_ = os.MkdirAll(filepath.Dir(absPath), 0755)

	mergedFile, err := os.Create(absPath)
	if err != nil {
		return fmt.Errorf("create merged file %s: %w", absPath, err)
	}

	if _, err := io.Copy(mergedFile, mType.TypeReader(mergeResult.Result)); err != nil {
		mergedFile.Close()
		return fmt.Errorf("write merged content %s: %w", absPath, err)
	}
	mergedFile.Close()

	hasConflicts := mergeResult.Conflicts
	if !hasConflicts {
		if hasConflicts = filecontents.IsBinaryConflictFileName(mergeFile.FileName); !hasConflicts {
			if hasConflicts, err = filecontents.HasConflictMarkers(absPath); err != nil {
				return fmt.Errorf("check conflict markers for file %s: %w", mergeFile.FileName, err)
			}
		}
	}

	hash, err := filecontents.StoreFile(absPath)
	if err != nil {
		return fmt.Errorf("store merged file %s: %w", mergeFile.FileName, err)
	}

	return tx.AddFileToChange(ctx, newChangeId, mergeFile.FileName, executable, hash, hasConflicts)
}

func threeWayMergeExecutable(a, o, b *bool) bool {
	base := false
	if o != nil {
		base = *o
	}

	if a != nil && *a != base {
		return *a
	}

	if b != nil && *b != base {
		return *b
	}

	return base
}

func (a *Server) NewChange(ctx context.Context, req *protos.NewChangeRequest) (*protos.NewChangeResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access and get user ID
	userId, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("open db transaction: %w", err)
	}
	defer tx.Close()

	// Determine parent change IDs
	var parentChangeIds []int64
	if len(req.ParentChangeNames) > 0 {
		// Use provided parent change names
		for _, parentName := range req.ParentChangeNames {
			parentId, err := tx.FindChangeByNameFuzzyUnique(ctx, req.RepoId, parentName)
			if err != nil {
				return nil, fmt.Errorf("find parent change by name %s: %w", parentName, err)
			}
			parentChangeIds = append(parentChangeIds, parentId)
		}
	} else if req.CheckedOutChangeId != nil {
		// Use checked out change ID as parent
		parentChangeIds = append(parentChangeIds, *req.CheckedOutChangeId)
	} else {
		return nil, errors.New("either parent_change_names or checked_out_change_id must be provided")
	}

	if len(parentChangeIds) == 0 {
		return nil, errors.New("at least one parent change is required")
	}

	var response *protos.NewChangeResponse
	if len(parentChangeIds) == 1 {
		// Single parent commit
		response, err = a.createSingleParentChange(ctx, tx, req, parentChangeIds[0], userId)
	} else {
		// Merge commit
		response, err = a.createMergeChange(ctx, tx, req, parentChangeIds, userId)
	}

	if err != nil {
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return response, nil
}

func (a *Server) GetDescription(ctx context.Context, req *protos.GetDescriptionRequest) (*protos.GetDescriptionResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Get the change to find repository ID
	change, err := db.Q.GetChange(ctx, req.ChangeId)
	if err != nil {
		return nil, fmt.Errorf("get change: %w", err)
	}

	// Check repository access
	_, err = checkRepositoryAccessFromAuth(ctx, req.Auth, change.RepositoryID)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	description, err := db.Q.GetChangeDescription(ctx, req.ChangeId)
	if err != nil {
		return nil, fmt.Errorf("get change description: %w", err)
	}

	response := &protos.GetDescriptionResponse{
		Description: description,
	}
	return response, nil
}

func (a *Server) SetDescription(ctx context.Context, req *protos.SetDescriptionRequest) (*protos.SetDescriptionResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Get the change to find repository ID
	change, err := db.Q.GetChange(ctx, req.ChangeId)
	if err != nil {
		return nil, fmt.Errorf("get change: %w", err)
	}

	// Check repository access
	_, err = checkRepositoryAccessFromAuth(ctx, req.Auth, change.RepositoryID)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	err = db.Q.SetChangeDescription(ctx, req.ChangeId, req.Description)
	if err != nil {
		return nil, fmt.Errorf("set change description: %w", err)
	}

	return &protos.SetDescriptionResponse{}, nil
}

func (a *Server) Log(ctx context.Context, req *protos.LogRequest) (*protos.LogResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	// Get the n newest changes ordered by updated_at
	newestChanges, err := db.Q.GetNewestChanges(ctx, req.RepoId, req.MaxChanges)
	if err != nil {
		return nil, fmt.Errorf("get newest changes: %w", err)
	}

	// Build a set of change IDs we have
	var changeIds []int64
	changeIdSet := make(map[int64]bool)
	for _, c := range newestChanges {
		changeIds = append(changeIds, c.ID)
		changeIdSet[c.ID] = true
	}

	// Find ALL relations for these changes
	// Include relations even if they reference changes not in our set
	var relations []db.GetChangeRelationsForChangesRow
	if len(changeIds) > 0 {
		relations, err = db.Q.GetChangeRelationsForChanges(ctx, changeIds)
		if err != nil {
			return nil, fmt.Errorf("get change relations: %w", err)
		}
	}

	// Build the response
	response := &protos.LogResponse{
		CheckedOutChangeId: req.CheckedOutChangeId,
	}

	// Add changes to response
	for _, change := range newestChanges {
		logChange := &protos.LogChange{
			Id:           change.ID,
			Name:         change.Name,
			UniquePrefix: change.UniquePrefix,
			CreatedAt:    change.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:    change.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		}
		if change.Description != nil {
			logChange.Description = change.Description
		}

		// Get conflict files for this change
		conflictFiles, err := db.Q.GetConflictFilesForChange(ctx, change.ID)
		if err != nil {
			return nil, fmt.Errorf("get conflict files for change %d: %w", change.ID, err)
		}
		logChange.ConflictFiles = conflictFiles

		response.Changes = append(response.Changes, logChange)
	}

	// Add relations to response with adjacency list modification:
	// If a parent is not in our selected changes, replace its name with "~"
	for _, rel := range relations {
		// Child name stays as is (child is always in our set by query design)
		childName := rel.ChildName

		// Parent name: use "~" if parent is not in our selected changes
		parentName := rel.ParentName
		if rel.ParentID != nil && !changeIdSet[*rel.ParentID] {
			parentName = "~"
		}

		logRel := &protos.LogRelation{
			ChildId:    rel.ChildID,
			ChildName:  childName,
			ParentName: parentName,
		}
		if rel.ParentID != nil {
			logRel.ParentId = rel.ParentID
		}
		response.Relations = append(response.Relations, logRel)
	}

	return response, nil
}

func (a *Server) Info(ctx context.Context, req *protos.InfoRequest) (*protos.InfoResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	change, err := db.Q.GetChange(ctx, req.CheckedOutChangeId)
	if err != nil {
		return nil, fmt.Errorf("get change: %w", err)
	}

	resp := &protos.InfoResponse{
		ChangeNamePrefix:  change.UniquePrefix,
		ChangeNameSuffix:  change.Name[len(change.UniquePrefix):],
		ChangeName:        change.Name,
		ChangeDescription: change.Description,
	}

	bookmarks, err := db.Q.GetChangeBookmarks(ctx, req.CheckedOutChangeId)
	if err != nil {
		return nil, fmt.Errorf("get change bookmarks: %w", err)
	}
	resp.Bookmarks = bookmarks

	if conflict, err := db.Q.IsChangeInConflict(ctx, req.CheckedOutChangeId); err != nil {
		return nil, fmt.Errorf("check for conflict: %w", err)
	} else {
		resp.IsInConflict = conflict
	}

	return resp, nil
}

const noDescription = "(no description set)"

func (a *Server) Edit(req *protos.EditRequest, stream grpc.ServerStreamingServer[protos.EditResponse]) error {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	ctx := stream.Context()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return fmt.Errorf("check repository access: %w", err)
	}

	var revision string
	var changeId int64
	var revisionFiles []db.File
	if req.Revision != "" {
		revision = req.Revision
		var err error
		changeId, err = db.Q.FindChangeByNameFuzzyUnique(ctx, req.RepoId, req.Revision)
		if err != nil {
			return fmt.Errorf("revision '%s' not found", req.Revision)
		}
		revisionFiles, err = db.Q.GetRepositoryFilesForRevisionFuzzy(ctx, req.RepoId, revision)
		if err != nil {
			return fmt.Errorf("get repository files for revision: %w", err)
		}
	} else {
		changeId = req.ChangeId
		// Verify the change exists and get its name for ignore matcher
		name, err := db.Q.GetChangeName(ctx, changeId)
		if err != nil {
			return fmt.Errorf("change %d not found: %w", changeId, err)
		}
		revision = name
		revisionFiles, err = db.Q.GetRepositoryFilesForChangeId(ctx, changeId)
		if err != nil {
			return fmt.Errorf("get repository files for change: %w", err)
		}
	}

	// If no files found in revision, it might be an empty revision (which is valid)
	// but we should still proceed with the edit operation

	// Get ignore matcher for the revision
	ignoreMatcher, err := GetRevisionIgnoreMatcher(ctx, GetRevisionIgnoreMatcherParams{req.Revision, changeId, req.RepoId})
	if err != nil {
		return fmt.Errorf("get revision ignore matcher: %w", err)
	}

	// Create maps for efficient lookup
	clientFileSet := make(map[string]bool)
	for _, clientFile := range req.ClientFiles {
		clientFileSet[clientFile] = true
	}

	revisionFileMap := make(map[string]db.File)
	for _, revisionFile := range revisionFiles {
		// Check if file should be ignored - use forward slashes for git paths
		gitPath := strings.Split(filepath.ToSlash(revisionFile.Name), "/")
		isIgnored := ignoreMatcher.Match(gitPath, false)
		if isIgnored {
			continue
		}
		revisionFileMap[revisionFile.Name] = revisionFile
	}

	// Step 1: Send files to delete (client files not in revision and not ignored)
	for clientFile := range clientFileSet {
		if _, exists := revisionFileMap[clientFile]; !exists {
			// Check if client file should be ignored - use forward slashes for git paths
			gitPath := strings.Split(filepath.ToSlash(clientFile), "/")
			isIgnored := ignoreMatcher.Match(gitPath, false)
			if !isIgnored {
				if err := stream.Send(&protos.EditResponse{
					Payload: &protos.EditResponse_FileToDelete{
						FileToDelete: &protos.FileToDelete{
							Name: clientFile,
						},
					},
				}); err != nil {
					return fmt.Errorf("send delete file %s: %w", clientFile, err)
				}
			}
		}
	}

	// Step 2: Send files to add/update
	for fileName, revisionFile := range revisionFileMap {

		// Send file header
		fileHeader := &protos.FileHeader{
			Name: fileName,
		}
		if revisionFile.Executable {
			fileHeader.Executable = &revisionFile.Executable
		}

		if err := stream.Send(&protos.EditResponse{
			Payload: &protos.EditResponse_FileHeader{
				FileHeader: fileHeader,
			},
		}); err != nil {
			return fmt.Errorf("send file header %s: %w", fileName, err)
		}

		// Send file content
		hashStr := base64.URLEncoding.EncodeToString(revisionFile.ContentHash)
		f, _, err := filecontents.OpenFileByHashWithType(hashStr)
		if err != nil {
			return fmt.Errorf("open file %s: %w", fileName, err)
		}
		defer f.Close()

		if _, err := io.Copy(&Edit_StreamWriter{stream}, f); err != nil {
			return fmt.Errorf("send file content %s: %w", fileName, err)
		}

		// Send EOF
		if err := stream.Send(&protos.EditResponse{
			Payload: &protos.EditResponse_Eof{
				Eof: &protos.EOF{},
			},
		}); err != nil {
			return fmt.Errorf("send eof %s: %w", fileName, err)
		}
	}

	// Send end of files
	if err := stream.Send(&protos.EditResponse{
		Payload: &protos.EditResponse_EndOfFiles{
			EndOfFiles: &protos.EndOfFiles{},
		},
	}); err != nil {
		return fmt.Errorf("send end of files: %w", err)
	}

	// Send the change ID that we already found earlier
	if err := stream.Send(&protos.EditResponse{
		Payload: &protos.EditResponse_ChangeId{
			ChangeId: changeId,
		},
	}); err != nil {
		return fmt.Errorf("send change id: %w", err)
	}

	return nil
}

func (a *Server) RemoveChange(ctx context.Context, req *protos.RemoveChangeRequest) (*protos.RemoveChangeResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check repository access
	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("open db transaction: %w", err)
	}
	defer tx.Close()

	changeId, err := tx.FindChangeByNameFuzzyUnique(ctx, req.RepoId, req.ChangeName)
	if err != nil {
		return nil, fmt.Errorf("find change by name %s: %w", req.ChangeName, err)
	}

	if req.KeepChildren {
		// Get parents of the change to be deleted
		parents, err := tx.GetChangeParents(ctx, changeId)
		if err != nil {
			return nil, fmt.Errorf("get change parents: %w", err)
		}

		// Get direct children of the change to be deleted
		children, err := tx.GetChangeChildren(ctx, &changeId)
		if err != nil {
			return nil, fmt.Errorf("get change children: %w", err)
		}

		// First, connect each child to each parent of the deleted change
		for _, child := range children {
			for _, parent := range parents {
				if err := tx.SetParent(ctx, child.ID, &parent.ID); err != nil {
					return nil, fmt.Errorf("set parent for child: %w", err)
				}
				if err := tx.SetDepthFromParent(ctx, child.ID, parent.ID); err != nil {
					return nil, fmt.Errorf("set depth from parent: %w", err)
				}
			}
		}

		// Remove all relations involving the change to be deleted
		if err := tx.RemoveChangeRelations(ctx, changeId); err != nil {
			return nil, fmt.Errorf("remove change relations: %w", err)
		}

		// Delete the change itself
		if err := tx.DeleteChange(ctx, changeId); err != nil {
			return nil, fmt.Errorf("delete change: %w", err)
		}
	} else {
		// Collect all descendants that need to be deleted (deepest first)
		descendants, err := tx.GetAllDescendants(ctx, changeId)
		if err != nil {
			return nil, fmt.Errorf("get all descendants: %w", err)
		}

		// Delete all descendants first (deepest to shallowest)
		for _, descendant := range descendants {
			if err := tx.DeleteChange(ctx, descendant.ChangeID); err != nil {
				return nil, fmt.Errorf("delete descendant change %s: %w", descendant.ChangeName, err)
			}
		}

		// Finally delete the root change
		if err := tx.DeleteChange(ctx, changeId); err != nil {
			return nil, fmt.Errorf("delete change: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &protos.RemoveChangeResponse{}, nil
}

func (a *Server) GetRepositoryInfo(ctx context.Context, req *protos.GetRepositoryInfoRequest) (*protos.GetRepositoryInfoResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Get repository by name
	repository, err := db.Q.GetRepositoryByName(ctx, req.RepoName)
	if err != nil {
		return nil, fmt.Errorf("repository '%s' not found", req.RepoName)
	}

	// Check if repository is public or if user has access
	if !repository.Public {
		_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, repository.ID)
		if err != nil {
			return nil, fmt.Errorf("access denied to repository '%s': %w", req.RepoName, err)
		}
	}

	response := &protos.GetRepositoryInfoResponse{
		RepoId:   repository.ID,
		RepoName: repository.Name,
		IsPublic: repository.Public,
	}

	// Try to get main bookmark
	if mainChangeId, err := db.Q.GetBookmark(ctx, repository.ID, "main"); err == nil {
		if mainChange, err := db.Q.GetChange(ctx, mainChangeId); err == nil {
			response.MainBookmarkChange = &mainChange.Name
		}
	}

	// Try to get root change
	if rootChange, err := db.Q.GetRepositoryRootChange(ctx, repository.ID); err == nil {
		response.RootChange = &rootChange.Name
	}

	return response, nil
}

// generateSecureToken creates a cryptographically secure random token
func generateSecureToken() ([]byte, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate random token: %w", err)
	}
	return token, nil
}

// getPublicAddress returns the public address from environment variable
func getPublicAddress() string {
	return env.PublicAddress
}

func (a *Server) CreateInvite(ctx context.Context, req *protos.CreateInviteRequest) (*protos.CreateInviteResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Authenticate the user
	user, err := getUserFromAuth(ctx, req.Auth)
	if err != nil {
		return nil, fmt.Errorf("authenticate user: %w", err)
	}

	// Generate secure invite token
	inviteToken, err := generateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("generate invite token: %w", err)
	}

	// Calculate expiration time
	expiresAt := pgtype.Timestamptz{
		Time:  time.Now().Add(time.Duration(req.ExpiresInHours) * time.Hour),
		Valid: true,
	}

	// Create invite in database
	_, err = db.Q.CreateInvite(ctx, inviteToken, user.ID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}

	// Generate invite URL (assuming the server is accessible via PUBLIC_ADDRESS env var)
	inviteTokenStr := db.EncodeToken(inviteToken)
	inviteURL := fmt.Sprintf("%s/register?invite=%s", getPublicAddress(), inviteTokenStr)

	return &protos.CreateInviteResponse{
		InviteUrl:   inviteURL,
		InviteToken: inviteTokenStr,
	}, nil
}

func (a *Server) GetInvites(ctx context.Context, req *protos.GetInvitesRequest) (*protos.GetInvitesResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Authenticate the user
	user, err := getUserFromAuth(ctx, req.Auth)
	if err != nil {
		return nil, fmt.Errorf("authenticate user: %w", err)
	}

	// Get all invites created by this user
	invites, err := db.Q.GetAllInvitesByUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("get invites: %w", err)
	}

	// Convert to protobuf format
	var protoInvites []*protos.Invite
	for _, invite := range invites {
		protoInvite := &protos.Invite{
			Token:     db.EncodeToken(invite.Token),
			CreatedAt: invite.CreatedAt.Time.Format(time.RFC3339),
			ExpiresAt: invite.ExpiresAt.Time.Format(time.RFC3339),
		}

		if invite.UsedAt.Valid {
			usedAt := invite.UsedAt.Time.Format(time.RFC3339)
			protoInvite.UsedAt = &usedAt
		}

		if invite.UsedByUsername != nil && *invite.UsedByUsername != "" {
			protoInvite.UsedByUsername = invite.UsedByUsername
		}

		protoInvites = append(protoInvites, protoInvite)
	}

	return &protos.GetInvitesResponse{
		Invites: protoInvites,
	}, nil
}

// cleanupOrphanedFiles removes files from both database and filesystem that are no longer referenced by any change
func cleanupOrphanedFiles(ctx context.Context, candidateFiles []db.GetChangeFilesRow) error {
	if len(candidateFiles) == 0 {
		return nil
	}

	// Extract file IDs for batch processing
	fileIds := make([]int64, len(candidateFiles))
	for i, file := range candidateFiles {
		fileIds[i] = file.ID
	}

	// Start a new transaction for cleanup operations
	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer tx.Close()

	// Check which of these files are actually orphaned (not referenced by any change)
	orphanedFiles, err := db.Q.GetOrphanedFileIds(ctx, fileIds)
	if err != nil {
		return fmt.Errorf("get orphaned file ids: %w", err)
	}

	// Log only if there are actually orphaned files to clean up
	if len(orphanedFiles) > 0 {
		fmt.Printf("GC during push: cleaning up %d orphaned files\n", len(orphanedFiles))
	}

	if len(orphanedFiles) == 0 {
		// No orphaned files, nothing to clean up
		return nil
	}

	// Delete orphaned files from database first
	orphanedFileIds := make([]int64, len(orphanedFiles))
	for i, file := range orphanedFiles {
		orphanedFileIds[i] = file.ID
	}

	if err := tx.DeleteFilesByIds(ctx, orphanedFileIds); err != nil {
		return fmt.Errorf("delete orphaned files from database: %w", err)
	}

	// Commit database transaction before filesystem cleanup
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cleanup transaction: %w", err)
	}

	// Now remove files from filesystem
	var deletedCount int
	var totalSize int64

	for _, file := range orphanedFiles {
		hashStr := base64.URLEncoding.EncodeToString(file.ContentHash)
		filePath := filecontents.GetFilePathFromHash(hashStr)

		// Get file size before deletion for reporting
		if info, err := os.Stat(filePath); err == nil {
			totalSize += info.Size()
		}

		// Delete the file from storage
		if err := os.Remove(filePath); err == nil {
			deletedCount++
		} else if !os.IsNotExist(err) {
			// Log non-critical filesystem errors but continue
			fmt.Printf("warning: failed to delete file %s: %v\n", filePath, err)
		}
	}

	if deletedCount > 0 {
		fmt.Printf("GC: deleted %d orphaned files during push (%d bytes freed)\n", deletedCount, totalSize)
	}

	return nil
}

func (a *Server) SetRepositoryVisibility(ctx context.Context, req *protos.SetRepositoryVisibilityRequest) (*protos.SetRepositoryVisibilityResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	if err := db.Q.UpdateRepositoryVisibility(ctx, req.RepoId, req.Public); err != nil {
		return nil, fmt.Errorf("update repository visibility: %w", err)
	}

	return &protos.SetRepositoryVisibilityResponse{}, nil
}

func buildCIRunSummary(rowID int32, configFilename, eventType, rev string, pattern *string, reason, taskType string, statusCode int32, success bool, startedAt, finishedAt pgtype.Timestamptz) *protos.CIRunSummary {
	summary := &protos.CIRunSummary{
		Id:             int64(rowID),
		ConfigFilename: configFilename,
		EventType:      eventType,
		Rev:            rev,
		Reason:         reason,
		TaskType:       taskType,
		StatusCode:     statusCode,
		Success:        success,
		StartedAt:      formatTimestamptz(startedAt),
		FinishedAt:     formatTimestamptz(finishedAt),
	}
	if pattern != nil {
		summary.Pattern = proto.String(*pattern)
	}
	return summary
}

func formatTimestamptz(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339)
}

func (a *Server) ListCIRuns(ctx context.Context, req *protos.ListCIRunsRequest) (*protos.ListCIRunsResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	rows, err := db.Q.ListCIRuns(ctx, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("list ci runs: %w", err)
	}

	summaries := make([]*protos.CIRunSummary, 0, len(rows))
	for _, row := range rows {
		summaries = append(summaries, buildCIRunSummary(
			row.ID,
			row.ConfigFilename,
			row.EventType,
			row.Rev,
			row.Pattern,
			row.Reason,
			row.TaskType,
			row.StatusCode,
			row.Success,
			row.StartedAt,
			row.FinishedAt,
		))
	}

	return &protos.ListCIRunsResponse{Runs: summaries}, nil
}

func (a *Server) GetCIRun(ctx context.Context, req *protos.GetCIRunRequest) (*protos.GetCIRunResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	row, err := db.Q.GetCIRun(ctx, req.RepoId, int32(req.RunId))
	if err != nil {
		return nil, fmt.Errorf("get ci run: %w", err)
	}

	decompressedLog, err := compressions.DecompressBytes(row.Log)
	if err != nil {
		return nil, fmt.Errorf("decompress log: %w", err)
	}

	return &protos.GetCIRunResponse{
		Run: buildCIRunSummary(
			row.ID,
			row.ConfigFilename,
			row.EventType,
			row.Rev,
			row.Pattern,
			row.Reason,
			row.TaskType,
			row.StatusCode,
			row.Success,
			row.StartedAt,
			row.FinishedAt,
		),
		Log: string(decompressedLog),
	}, nil
}

func (a *Server) SetSecret(ctx context.Context, req *protos.SetSecretRequest) (*protos.SetSecretResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	if err := db.Q.SetSecret(ctx, req.RepoId, req.Key, req.Value); err != nil {
		return nil, fmt.Errorf("set secret: %w", err)
	}

	return &protos.SetSecretResponse{}, nil
}

func (a *Server) GetSecret(ctx context.Context, req *protos.GetSecretRequest) (*protos.GetSecretResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	value, err := db.Q.GetSecret(ctx, req.RepoId, req.Key)
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	return &protos.GetSecretResponse{Value: value}, nil
}

func (a *Server) GetAllSecrets(ctx context.Context, req *protos.GetAllSecretsRequest) (*protos.GetAllSecretsResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	secrets, err := db.Q.GetAllSecrets(ctx, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("get all secrets: %w", err)
	}

	protoSecrets := make([]*protos.Secret, len(secrets))
	for i, s := range secrets {
		protoSecrets[i] = &protos.Secret{
			Key:   s.Key,
			Value: s.Value,
		}
	}

	return &protos.GetAllSecretsResponse{Secrets: protoSecrets}, nil
}

func (a *Server) DeleteSecret(ctx context.Context, req *protos.DeleteSecretRequest) (*protos.DeleteSecretResponse, error) {
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	_, err := checkRepositoryAccessFromAuth(ctx, req.Auth, req.RepoId)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	if err := db.Q.DeleteSecret(ctx, req.RepoId, req.Key); err != nil {
		return nil, fmt.Errorf("delete secret: %w", err)
	}

	return &protos.DeleteSecretResponse{}, nil
}
