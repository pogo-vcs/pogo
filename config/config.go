package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type (
	Config struct {
		Daemon DaemonConfig `yaml:"daemon"`
	}
	DaemonConfig struct {
		ObserveDirs []string `yaml:"observe_dirs"`
		PushDelay   string   `yaml:"push_delay"`
	}
)

type (
	ConfigParsed struct {
		Daemon DaemonConfigParsed
	}
	DaemonConfigParsed struct {
		ObserveDirs []string
		PushDelay   time.Duration
	}
)

func Load() (ConfigParsed, error) {
	c, err := load()
	if err != nil {
		return ConfigParsed{}, err
	}

	daemonPushDelay, err := time.ParseDuration(c.Daemon.PushDelay)
	if err != nil {
		return ConfigParsed{}, fmt.Errorf("failed to parse daemon push delay: %w", err)
	}

	return ConfigParsed{
		Daemon: DaemonConfigParsed{
			ObserveDirs: c.Daemon.ObserveDirs,
			PushDelay:   daemonPushDelay,
		},
	}, nil
}

func load() (Config, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return defaultConfig(), fmt.Errorf("failed to get config dir: %w", err)
	}
	configFile := filepath.Join(configDir, "config.yaml")

	// Create config directory if it doesn't exist
	_ = os.MkdirAll(configDir, 0755)

	// check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		c := defaultConfig()
		f, err := os.Create(configFile)
		if err != nil {
			return c, fmt.Errorf("failed to create config file: %w", err)
		}
		defer f.Close()
		ye := yaml.NewEncoder(f)
		ye.SetIndent(2)
		if err := ye.Encode(c); err != nil {
			return c, fmt.Errorf("failed to encode config: %w", err)
		}
		return c, nil
	}
	f, err := os.Open(configFile)
	if err != nil {
		return defaultConfig(), fmt.Errorf("failed to open config file: %w", err)
	}
	defer f.Close()
	yd := yaml.NewDecoder(f)
	c := Config{}
	if err := yd.Decode(&c); err != nil {
		return defaultConfig(), fmt.Errorf("failed to decode config: %w", err)
	}

	return c, nil
}

func getConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "pogo"), nil
}

func defaultConfig() Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return Config{
		Daemon: DaemonConfig{
			ObserveDirs: []string{homeDir},
			PushDelay:   "10s",
		},
	}
}
