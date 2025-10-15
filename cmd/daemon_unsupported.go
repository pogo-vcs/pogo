//go:build !darwin

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:     "daemon",
	Aliases: []string{"service"},
	Short:   "Manage Pogo daemon service",
	Long: `Manage the Pogo daemon service for automatic operations.

This is currently only implemented for macOS. Windows and Linux will follow soon.

The daemon service can be installed to run automatically and provides
background functionality for Pogo operations.

This daemon is not required but it allows for automatic pushing of any changes.
You can tweak its behaviour by editing the global configuration file which is located at your system's default config directory.`,
}

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Pogo daemon service",
	Long: `Install the Pogo daemon service to run automatically.

This will create the necessary service configuration files and register
the daemon with the system service manager.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented on this platform")
	},
}

var daemonRunCmd = &cobra.Command{
	Hidden: true,
	Use:    "run",
	Short:  "Run the Pogo daemon service",
	Long: `Run the Pogo daemon service process.

This command starts the daemon and waits for SIGTERM to gracefully shutdown.
It is typically called by the system service manager, not directly by users.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented on this platform")
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Pogo daemon service",
	Long: `Stop the Pogo daemon service process.

This command stops the daemon and waits for it to gracefully shutdown.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented on this platform")
	},
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Pogo daemon service",
	Long: `Start the Pogo daemon service process.

This command starts the daemon and waits for it to gracefully shutdown.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented on this platform")
	},
}

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Pogo daemon service",
	Long: `Uninstall the Pogo daemon service from the system.

This will remove the service configuration files and unregister the daemon
with the system service manager.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented on this platform")
	},
}

func init() {
	daemonCmd.AddCommand(daemonInstallCmd)
	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonUninstallCmd)
	RootCmd.AddCommand(daemonCmd)
}