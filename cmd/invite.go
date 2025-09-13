package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/spf13/cobra"
)

var inviteCmd = &cobra.Command{
	Use:     "invite",
	Aliases: []string{"inv"},
	Short:   "Manage user invitations",
}

var inviteCreateCmd = &cobra.Command{
	Use:   "create [expires-in-hours]",
	Short: "Create a new invitation link",
	Long: `Create a new invitation link that can be used to register a new user.
The invitation will expire after the specified number of hours (default: 168 hours = 1 week).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default to 1 week expiration
		expiresInHours := int64(168)

		if len(args) > 0 {
			parsed, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid expires-in-hours value: %w", err)
			}
			if parsed <= 0 {
				return fmt.Errorf("expires-in-hours must be positive")
			}
			expiresInHours = parsed
		}

		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		c, err := client.OpenFromFile(context.Background(), wd)
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()
		configureClientOutputs(cmd, c)

		ctx := context.Background()
		response, err := c.Pogo.CreateInvite(ctx, &protos.CreateInviteRequest{
			Auth:           &protos.Auth{PersonalAccessToken: c.Token},
			ExpiresInHours: expiresInHours,
		})
		if err != nil {
			return fmt.Errorf("create invite: %w", err)
		}

		fmt.Printf("Invitation created successfully!\n\n")
		fmt.Printf("Invitation URL: %s\n", response.InviteUrl)
		fmt.Printf("Token: %s\n", response.InviteToken)

		expirationTime := time.Now().Add(time.Duration(expiresInHours) * time.Hour)
		fmt.Printf("Expires: %s\n", expirationTime.Format("2006-01-02 15:04:05 MST"))

		return nil
	},
}

var inviteListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "l"},
	Short:   "List all invitations you have created",
	RunE: func(cmd *cobra.Command, args []string) error {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		c, err := client.OpenFromFile(context.Background(), wd)
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()
		configureClientOutputs(cmd, c)

		ctx := context.Background()
		response, err := c.Pogo.GetInvites(ctx, &protos.GetInvitesRequest{
			Auth: &protos.Auth{PersonalAccessToken: c.Token},
		})
		if err != nil {
			return fmt.Errorf("get invites: %w", err)
		}

		if len(response.Invites) == 0 {
			fmt.Println("No invitations found.")
			return nil
		}

		fmt.Printf("%-20s %-20s %-20s %-15s %s\n", "Token", "Created", "Expires", "Status", "Used By")
		fmt.Println("--------------------------------------------------------------------------------")

		for _, invite := range response.Invites {
			createdAt, _ := time.Parse(time.RFC3339, invite.CreatedAt)
			expiresAt, _ := time.Parse(time.RFC3339, invite.ExpiresAt)

			status := "Active"
			usedBy := "-"

			if invite.UsedAt != nil {
				status = "Used"
				if invite.UsedByUsername != nil {
					usedBy = *invite.UsedByUsername
				}
			} else if time.Now().After(expiresAt) {
				status = "Expired"
			}

			// Truncate token for display
			displayToken := invite.Token
			if len(displayToken) > 16 {
				displayToken = displayToken[:16] + "..."
			}

			fmt.Printf("%-20s %-20s %-20s %-15s %s\n",
				displayToken,
				createdAt.Format("2006-01-02 15:04"),
				expiresAt.Format("2006-01-02 15:04"),
				status,
				usedBy,
			)
		}

		return nil
	},
}

func init() {
	inviteCmd.AddCommand(inviteCreateCmd)
	inviteCmd.AddCommand(inviteListCmd)
	RootCmd.AddCommand(inviteCmd)
}
