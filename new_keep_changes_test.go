//go:build fakekeyring

package main_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pogo-vcs/pogo/client"
)

func TestNewWithKeepChanges(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("KeepChangesFlag", func(t *testing.T) {
		testNewKeepChanges(t, ctx, env.serverAddr)
	})
}

func testNewKeepChanges(t *testing.T, ctx context.Context, serverAddr string) {
	tmpDir, err := os.MkdirTemp("", "pogo-test-keep-changes-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := setupToken(serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	repoName := "test-keep-changes-" + time.Now().Format("20060102150405")
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}
	t.Logf("Created repository %s (ID: %d, Initial change: %d)", repoName, repoId, changeId)

	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	file1Path := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(file1Path, []byte("Initial content in first change"), 0644); err != nil {
		t.Fatalf("Failed to create file1.txt: %v", err)
	}

	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c2.Close()

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push initial content: %v", err)
	}

	info1, err := c2.Info()
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	firstChangeName := info1.ChangeName
	t.Logf("First change: %s", firstChangeName)

	file2Path := filepath.Join(tmpDir, "file2.txt")
	if err := os.WriteFile(file2Path, []byte("Content for second change"), 0644); err != nil {
		t.Fatalf("Failed to create file2.txt: %v", err)
	}

	desc := "Second change created with keep-changes"
	secondChangeId, secondChangeName, err := c2.NewChange(&desc, []string{firstChangeName})
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}
	t.Logf("Created second change: %s (ID: %d)", secondChangeName, secondChangeId)

	c2.ConfigSetChangeId(secondChangeId)

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push to second change: %v", err)
	}

	if err := c2.Edit(firstChangeName); err != nil {
		t.Fatalf("Failed to switch back to first change: %v", err)
	}

	if _, err := os.Stat(file2Path); !os.IsNotExist(err) {
		t.Error("file2.txt should not exist in first change after switching back")
	}

	content1, err := os.ReadFile(file1Path)
	if err != nil {
		t.Fatalf("Failed to read file1.txt in first change: %v", err)
	}
	if string(content1) != "Initial content in first change" {
		t.Errorf("Unexpected content in first change: got %q, want %q", string(content1), "Initial content in first change")
	}

	if err := c2.Edit(secondChangeName); err != nil {
		t.Fatalf("Failed to switch to second change: %v", err)
	}

	if _, err := os.Stat(file2Path); err != nil {
		t.Fatalf("file2.txt should exist in second change: %v", err)
	}

	content2, err := os.ReadFile(file2Path)
	if err != nil {
		t.Fatalf("Failed to read file2.txt in second change: %v", err)
	}
	if string(content2) != "Content for second change" {
		t.Errorf("Unexpected content in file2.txt: got %q, want %q", string(content2), "Content for second change")
	}

	content1InSecond, err := os.ReadFile(file1Path)
	if err != nil {
		t.Fatalf("Failed to read file1.txt in second change: %v", err)
	}
	if string(content1InSecond) != "Initial content in first change" {
		t.Errorf("file1.txt should still have original content in second change: got %q", string(content1InSecond))
	}

	t.Log("Keep changes test completed successfully")
}