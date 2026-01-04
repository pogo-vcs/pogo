package main

import (
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

type Preferences struct {
	RecentRepositories []string `yaml:"recent_repositories"`
}

const maxRecentRepos = 5

var (
	preferences Preferences
	homeDir     string
)

func init() {
	if dir, err := os.UserHomeDir(); err == nil {
		homeDir = dir
	} else {
		panic(err)
	}
	preferencesFilePath := getPreferencesFilePath()
	_ = os.MkdirAll(filepath.Dir(preferencesFilePath), 0755)
	if _, err := os.Stat(preferencesFilePath); err == nil {
		f, err := os.Open(preferencesFilePath)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := yaml.NewDecoder(f).Decode(&preferences); err != nil {
			panic(err)
		}
	}
}

func notifyOpenRepository(repo string) {
	if slices.Contains(preferences.RecentRepositories, repo) {
		return
	}
	preferences.RecentRepositories = append(preferences.RecentRepositories, repo)
	if len(preferences.RecentRepositories) > maxRecentRepos {
		preferences.RecentRepositories = preferences.RecentRepositories[1:]
	}
	savePreferences()
}

func savePreferences() {
	f, err := os.Create(getPreferencesFilePath())
	if err != nil {
		panic(err)
	}
	defer f.Close()
	ye := yaml.NewEncoder(f)
	ye.SetIndent(4)
	if err := ye.Encode(preferences); err != nil {
		panic(err)
	}
}

func getPreferencesFilePath() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "pogo", "gui.yaml")
	}
	if dir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dir, ".config", "pogo", "gui.yaml")
	}
	panic("Could not determine user preferences directory")
}
