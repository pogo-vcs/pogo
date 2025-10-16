package env

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

var (
	DatabaseUrl       string
	PublicAddress     string
	Hostname          string
	RootToken         string
	ListenAddress     string
	GcMemoryThreshold int64
	CiRunRetention    time.Duration
)

type Config struct {
	DatabaseUrl       string
	PublicAddress     string
	Hostname          string
	RootToken         string
	ListenAddress     string
	GcMemoryThreshold int64
	CiRunRetention    time.Duration
}

func InitFromEnvironment() error {
	var ok bool
	if DatabaseUrl, ok = os.LookupEnv("DATABASE_URL"); !ok {
		return fmt.Errorf("missing DATABASE_URL")
	}
	if PublicAddress, ok = os.LookupEnv("PUBLIC_ADDRESS"); !ok {
		return fmt.Errorf("missing PUBLIC_ADDRESS")
	}
	if url, err := url.Parse(PublicAddress); err == nil {
		Hostname = url.Host
	} else {
		return fmt.Errorf("invalid PUBLIC_ADDRESS %s: %w", PublicAddress, err)
	}
	if RootToken, ok = os.LookupEnv("ROOT_TOKEN"); !ok {
		RootToken = ""
	}
	if host, ok := os.LookupEnv("HOST"); ok {
		ListenAddress = host
	} else if portStr, ok := os.LookupEnv("PORT"); ok {
		if port, err := strconv.Atoi(portStr); err == nil {
			ListenAddress = fmt.Sprintf(":%d", port)
		} else {
			return fmt.Errorf("invalid PORT %s: %w", portStr, err)
		}
	} else {
		ListenAddress = ":8080"
	}
	if GcMemoryThresholdStr, ok := os.LookupEnv("GC_MEMORY_THRESHOLD"); ok {
		var err error
		if GcMemoryThreshold, err = strconv.ParseInt(GcMemoryThresholdStr, 10, 64); err != nil {
			return fmt.Errorf("invalid GC_MEMORY_THRESHOLD: %w", err)
		}
	}
	CiRunRetention = 30 * 24 * time.Hour
	if retentionStr, ok := os.LookupEnv("CI_RUN_RETENTION"); ok {
		duration, err := time.ParseDuration(retentionStr)
		if err != nil {
			return fmt.Errorf("invalid CI_RUN_RETENTION: %w", err)
		}
		if duration <= 0 {
			return fmt.Errorf("CI_RUN_RETENTION must be positive duration, got %s", retentionStr)
		}
		CiRunRetention = duration
	}

	return nil
}

func InitFromConfig(config Config) error {
	DatabaseUrl = config.DatabaseUrl
	PublicAddress = config.PublicAddress
	RootToken = config.RootToken
	ListenAddress = config.ListenAddress
	GcMemoryThreshold = config.GcMemoryThreshold
	if config.CiRunRetention > 0 {
		CiRunRetention = config.CiRunRetention
	} else {
		CiRunRetention = 30 * 24 * time.Hour
	}

	if config.Hostname != "" {
		Hostname = config.Hostname
	} else if config.PublicAddress != "" {
		if url, err := url.Parse(config.PublicAddress); err == nil {
			Hostname = url.Host
		} else {
			return fmt.Errorf("invalid PUBLIC_ADDRESS %s: %w", config.PublicAddress, err)
		}
	}

	if config.ListenAddress == "" {
		ListenAddress = ":8080"
	}

	return nil
}
