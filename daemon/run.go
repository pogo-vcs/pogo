package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/config"
	"github.com/pogo-vcs/pogo/gui"
	"github.com/rjeczalik/notify"
)

var (
	repoTimers = make(map[string]*time.Timer)
	repoMutex  = sync.RWMutex{}
)

// getWatchDirectories returns array of directories to watch
// Currently returns only user's home directory, but can be extended
func getWatchDirectories() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		gui.Errorf("Failed to get user home directory: %v", err)
		return []string{}
	}

	return []string{homeDir}
}

// findPogoRepository walks up the directory tree looking for .pogo.yaml
func findPogoRepository(filePath string) (string, bool) {
	dir := filepath.Dir(filePath)
	if dir == filePath {
		// Reached root directory
		return "", false
	}

	pogoFile := filepath.Join(dir, ".pogo.yaml")
	if _, err := os.Stat(pogoFile); err == nil {
		return dir, true
	}

	return findPogoRepository(dir)
}

// handleEvent processes a single file system event
func handleEvent(ei notify.EventInfo, pushFn func(string) error, pushDelay time.Duration) {
	filePath := ei.Path()

	// Find the repository root
	repoRoot, found := findPogoRepository(filePath)
	if !found {
		// File is not in a Pogo repository
		return
	}

	// log.Printf("File change detected: %s (repo: %s, event: %s)",
	// 	filePath, repoRoot, ei.Event())

	scheduleRepositoryPush(repoRoot, pushFn, pushDelay)
}

// scheduleRepositoryPush schedules or reschedules a push for the given repository
func scheduleRepositoryPush(repoRoot string, pushFn func(string) error, pushDelay time.Duration) {
	repoMutex.Lock()
	defer repoMutex.Unlock()

	// Cancel existing timer if it exists
	if timer, exists := repoTimers[repoRoot]; exists {
		timer.Stop()
		// 	log.Printf("Reset timer for repository: %s", repoRoot)
		// } else {
		// 	log.Printf("Scheduled push for repository: %s (delay: %v)", repoRoot, pushDelay)
	}

	// Create new timer
	repoTimers[repoRoot] = time.AfterFunc(pushDelay, func() {
		triggerPush(repoRoot, pushFn)
	})
}

// triggerPush executes the push operation and cleans up the timer
func triggerPush(repoRoot string, pushFn func(string) error) {
	// log.Printf("Triggering push for repository: %s", repoRoot)

	if err := pushFn(repoRoot); err != nil {
		gui.Errorf("Failed to push repository %s: %v", repoRoot, err)
	}

	// Clean up timer
	repoMutex.Lock()
	delete(repoTimers, repoRoot)
	repoMutex.Unlock()
}

// processEvents handles incoming file system events
func processEvents(ctx context.Context, eventChan chan notify.EventInfo, pushFn func(string) error, pushDelay time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return

		case ei := <-eventChan:
			handleEvent(ei, pushFn, pushDelay)
		}
	}
}

// startWatching begins watching for file changes
func startWatching(ctx context.Context, pushFn func(string) error, pushDelay time.Duration) error {
	eventChan := make(chan notify.EventInfo, 100)
	watchDirs := getWatchDirectories()

	// Set up watchers for all directories
	for _, dir := range watchDirs {
		// Watch recursively using "..." suffix
		watchPath := filepath.Join(dir, "...")

		if err := notify.Watch(watchPath, eventChan, notify.All); err != nil {
			return fmt.Errorf("failed to watch directory %s: %v", dir, err)
		}

		log.Printf("Watching directory: %s (recursive)", dir)
	}

	// Start event processing goroutine
	go processEvents(ctx, eventChan, pushFn, pushDelay)

	// Handle cleanup when context is cancelled
	go func() {
		<-ctx.Done()
		notify.Stop(eventChan)

		// Cancel all pending timers
		repoMutex.Lock()
		for _, timer := range repoTimers {
			timer.Stop()
		}
		repoMutex.Unlock()

		log.Println("Pogo watcher stopped")
	}()

	return nil
}

func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pushRepo := func(repoRoot string) error {
		c, err := client.OpenFromFile(ctx, repoRoot)
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()

		if err := c.PushFull(false); err != nil {
			return fmt.Errorf("push full: %w", err)
		}
		return nil
	}

	if err := startWatching(ctx, pushRepo, cfg.Daemon.PushDelay); err != nil {
		return fmt.Errorf("start watching: %w", err)
	}
	return nil
}