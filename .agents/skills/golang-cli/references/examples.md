# golang-cli examples

These examples are for reference only. They come from upstream commit `bb1d43d98a3f8ab639a0042f90d0ec524e39a477`, `skills/golang-cli/assets/examples`. Frostyard ships them as Markdown so that consumer repositories hold no Go source from a skill.

## main.go

```go
// cmd/myapp/main.go
package main

import (
	"os"
)

func main() {
	if err := Execute(); err != nil {
		os.Exit(1)
	}
}
```

## root.go

```go
// cmd/myapp/root.go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "myapp",
	Short: "A brief description of your application",
	Long:  "A longer description with usage examples.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfig()
	},
	SilenceUsage:  true, // don't print usage on errors from RunE
	SilenceErrors: true, // handle error printing yourself
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.myapp.yaml)")
	rootCmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level"))
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("finding home directory: %w", err)
		}
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigName(".myapp")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("MYAPP")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	// Set up logging based on config
	level := slog.LevelInfo
	switch strings.ToLower(viper.GetString("log-level")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	return nil
}
```

## serve.go

```go
// cmd/myapp/serve.go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		port := viper.GetInt("port")
		fmt.Fprintf(cmd.OutOrStdout(), "listening on :%d\n", port)
		// start server...
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
	viper.BindPFlag("port", serveCmd.Flags().Lookup("port"))
}

// For command groups, use AddGroup and set GroupID on commands:
//
//   rootCmd.AddGroup(&cobra.Group{ID: "management", Title: "Management Commands:"})
//   serveCmd.GroupID = "management"
```

## flags.go

```go
package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func flagExamples() {
	// === Persistent vs Local ===

	// Persistent — inherited by all subcommands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")

	// Local — only for this command
	serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")

	// === Required Flags ===

	serveCmd.Flags().String("host", "", "hostname to bind to")
	serveCmd.MarkFlagRequired("host")

	// Mutually exclusive flags
	rootCmd.MarkFlagsMutuallyExclusive("json", "yaml")

	// At least one required
	rootCmd.MarkFlagsOneRequired("output-file", "stdout")

	// === Flag Validation with RegisterFlagCompletionFunc ===

	serveCmd.Flags().String("env", "dev", "environment (dev, staging, prod)")
	serveCmd.RegisterFlagCompletionFunc("env", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"dev", "staging", "prod"}, cobra.ShellCompDirectiveNoFileComp
	})

	// === Always Bind Flags to Viper ===
	// This ensures viper.GetInt("port") returns the flag value, env var MYAPP_PORT,
	// or config file value — whichever has highest precedence.

	serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
	viper.BindPFlag("port", serveCmd.Flags().Lookup("port"))
}
```

## args.go

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cobra provides built-in validators for positional arguments.
// See the table in SKILL.md for all available validators.
var deployCmd = &cobra.Command{
	Use:   "deploy [environment]",
	Short: "Deploy to an environment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := args[0]
		_ = env
		// deploy...
		return nil
	},
}

// Custom validation example:
var deployWithValidationCmd = &cobra.Command{
	Use:   "deploy [environment]",
	Short: "Deploy to an environment",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("expected exactly 1 argument, got %d", len(args))
		}
		valid := map[string]bool{"dev": true, "staging": true, "prod": true}
		if !valid[args[0]] {
			return fmt.Errorf("invalid environment %q, must be one of: dev, staging, prod", args[0])
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// deploy...
		return nil
	},
}
```

## config.go

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// === Complete Cobra + Viper Integration ===

func initConfigComplete() error {
	// 1. Config file
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile) // explicit path
	} else {
		home, _ := os.UserHomeDir()
		viper.AddConfigPath(home) // search $HOME
		viper.AddConfigPath(".")  // search current dir
		viper.SetConfigName(".myapp")
		viper.SetConfigType("yaml")
	}

	// 2. Environment variables
	viper.SetEnvPrefix("MYAPP")                            // MYAPP_PORT, MYAPP_LOG_LEVEL
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_")) // log-level → MYAPP_LOG_LEVEL
	viper.AutomaticEnv()                                   // bind all env vars automatically

	// 3. Read config file (ignore "not found")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	return nil
}

// === Unmarshaling into Structs ===

type Config struct {
	Port     int    `mapstructure:"port"`
	Host     string `mapstructure:"host"`
	LogLevel string `mapstructure:"log-level"`
	Database struct {
		DSN     string `mapstructure:"dsn"`
		MaxConn int    `mapstructure:"max-conn"`
	} `mapstructure:"database"`
}

func loadConfig() (Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshaling config: %w", err)
	}
	return cfg, nil
}

// === Watching Config File Changes ===
// For long-running CLIs (servers, daemons):

func watchConfig() {
	viper.OnConfigChange(func(e fsnotify.Event) {
		slog.Info("config file changed", "file", e.Name)
		// re-read and apply config
	})
	viper.WatchConfig()
}
```

