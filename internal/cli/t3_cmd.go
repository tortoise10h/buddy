package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend/t3"
)

func newT3Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "t3",
		Short: "T3 Code integration commands",
	}

	cmd.AddCommand(newT3AuthCmd())
	cmd.AddCommand(newT3SnapshotCmd())
	return cmd
}

func newT3AuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage T3 Code authentication and pairing tokens",
	}

	cmd.AddCommand(newT3AuthLoginCmd())
	cmd.AddCommand(newT3AuthStatusCmd())
	return cmd
}

func newT3AuthLoginCmd() *cobra.Command {
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Pair Buddy with T3 Code via 30-day loopback OAuth PKCE",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			out := cmd.OutOrStdout()
			opts := t3.LoopbackOAuthOptions{
				NoBrowser: noBrowser,
				StatusCallback: func(step, detail string) {
					switch step {
					case "listening":
						fmt.Fprintf(out, "✓ %s\n", detail)
					case "registering":
						fmt.Fprintf(out, "✓ %s\n", detail)
					case "authorizing":
						if noBrowser {
							fmt.Fprintln(out, "\nOpen the following authorization URL in your browser:")
							fmt.Fprintln(out, detail)
							fmt.Fprintln(out, "\nWaiting for approval...")
						} else {
							fmt.Fprintln(out, "\nOpening browser for authorization...")
							fmt.Fprintln(out, "If the browser did not open automatically, visit:")
							fmt.Fprintln(out, detail)
							fmt.Fprintln(out, "\nWaiting for approval...")
						}
					case "exchanging":
						fmt.Fprintf(out, "✓ %s\n", detail)
					}
				},
			}

			tok, err := t3.PerformLoopbackOAuth(ctx, opts)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}

			fmt.Fprintln(out, "\n🎉 Successfully authenticated with T3 Code!")
			fmt.Fprintf(out, "Scope:      %s\n", tok.Scope)
			if !tok.ExpiresAt.IsZero() {
				days := int(time.Until(tok.ExpiresAt).Hours() / 24)
				fmt.Fprintf(out, "Expires at: %s (~%d days remaining)\n", tok.ExpiresAt.Format(time.RFC3339), days)
			}
			fmt.Fprintf(out, "Saved to:   %s (mode 0600)\n", t3.DefaultMCPOAuthPath())

			return nil
		},
	}

	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not attempt to open browser automatically; print authorization URL instead")
	return cmd
}

func newT3AuthStatusCmd() *cobra.Command {
	var formatStr string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check T3 Code OAuth and session token status",
		RunE: func(cmd *cobra.Command, args []string) error {
			oauthPath := t3.DefaultMCPOAuthPath()
			oauthStatus := t3.InspectOAuthToken(oauthPath)

			// Also check session token
			sessionPath := t3.DefaultSessionFilePath()
			sessionExists := false
			sessionExpiry := ""
			if data, err := os.ReadFile(sessionPath); err == nil {
				var stored t3.StoredSessionToken
				if json.Unmarshal(data, &stored) == nil && stored.Token != "" {
					sessionExists = true
					if !stored.ExpiresAt.IsZero() {
						sessionExpiry = stored.ExpiresAt.Format(time.RFC3339)
					}
				}
			}

			// Fast ping to check running server
			discovery := t3.NewServerDiscovery()
			serverOnline := false
			serverOrigin := ""
			if runtime, err := discovery.DiscoverRuntime(); err == nil {
				serverOrigin = runtime.Origin
				serverOnline = true
			}

			format := strings.ToLower(formatStr)
			if format == "" || format == "json" {
				resp := map[string]any{
					"mcp_oauth": map[string]any{
						"status":          oauthStatus.Status,
						"file_path":       oauthStatus.FilePath,
						"exists":          oauthStatus.Exists,
						"scope":           oauthStatus.Scope,
						"expires_at":      oauthStatus.ExpiresAt,
						"hours_remaining": oauthStatus.HoursRemaining,
						"warning":         oauthStatus.WarningMessage,
					},
					"session_token": map[string]any{
						"file_path":  sessionPath,
						"exists":     sessionExists,
						"expires_at": sessionExpiry,
					},
					"server": map[string]any{
						"online": serverOnline,
						"origin": serverOrigin,
					},
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			// Text format
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "T3 Code Authentication Status")
			fmt.Fprintln(out, strings.Repeat("-", 40))
			if serverOnline {
				fmt.Fprintf(out, "Server Origin:   %s (online)\n", serverOrigin)
			} else {
				fmt.Fprintln(out, "Server Origin:   (offline / not discovered)")
			}
			fmt.Fprintf(out, "OAuth Token:     %s\n", strings.ToUpper(oauthStatus.Status))
			fmt.Fprintf(out, "OAuth Path:      %s\n", oauthPath)
			if oauthStatus.Exists && !oauthStatus.ExpiresAt.IsZero() {
				fmt.Fprintf(out, "OAuth Expires:   %s (~%.1f hours remaining)\n", oauthStatus.ExpiresAt.Format(time.RFC3339), oauthStatus.HoursRemaining)
				fmt.Fprintf(out, "OAuth Scope:     %s\n", oauthStatus.Scope)
			}
			if oauthStatus.WarningMessage != "" {
				fmt.Fprintf(out, "\n⚠️ %s\n", oauthStatus.WarningMessage)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&formatStr, "format", "f", "json", "Output format (json, text)")
	return cmd
}

func newT3SnapshotCmd() *cobra.Command {
	var formatStr string

	cmd := &cobra.Command{
		Use:     "snapshot",
		Aliases: []string{"shell"},
		Short:   "Zero-OAuth orchestration shell snapshot (GET /api/orchestration/shell)",
		RunE: func(cmd *cobra.Command, args []string) error {
			backend := t3.NewT3Backend()
			if err := backend.EnsureInitialized(cmd.Context()); err != nil {
				return fmt.Errorf("failed to connect to T3 Code server: %w", err)
			}

			snapshot, err := backend.HTTPClient().GetShellSnapshot(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to fetch orchestration shell snapshot: %w", err)
			}

			format := strings.ToLower(formatStr)
			if format == "" || format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(snapshot)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "T3 Code Orchestration Shell (Sequence %d, Threads: %d)\n", snapshot.SnapshotSequence, len(snapshot.Threads))
			fmt.Fprintln(out, strings.Repeat("-", 60))
			for _, t := range snapshot.Threads {
				worktree := ""
				if t.WorktreePath != nil {
					worktree = *t.WorktreePath
				}
				branch := ""
				if t.Branch != nil {
					branch = *t.Branch
				}
				fmt.Fprintf(out, "• Thread %s [%s] (%s) - %s (pending: %d)\n", t.ID, t.Status, t.ProviderInstanceID, t.Title, t.EffectivePendingRequestCount())
				if worktree != "" || branch != "" {
					fmt.Fprintf(out, "  Worktree: %s (branch: %s)\n", worktree, branch)
				}
				if t.LastError != nil && *t.LastError != "" {
					fmt.Fprintf(out, "  Error: %s\n", *t.LastError)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&formatStr, "format", "f", "json", "Output format (json, text)")
	return cmd
}
