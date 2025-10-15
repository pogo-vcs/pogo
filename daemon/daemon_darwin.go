package daemon

import (
	_ "embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"text/template"
)

//go:embed launchd.txt
var launchdTemplateStr string

const serviceIdentifier = "com.pogo-vcs.pogod"

type LaunchdConfig struct {
	Arguments            []string // Run with arguments.
	Path                 string
	StandardOutPath      string
	StandardErrorPath    string
	Name                 string // Required name of the service. No spaces suggested.
	Description          string // Long description of service.
	WorkingDirectory     string // Initial working directory.
	EnvVars              map[string]string
	KeepAlive, RunAtLoad bool
	SessionCreate        bool
}

func getServiceFilePath() (string, error) {
	homeDir, err := getHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents", "com.pogo-vcs.pogod.plist"), nil
}

func Install() error {
	serviceFile, err := getServiceFilePath()
	if err != nil {
		return err
	}

	homeDir, err := getHomeDir()
	if err != nil {
		return err
	}

	selfPath, err := getSelfPath()
	if err != nil {
		return err
	}

	t, err := template.New("launchd").
		Funcs(template.FuncMap{
			"bool": func(v bool) string {
				if v {
					return "true"
				}
				return "false"
			},
		}).
		Parse(launchdTemplateStr)

	if err != nil {
		return fmt.Errorf("failed to parse launchd template: %w", err)
	}

	uid, err := getUID()
	if err != nil {
		return fmt.Errorf("failed to get user id: %w", err)
	}

	// Check if service file already exists
	if _, err := os.Stat(serviceFile); err == nil {
		if err := sysRun("launchctl", "bootstrap", path.Join("gui", uid), serviceFile); err != nil {
			_ = fmt.Errorf("failed to bootstrap service: %w", err)
		}
		if err := sysRun("launchctl", "enable", path.Join("gui", uid, serviceIdentifier)); err != nil {
			_ = fmt.Errorf("failed to enable service: %w", err)
		}
		if err := sysRun("launchctl", "kickstart", "-k", path.Join("gui", uid, serviceIdentifier)); err != nil {
			_ = fmt.Errorf("failed to kickstart service: %w", err)
		}
		return nil
	}

	// Make sure the directory exists
	if err := os.MkdirAll(filepath.Dir(serviceFile), 0755); err != nil {
		return err
	}

	// Create the service file
	f, err := os.OpenFile(serviceFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	err = t.Execute(f, LaunchdConfig{
		Name:              serviceIdentifier,
		Description:       "Pogo daemon",
		Path:              selfPath,
		KeepAlive:         true,
		RunAtLoad:         true,
		StandardOutPath:   filepath.Join(homeDir, "Library", "Logs", "pogod.log"),
		StandardErrorPath: filepath.Join(homeDir, "Library", "Logs", "pogod.log"),
		EnvVars: map[string]string{
			"XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME"),
		},
		Arguments:        []string{"daemon", "run"},
		WorkingDirectory: homeDir,
	})
	if err != nil {
		return fmt.Errorf("failed to execute launchd template: %w", err)
	}

	// Install the service
	if err := sysRun("launchctl", "bootstrap", path.Join("gui", uid), serviceFile); err != nil {
		return fmt.Errorf("failed to bootstrap service: %w", err)
	}
	if err := sysRun("launchctl", "enable", path.Join("gui", uid, serviceIdentifier)); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	if err := sysRun("launchctl", "kickstart", "-k", path.Join("gui", uid, serviceIdentifier)); err != nil {
		return fmt.Errorf("failed to kickstart service: %w", err)
	}

	return nil
}

func Uninstall() error {
	serviceFile, err := getServiceFilePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(serviceFile); err != nil {
		return fmt.Errorf("service file not found: %w", err)
	}
	uid, err := getUID()
	if err != nil {
		return fmt.Errorf("failed to get user id: %w", err)
	}

	// might not be running
	_ = sysRun("launchctl", "bootout", path.Join("gui", uid), serviceFile)

	if err := os.Remove(serviceFile); err != nil {
		return fmt.Errorf("failed to remove service file: %w", err)
	}

	return nil
}

func Stop() error {
	uid, err := getUID()
	if err != nil {
		return fmt.Errorf("failed to get user id: %w", err)
	}
	serviceFile, err := getServiceFilePath()
	if err != nil {
		return err
	}

	if err := sysRun("launchctl", "bootout", path.Join("gui", uid), serviceFile); err != nil {
		return fmt.Errorf("failed to bootout service: %w", err)
	}
	return nil
}

func Start() error {
	serviceFile, err := getServiceFilePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(serviceFile); err != nil {
		return fmt.Errorf("service file not found: %w", err)
	}

	uid, err := getUID()
	if err != nil {
		return fmt.Errorf("failed to get user id: %w", err)
	}

	if err := sysRun("launchctl", "bootstrap", path.Join("gui", uid), serviceFile); err != nil {
		return fmt.Errorf("failed to bootstrap service: %w", err)
	}

	if err := sysRun("launchctl", "kickstart", "-k", path.Join("gui", uid, serviceIdentifier)); err != nil {
		return fmt.Errorf("failed to kickstart service: %w", err)
	}

	return nil
}