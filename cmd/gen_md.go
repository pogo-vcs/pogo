package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var genMdCmd = &cobra.Command{
	Use:    "gen-md [target-dir]",
	Short:  "Generate markdown documentation from Cobra commands",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := "src/content/docs/reference/"
		if len(args) > 0 {
			targetDir = args[0]
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create target directory: %w", err)
		}

		entries, err := os.ReadDir(targetDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read target directory: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				path := filepath.Join(targetDir, entry.Name())
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("failed to remove file %s: %w", path, err)
				}
			}
		}

		if err := generateMarkdownDocs(rootCmd, targetDir); err != nil {
			return err
		}

		// Generate commands.md
		return generateCommandsIndex(rootCmd, targetDir)
	},
}

func init() {
	rootCmd.AddCommand(genMdCmd)
}

func generateMarkdownDocs(cmd *cobra.Command, targetDir string) error {
	commandDocs := make(map[string]*strings.Builder)

	var processCommand func(*cobra.Command, string) error
	processCommand = func(c *cobra.Command, parentName string) error {
		if c.Hidden {
			return nil
		}

		commandName := c.Name()
		if parentName != "" {
			commandName = parentName + " " + commandName
		}

		baseCommandName := strings.Split(commandName, " ")[0]
		if baseCommandName == "pogo" {
			for _, child := range c.Commands() {
				if err := processCommand(child, ""); err != nil {
					return err
				}
			}
			return nil
		}

		if _, exists := commandDocs[baseCommandName]; !exists {
			commandDocs[baseCommandName] = &strings.Builder{}
			commandDocs[baseCommandName].WriteString("---\n")
			commandDocs[baseCommandName].WriteString(fmt.Sprintf("title: %s\n", baseCommandName))

			rootLevelCmd := c
			for rootLevelCmd.Parent() != nil && rootLevelCmd.Parent().Name() != "pogo" {
				rootLevelCmd = rootLevelCmd.Parent()
			}
			commandDocs[baseCommandName].WriteString(fmt.Sprintf("description: %s\n", rootLevelCmd.Short))
			commandDocs[baseCommandName].WriteString("---\n\n")
		}

		doc := commandDocs[baseCommandName]

		if parentName != "" {
			level := strings.Count(commandName, " ") + 1
			doc.WriteString(fmt.Sprintf("%s %s\n\n", strings.Repeat("#", level), commandName))
		}

		if c.Long != "" {
			doc.WriteString(c.Long + "\n\n")
		} else if c.Short != "" {
			doc.WriteString(c.Short + "\n\n")
		}

		doc.WriteString("## Usage\n\n")
		doc.WriteString("```bash\n")
		doc.WriteString(fmt.Sprintf("pogo %s", commandName))
		if c.Use != "" && !strings.HasPrefix(c.Use, commandName) {
			useParts := strings.SplitN(c.Use, " ", 2)
			if len(useParts) > 1 {
				doc.WriteString(" " + useParts[1])
			}
		}
		doc.WriteString("\n```\n\n")

		if c.Aliases != nil && len(c.Aliases) > 0 {
			doc.WriteString("## Aliases\n\n")
			for _, alias := range c.Aliases {
				doc.WriteString(fmt.Sprintf("- `%s`\n", alias))
			}
			doc.WriteString("\n")
		}

		flags := c.Flags()
		if flags.HasAvailableFlags() {
			doc.WriteString("## Flags\n\n")
			flags.VisitAll(func(flag *pflag.Flag) {
				if flag.Hidden {
					return
				}
				doc.WriteString(fmt.Sprintf("- `--%s`", flag.Name))
				if flag.Shorthand != "" {
					doc.WriteString(fmt.Sprintf(", `-%s`", flag.Shorthand))
				}
				if flag.Value.Type() != "bool" {
					doc.WriteString(fmt.Sprintf(" <%s>", flag.Value.Type()))
				}
				doc.WriteString(fmt.Sprintf(": %s", flag.Usage))
				if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "[]" {
					doc.WriteString(fmt.Sprintf(" (default: `%s`)", flag.DefValue))
				}
				doc.WriteString("\n")
			})
			doc.WriteString("\n")
		}

		if c.Example != "" {
			doc.WriteString("## Examples\n\n")
			doc.WriteString("```bash\n")
			doc.WriteString(c.Example)
			doc.WriteString("\n```\n\n")
		}

		for _, child := range c.Commands() {
			if err := processCommand(child, commandName); err != nil {
				return err
			}
		}

		return nil
	}

	if err := processCommand(cmd, ""); err != nil {
		return err
	}

	for cmdName, doc := range commandDocs {
		filename := filepath.Join(targetDir, cmdName+".md")
		if err := os.WriteFile(filename, []byte(doc.String()), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", filename, err)
		}
	}

	return nil
}

func generateCommandsIndex(cmd *cobra.Command, targetDir string) error {
	var doc strings.Builder

	doc.WriteString("---\n")
	doc.WriteString("title: Commands\n")
	doc.WriteString("description: Overview of all Pogo commands and global flags\n")
	doc.WriteString("---\n\n")

	doc.WriteString("Pogo provides a comprehensive set of commands for version control operations. All commands follow a consistent pattern and provide helpful feedback.\n\n")

	// Global flags section
	doc.WriteString("## Global Flags\n\n")
	doc.WriteString("These flags are available for all commands:\n\n")

	globalFlags := cmd.PersistentFlags()
	if globalFlags.HasAvailableFlags() {
		globalFlags.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			doc.WriteString(fmt.Sprintf("- `--%s`", flag.Name))
			if flag.Shorthand != "" {
				doc.WriteString(fmt.Sprintf(", `-%s`", flag.Shorthand))
			}
			if flag.Value.Type() != "bool" {
				doc.WriteString(fmt.Sprintf(" <%s>", flag.Value.Type()))
			}
			doc.WriteString(fmt.Sprintf(": %s", flag.Usage))
			if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "[]" {
				doc.WriteString(fmt.Sprintf(" (default: `%s`)", flag.DefValue))
			}
			doc.WriteString("\n")
		})
		doc.WriteString("\n")
	}

	// Top-level commands section
	doc.WriteString("## Commands\n\n")
	doc.WriteString("| Command | Description |\n")
	doc.WriteString("|---------|-------------|\n")

	// Collect and sort top-level commands
	var commands []*cobra.Command
	for _, child := range cmd.Commands() {
		if !child.Hidden {
			commands = append(commands, child)
		}
	}

	// Generate table rows for each command
	for _, c := range commands {
		commandName := c.Name()
		description := c.Short
		if description == "" {
			description = "No description available"
		}
		doc.WriteString(fmt.Sprintf("| [%s](/reference/%s) | %s |\n", commandName, commandName, description))
	}

	// Write the commands.md file
	filename := filepath.Join(targetDir, "commands.md")
	if err := os.WriteFile(filename, []byte(doc.String()), 0644); err != nil {
		return fmt.Errorf("failed to write commands.md: %w", err)
	}

	return nil
}