## version.go

```go
// cmd/myapp/version.go
package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Set via ldflags
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "myapp %s (commit: %s, built: %s)\n", version, commit, date)

		if info, ok := debug.ReadBuildInfo(); ok {
			fmt.Fprintf(cmd.OutOrStdout(), "go: %s\n", info.GoVersion)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// Build with:
//
//   go build -ldflags "-X github.com/you/myapp/cmd/myapp.version=1.2.3 \
//     -X github.com/you/myapp/cmd/myapp.commit=$(git rev-parse --short HEAD) \
//     -X github.com/you/myapp/cmd/myapp.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
//     -o bin/myapp ./cmd/myapp
```

## Exit codes

Upstream file: `exit_codes.go`.

```go
package main

import (
	"errors"
	"os"

	"github.com/you/myapp/cmd"
)

// Pattern for mapping errors to exit codes.
func mainWithExitCodes() {
	if err := cmd.Execute(); err != nil {
		// Cobra already printed the error via RunE
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }
```

## output.go

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// === stdout vs stderr ===
// stdout: Program output (data, results). This is what gets piped.
// stderr: Logs, progress, errors, diagnostics. Not piped by default.

func outputExample(cmd *cobra.Command, result string, err error) {
	// Output data to stdout (pipeable)
	fmt.Fprintln(cmd.OutOrStdout(), result)

	// Logs and errors to stderr (use slog)
	// slog.Error("operation failed", "error", err)
}

// === Detecting Pipe vs Terminal ===

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// === Machine-Readable Output ===
// Support --output flag for different output formats.

type User struct {
	ID   string
	Name string
}

func printUsers(cmd *cobra.Command, users []User) error {
	format, _ := cmd.Flags().GetString("output")
	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(users)
	case "plain":
		for _, u := range users {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", u.ID, u.Name)
		}
	default: // "table"
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME")
		for _, u := range users {
			fmt.Fprintf(w, "%s\t%s\n", u.ID, u.Name)
		}
		w.Flush()
	}
	return nil
}

// === Colors ===
// Use fatih/color — it auto-disables when output is not a terminal.

func colorExamples(cmd *cobra.Command, env string, err error) {
	color.Green("Success: deployed to %s", env)
	color.Red("Error: %v", err)

	// Or for reusable styles
	success := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Fprintf(cmd.OutOrStdout(), "%s deployed\n", success("v1.2.3"))
}
```

## signal.go

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// Use signal.NotifyContext to propagate cancellation through context.
var serveWithSignalCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the server",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		srv := &http.Server{Addr: ":8080"}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv.Shutdown(shutdownCtx)
		}()

		slog.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			return fmt.Errorf("server failed: %w", err)
		}
		return nil
	},
}
```

## completion.go

```go
package main

import (
	"os"

	"github.com/spf13/cobra"
)

// === Shell Completion Command ===
// Cobra generates completions for bash, zsh, fish, and PowerShell automatically.

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion script",
		Args:      cobra.ExactValidArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return nil
		},
	})
}

// === Custom Completions ===
// Add custom completions for flags and arguments.

func customCompletionExamples() {
	deployCmd.RegisterFlagCompletionFunc("env", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{
			"dev\tDevelopment environment",
			"staging\tStaging environment",
			"prod\tProduction environment",
		}, cobra.ShellCompDirectiveNoFileComp
	})

	// Dynamic argument completion
	deployCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return getAvailableServices(), cobra.ShellCompDirectiveNoFileComp
	}
}

func getAvailableServices() []string {
	// fetch available services dynamically
	return nil
}
```

## CLI tests

Upstream file: `cli_test.go`.

```go
package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

// Test commands by executing them programmatically and capturing output.
// Use cmd.OutOrStdout() and cmd.ErrOrStderr() in commands (instead of
// os.Stdout / os.Stderr) so output can be captured in tests.

func executeCommand(root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestServeCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "default port",
			args: []string{"serve"},
			want: "listening on :8080\n",
		},
		{
			name: "custom port",
			args: []string{"serve", "--port", "9090"},
			want: "listening on :9090\n",
		},
		{
			name:    "missing required flag",
			args:    []string{"serve", "--host", ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := executeCommand(rootCmd, tt.args...)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}
```

## go.mod

```text
module github.com/you/myapp/cmd/myapp
```
