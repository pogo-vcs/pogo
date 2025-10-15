package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pogo-vcs/pogo/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func Md(targetDir string) error {
	fmt.Printf("Generating markdown docs into %s\n", targetDir)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	cmd.RootCmd.InitDefaultCompletionCmd()
	cmd.RootCmd.InitDefaultHelpCmd()

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

	if err := generateMarkdownDocs(cmd.RootCmd, targetDir); err != nil {
		return err
	}

	// Generate commands.md
	return generateCommandsIndex(cmd.RootCmd, targetDir)
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
			fmt.Fprintf(commandDocs[baseCommandName], "title: %s\n", baseCommandName)

			rootLevelCmd := c
			for rootLevelCmd.Parent() != nil && rootLevelCmd.Parent().Name() != "pogo" {
				rootLevelCmd = rootLevelCmd.Parent()
			}
			fmt.Fprintf(commandDocs[baseCommandName], "description: %s\n", rootLevelCmd.Short)
			commandDocs[baseCommandName].WriteString("---\n\n")
		}

		doc := commandDocs[baseCommandName]

		if parentName != "" {
			level := strings.Count(commandName, " ") + 1
			fmt.Fprintf(doc, "%s %s\n\n", strings.Repeat("#", level), commandName)
		}

		if c.Long != "" {
			doc.WriteString(c.Long + "\n\n")
		} else if c.Short != "" {
			doc.WriteString(c.Short + "\n\n")
		}

		doc.WriteString("## Usage\n\n")
		doc.WriteString("```bash\n")
		fmt.Fprintf(doc, "pogo %s", commandName)
		if c.Use != "" && !strings.HasPrefix(c.Use, commandName) {
			useParts := strings.SplitN(c.Use, " ", 2)
			if len(useParts) > 1 {
				doc.WriteString(" " + useParts[1])
			}
		}
		doc.WriteString("\n```\n\n")

		if len(c.Aliases) > 0 {
			doc.WriteString("## Aliases\n\n")
			for _, alias := range c.Aliases {
				fmt.Fprintf(doc, "- `%s`\n", alias)
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
				fmt.Fprintf(doc, "- `--%s`", flag.Name)
				if flag.Shorthand != "" {
					fmt.Fprintf(doc, ", `-%s`", flag.Shorthand)
				}
				if flag.Value.Type() != "bool" {
					fmt.Fprintf(doc, " <%s>", flag.Value.Type())
				}
				fmt.Fprintf(doc, ": %s", flag.Usage)
				if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "[]" {
					fmt.Fprintf(doc, " (default: `%s`)", flag.DefValue)
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