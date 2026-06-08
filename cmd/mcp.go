package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

// dotnetExtractDir is where a self-contained single-file .NET binary unpacks its
// native libraries inside the container. Kept on the container's ephemeral /tmp so
// the bind-mounted binary directory can stay read-only.
const dotnetExtractDir = "/tmp/intuneme-mcp-extract"

var mcpBinaryFlag string

// runMCP launches an MCP server binary in the foreground inside the container so
// its stdio is wired straight to the caller (VS Code). The host binary directory
// is bind-mounted at runtime, so the binary lives outside the rootfs and the setup
// survives `intuneme recreate` — the bind is re-established on demand.
func runMCP(r runner.Runner, root, binaryPath string, serverArgs []string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	if _, err := os.Stat(cfg.RootfsPath); err != nil {
		return fmt.Errorf("not initialized — run 'intuneme init' first")
	}

	if !nspawn.IsRunning(r, cfg.MachineName) {
		return fmt.Errorf("container is not running — run 'intuneme start' first")
	}

	if binaryPath == "" {
		binaryPath = cfg.MCPBinary
	}
	if binaryPath == "" {
		return fmt.Errorf("no MCP server binary configured — pass --binary <path> " +
			"or set mcp_binary in config.toml")
	}
	binaryPath, err = filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	if info, err := os.Stat(binaryPath); err != nil {
		return fmt.Errorf("MCP binary not found at %s — place a self-contained "+
			"binary there or pass --binary (or set mcp_binary in config.toml): %w",
			binaryPath, err)
	} else if info.IsDir() {
		return fmt.Errorf("MCP binary path %s is a directory, expected a file", binaryPath)
	}

	hostDir := filepath.Dir(binaryPath)
	containerBin := nspawn.MCPMountDir + "/" + filepath.Base(binaryPath)

	// Re-establish the runtime bind mount if the binary isn't visible inside the
	// container (e.g. after a fresh start or recreate). Idempotent and passwordless.
	if err := nspawn.EnsureBind(r, cfg.MachineName, cfg.HostUser,
		hostDir, nspawn.MCPMountDir, containerBin); err != nil {
		return err
	}

	// Trailing `-- args...` override the configured default args.
	if len(serverArgs) == 0 {
		serverArgs = cfg.MCPArgs
	}

	// Build: env DOTNET_BUNDLE_EXTRACT_BASE_DIR=<tmp> <binary> <args...>
	// The DOTNET_* var only affects self-contained single-file .NET binaries and is
	// harmless otherwise; it keeps their native-library extraction on ephemeral /tmp
	// so the bind-mounted binary directory can stay read-only.
	parts := []string{
		"env",
		"DOTNET_BUNDLE_EXTRACT_BASE_DIR=" + dotnetExtractDir,
		nspawn.ShellQuote(containerBin),
	}
	for _, a := range serverArgs {
		parts = append(parts, nspawn.ShellQuote(a))
	}
	command := strings.Join(parts, " ")

	return nspawn.ExecForeground(r, cfg.MachineName, cfg.HostUser, cfg.HostUID, command)
}

var mcpCmd = &cobra.Command{
	Use:   "mcp [-- args...]",
	Short: "Run an MCP server inside the container, wired to stdio",
	Long: `Run a self-contained MCP server binary inside the container in the
foreground, with its stdin/stdout/stderr attached to the caller. This lets a
host-side client (e.g. VS Code) speak to an MCP server that must authenticate
against the container's enrolled tenant — without enabling the broker proxy.

The binary lives on the host (set its path with --binary or the mcp_binary config
key). Its directory is bind-mounted into the container at runtime, so it stays out
of the rootfs and survives 'intuneme recreate'. Any MCP server works — there is no
assumption about a particular tool.

Arguments for the server binary come from the mcp_args config key by default, and
trailing 'intuneme mcp -- args...' override them. For a server whose stdio mode is
a subcommand, set e.g. mcp_args = ["mcp"] in config.toml so the VS Code config
stays minimal:

  {
    "servers": {
      "my-server": { "type": "stdio", "command": "intuneme", "args": ["mcp"] }
    }
  }

Here "my-server" is just the display name in VS Code; the server's own arguments
live in mcp_args.`,
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			var err error
			root, err = config.DefaultRoot()
			if err != nil {
				return err
			}
		}
		return runMCP(r, root, mcpBinaryFlag, args)
	},
}

func init() {
	mcpCmd.Flags().StringVar(&mcpBinaryFlag, "binary", "",
		"path to the MCP server binary on the host (overrides mcp_binary config)")
	rootCmd.AddCommand(mcpCmd)
}
