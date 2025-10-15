package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Dependency struct {
	Name        string
	Version     string
	Description string
	License     string
	LicenseURL  string
	Homepage    string
}

type GoModInfo struct {
	Version string `json:"Version"`
	Time    string `json:"Time"`
}

type GitHubRepoInfo struct {
	Description string `json:"description"`
	License     struct {
		Name string `json:"name"`
		SPDX string `json:"spdx_id"`
		Key  string `json:"key"`
	} `json:"license"`
	Homepage string `json:"homepage"`
	HTMLURL  string `json:"html_url"`
}

func Deps(targetFile string) error {
	fmt.Printf("Analyzing dependencies and generating report to %s\n", targetFile)

	deps, err := parseGoMod()
	if err != nil {
		return fmt.Errorf("failed to parse go.mod: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := enrichDependencyInfo(ctx, deps); err != nil {
		return fmt.Errorf("failed to enrich dependency info: %w", err)
	}

	if err := generateDependencyReport(deps, targetFile); err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	return nil
}

func parseGoMod() ([]Dependency, error) {
	file, err := os.Open("go.mod")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var deps []Dependency
	scanner := bufio.NewScanner(file)
	inRequireBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "require (") {
			inRequireBlock = true
			continue
		}

		if inRequireBlock && line == ")" {
			inRequireBlock = false
			continue
		}

		if inRequireBlock || strings.HasPrefix(line, "require ") {
			if strings.Contains(line, "// indirect") {
				continue
			}

			// Clean up the line
			line = strings.TrimPrefix(line, "require ")
			line = strings.TrimSpace(line)

			// Skip comments and empty lines
			if strings.HasPrefix(line, "//") || line == "" {
				continue
			}

			// Parse module and version
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[0]
				version := parts[1]

				// Skip standard library and our own module
				if !strings.Contains(name, ".") || strings.HasPrefix(name, "github.com/pogo-vcs/pogo") {
					continue
				}

				deps = append(deps, Dependency{
					Name:    name,
					Version: version,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return deps, nil
}

func enrichDependencyInfo(ctx context.Context, deps []Dependency) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for i := range deps {
		fmt.Printf("Fetching info for %s...\n", deps[i].Name)

		if err := fetchGoModuleInfo(ctx, client, &deps[i]); err != nil {
			fmt.Printf("  Warning: failed to fetch Go module info for %s: %v\n", deps[i].Name, err)
		}

		if err := fetchGitHubInfo(ctx, client, &deps[i]); err != nil {
			fmt.Printf("  Warning: failed to fetch GitHub info for %s: %v\n", deps[i].Name, err)
			// Try pkg.go.dev as fallback for non-GitHub packages
			if err := fetchPkgGoDevInfo(ctx, client, &deps[i]); err != nil {
				fmt.Printf("  Warning: failed to fetch pkg.go.dev info for %s: %v\n", deps[i].Name, err)
			}
		}

		// Small delay to be respectful to APIs
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

func fetchGoModuleInfo(ctx context.Context, client *http.Client, dep *Dependency) error {
	url := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.info", dep.Name, dep.Version)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var info GoModInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return err
	}

	// The Go proxy doesn't provide description/license, so this is mainly for validation
	return nil
}

func fetchGitHubInfo(ctx context.Context, client *http.Client, dep *Dependency) error {
	// Extract GitHub repo from module path
	if !strings.HasPrefix(dep.Name, "github.com/") {
		return fmt.Errorf("not a GitHub module")
	}

	// Convert module path to GitHub API URL
	parts := strings.Split(dep.Name, "/")
	if len(parts) < 3 {
		return fmt.Errorf("invalid GitHub module path")
	}

	owner := parts[1]
	repo := parts[2]

	// Remove version suffixes like /v2, /v3 etc.
	if strings.HasPrefix(repo, "v") && len(repo) > 1 {
		// Check if this is a version suffix
		for _, char := range repo[1:] {
			if char < '0' || char > '9' {
				goto notversion
			}
		}
		// This is a version suffix, use the parent path
		if len(parts) > 3 {
			repo = parts[3]
		}
	}
notversion:

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	// Add GitHub PAT if available
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var repoInfo GitHubRepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&repoInfo); err != nil {
		return err
	}

	dep.Description = repoInfo.Description
	if repoInfo.License.Name != "" {
		dep.License = repoInfo.License.Name
	} else if repoInfo.License.SPDX != "" {
		dep.License = repoInfo.License.SPDX
	}

	// Construct license URL from repository URL
	if repoInfo.License.Key != "" {
		dep.LicenseURL = repoInfo.HTMLURL + "/blob/main/LICENSE"
	}
	dep.Homepage = repoInfo.HTMLURL

	return nil
}

func fetchPkgGoDevInfo(ctx context.Context, client *http.Client, dep *Dependency) error {
	url := fmt.Sprintf("https://pkg.go.dev/%s", dep.Name)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	content := string(body)

	// Extract license information
	licenseRegex := regexp.MustCompile(`License:\s*<a[^>]*>([^<]+)</a>`)
	if matches := licenseRegex.FindStringSubmatch(content); len(matches) > 1 {
		dep.License = strings.TrimSpace(matches[1])
		// Construct license URL
		dep.LicenseURL = fmt.Sprintf("https://pkg.go.dev/%s?tab=licenses", dep.Name)
	}

	// Extract description from the overview section or readme
	overviewRegex := regexp.MustCompile(`<meta name="description" content="([^"]+)"`)
	if matches := overviewRegex.FindStringSubmatch(content); len(matches) > 1 {
		description := strings.TrimSpace(matches[1])
		// Clean up the description
		if strings.HasPrefix(description, "Package "+dep.Name) {
			description = strings.TrimPrefix(description, "Package "+dep.Name)
			description = strings.TrimSpace(description)
		}
		if description != "" && dep.Description == "" {
			dep.Description = description
		}
	}

	// Set homepage to pkg.go.dev if not already set
	if dep.Homepage == "" {
		dep.Homepage = url
	}

	return nil
}

func generateDependencyReport(deps []Dependency, targetFile string) error {
	// Sort dependencies by name
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
	})

	// Ensure target directory exists
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		return err
	}

	file, err := os.Create(targetFile)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	defer w.Flush()

	// Write header
	_, _ = fmt.Fprintf(w, "---\ntitle: Dependencies\ndescription: Pogo's first party Dependencies\n---\n\n")
	_, _ = fmt.Fprintf(w, "This document lists all first-party dependencies used by Pogo.\n\n")
	_, _ = fmt.Fprintf(w, "Generated on: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// Write summary
	_, _ = fmt.Fprintf(w, "## Summary\n\n")
	_, _ = fmt.Fprintf(w, "Total dependencies: %d\n\n", len(deps))

	// Count licenses
	licenses := make(map[string]int)
	for _, dep := range deps {
		if dep.License != "" {
			licenses[dep.License]++
		} else {
			licenses["Unknown"]++
		}
	}

	_, _ = fmt.Fprintf(w, "### License Distribution\n\n")
	for license, count := range licenses {
		_, _ = fmt.Fprintf(w, "- %s: %d\n", license, count)
	}
	_, _ = fmt.Fprintf(w, "\n")

	// Write detailed dependency list
	_, _ = fmt.Fprintf(w, "## Dependencies\n\n")

	for _, dep := range deps {
		_, _ = fmt.Fprintf(w, "### %s\n\n", dep.Name)
		_, _ = fmt.Fprintf(w, "**Version:** %s\n\n", dep.Version)

		if dep.Description != "" {
			_, _ = fmt.Fprintf(w, "**Description:** %s\n\n", dep.Description)
		}

		if dep.License != "" {
			if dep.LicenseURL != "" {
				_, _ = fmt.Fprintf(w, "**License:** [%s](%s)\n\n", dep.License, dep.LicenseURL)
			} else {
				_, _ = fmt.Fprintf(w, "**License:** %s\n\n", dep.License)
			}
		} else {
			_, _ = fmt.Fprintf(w, "**License:** Unknown\n\n")
		}

		if dep.Homepage != "" {
			_, _ = fmt.Fprintf(w, "**Homepage:** %s\n\n", dep.Homepage)
		}

		_, _ = fmt.Fprintf(w, "---\n\n")
	}

	return nil
}