package filecontents_test

import (
	"io"
	"os"
	"testing"

	"github.com/pogo-vcs/pogo/filecontents"
)

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		fileName string
		want     filecontents.FileType
		wantErr  bool
	}{
		{
			"utf-8 unix",
			"test_file_utf8_lf.bin",
			filecontents.FileType{false, filecontents.UTF8, false, filecontents.LF},
			false,
		},
		{
			"utf-16le unix",
			"test_file_utf16le_lf.bin",
			filecontents.FileType{false, filecontents.UTF16LE, false, filecontents.LF},
			false,
		},
		{
			"utf-16le dos",
			"test_file_utf16le_crlf.bin",
			filecontents.FileType{false, filecontents.UTF16LE, false, filecontents.CRLF},
			false,
		},
		{
			"utf-32le unix",
			"test_file_utf32le_lf.bin",
			filecontents.FileType{false, filecontents.UTF32LE, false, filecontents.LF},
			false,
		},
		{
			"avif",
			"test_file_avif.bin",
			filecontents.FileType{true, filecontents.UnknownEncoding, false, filecontents.UnknownLineEnding},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := filecontents.DetectFileType(tt.fileName)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("DetectFileType() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("DetectFileType() succeeded unexpectedly")
			}
			if got.String() != tt.want.String() {
				t.Errorf("DetectFileType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileType_CanonicalizeReader(t *testing.T) {
	tests := []struct {
		name     string // description of this test case
		fileName string
		t        filecontents.FileType
	}{
		{
			"utf-16le unix",
			"test_file_utf16le_lf.bin",
			filecontents.FileType{false, filecontents.UTF16LE, false, filecontents.LF},
		},
		{
			"utf-16le dos",
			"test_file_utf16le_crlf.bin",
			filecontents.FileType{false, filecontents.UTF16LE, false, filecontents.CRLF},
		},
		{
			"utf-32le unix",
			"test_file_utf32le_lf.bin",
			filecontents.FileType{false, filecontents.UTF32LE, false, filecontents.LF},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceFile, err := os.Open(tt.fileName)
			if err != nil {
				t.Fatalf("could not open source file: %v", err)
				return
			}
			defer sourceFile.Close()

			compareFile, err := os.Open("test_file_utf8_lf.bin")
			if err != nil {
				t.Fatalf("could not open compare file: %v", err)
				return
			}
			defer compareFile.Close()

			canonicalizedSourceFile := tt.t.CanonicalizeReader(sourceFile)

			canonicalizedSourceBytes, err := io.ReadAll(canonicalizedSourceFile)
			if err != nil {
				t.Fatalf("could not read canonicalized source file: %v", err)
				return
			}

			compareBytes, err := io.ReadAll(compareFile)
			if err != nil {
				t.Fatalf("could not read compare file: %v", err)
				return
			}

			if len(canonicalizedSourceBytes) != len(compareBytes) {
				t.Errorf("CanonicalizeReader() = %v, want %v", len(canonicalizedSourceBytes), len(compareBytes))
			}

			for i := range max(len(canonicalizedSourceBytes), len(compareBytes)) {
				var (
					gotByte  byte
					wantByte byte
				)
				if i < len(canonicalizedSourceBytes) {
					gotByte = canonicalizedSourceBytes[i]
				}
				if i < len(compareBytes) {
					wantByte = compareBytes[i]
				}
				if gotByte != wantByte {
					t.Fatalf("CanonicalizeReader() = %v, want %v at index %d", gotByte, wantByte, i)
				}
			}
		})
	}
}

func TestFileType_TypeReader(t *testing.T) {
	tests := []struct {
		name     string // description of this test case
		fileName string
		t        filecontents.FileType
	}{
		{
			"utf-16le unix",
			"test_file_utf16le_lf.bin",
			filecontents.FileType{false, filecontents.UTF16LE, false, filecontents.LF},
		},
		{
			"utf-16le dos",
			"test_file_utf16le_crlf.bin",
			filecontents.FileType{false, filecontents.UTF16LE, false, filecontents.CRLF},
		},
		{
			"utf-32le unix",
			"test_file_utf32le_lf.bin",
			filecontents.FileType{false, filecontents.UTF32LE, false, filecontents.LF},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceFile, err := os.Open("test_file_utf8_lf.bin")
			if err != nil {
				t.Fatalf("could not open source file: %v", err)
				return
			}
			defer sourceFile.Close()

			compareFile, err := os.Open(tt.fileName)
			if err != nil {
				t.Fatalf("could not open compare file: %v", err)
				return
			}
			defer compareFile.Close()

			typedSourceFile := tt.t.TypeReader(sourceFile)

			typedSourceBytes, err := io.ReadAll(typedSourceFile)
			if err != nil {
				t.Fatalf("could not read typed source file: %v", err)
				return
			}

			compareBytes, err := io.ReadAll(compareFile)
			if err != nil {
				t.Fatalf("could not read compare file: %v", err)
				return
			}

			if len(typedSourceBytes) != len(compareBytes) {
				t.Errorf("TypedReader() = %v, want %v", len(typedSourceBytes), len(compareBytes))
			}

			for i := range max(len(typedSourceBytes), len(compareBytes)) {
				var (
					gotByte  byte
					wantByte byte
				)
				if i < len(typedSourceBytes) {
					gotByte = typedSourceBytes[i]
				}
				if i < len(compareBytes) {
					wantByte = compareBytes[i]
				}
				if gotByte != wantByte {
					t.Fatalf("TypedReader() = %v, want %v at index %d", gotByte, wantByte, i)
				}
			}
		})
	}
}

func TestHasConflictMarkers(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "no conflict markers",
			content:  "This is a normal file\nwith no conflicts\n",
			expected: false,
		},
		{
			name:     "has all conflict markers",
			content:  "line 1\n<<<<<<< HEAD\nchanged in HEAD\n=======\nchanged in branch\n>>>>>>> branch\nline 2\n",
			expected: true,
		},
		{
			name:     "missing start marker",
			content:  "line 1\nchanged in HEAD\n=======\nchanged in branch\n>>>>>>> branch\nline 2\n",
			expected: false,
		},
		{
			name:     "missing separator",
			content:  "line 1\n<<<<<<< HEAD\nchanged in HEAD\nchanged in branch\n>>>>>>> branch\nline 2\n",
			expected: false,
		},
		{
			name:     "missing end marker",
			content:  "line 1\n<<<<<<< HEAD\nchanged in HEAD\n=======\nchanged in branch\nline 2\n",
			expected: false,
		},
		{
			name:     "binary file should return false",
			content:  "\x00\x01\x02\x03<<<<<<< HEAD\n=======\n>>>>>>> branch",
			expected: false, // Binary files are skipped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file
			tmpFile, err := os.CreateTemp("", "conflict_test_*.txt")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			// Write test content
			if _, err := tmpFile.WriteString(tt.content); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			// Test the function
			result, err := filecontents.HasConflictMarkers(tmpFile.Name())
			if err != nil {
				t.Fatalf("HasConflictMarkers() failed: %v", err)
			}

			if result != tt.expected {
				t.Errorf("HasConflictMarkers() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
