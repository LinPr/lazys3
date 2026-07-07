// Package cmd wires the lazys3 cobra entry point to the TUI.
package cmd

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/LinPr/lazys3/internal/config"
	"github.com/LinPr/lazys3/internal/tui"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/spf13/cobra"
)

type Options struct {
	Debug bool
	// ConfigPath is the --config value: an explicit lazys3 config file
	// that replaces the default $XDG_CONFIG_HOME/lazys3/config.yaml.
	ConfigPath string
	// AWSConfigPath / AWSCredentialsPath are the --aws-config /
	// --aws-credentials values; they take precedence over AWS_CONFIG_FILE /
	// AWS_SHARED_CREDENTIALS_FILE and the ~/.aws defaults.
	AWSConfigPath      string
	AWSCredentialsPath string
}

func NewRootCmd() *cobra.Command {
	o := &Options{}
	cmd := &cobra.Command{
		Use:     "lazys3",
		Short:   "A tui tool to operate S3 asserts.",
		Version: "",
		Args:    cobra.MaximumNArgs(1),

		Run: func(cmd *cobra.Command, args []string) {
			if err := o.Complete(args); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			if err := o.Validate(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			if err := o.Run(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}

	cmd.PersistentFlags().BoolVar(&o.Debug, "debug", false, "Enable debug mode")
	cmd.PersistentFlags().StringVar(&o.ConfigPath, "config", "", "lazys3 config file (default $XDG_CONFIG_HOME/lazys3/config.yaml)")
	cmd.PersistentFlags().StringVar(&o.AWSConfigPath, "aws-config", "", "AWS shared config file (default $AWS_CONFIG_FILE, then ~/.aws/config)")
	cmd.PersistentFlags().StringVar(&o.AWSCredentialsPath, "aws-credentials", "", "AWS shared credentials file (default $AWS_SHARED_CREDENTIALS_FILE, then ~/.aws/credentials)")

	return cmd
}

func (o *Options) Complete(args []string) error {
	return nil
}

// Validate rejects explicitly-flagged files that do not exist. Env-var and
// default locations are deliberately not checked: a missing ~/.aws/config
// just yields an empty profile list, exactly as before.
func (o *Options) Validate() error {
	for flag, path := range map[string]string{
		"--aws-config":      o.AWSConfigPath,
		"--aws-credentials": o.AWSCredentialsPath,
	} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s %s: %w", flag, path, err)
		}
	}
	return nil
}

func (o *Options) Run() error {
	cleanup := logInit(o.Debug)
	defer cleanup()

	// The config is loaded once and its theme applied to the shared style
	// vars BEFORE any component is constructed (delegates copy the styles
	// at construction time). An explicit --config pointing at a missing,
	// unreadable or malformed file is a hard error; the default location
	// falls back to defaults. The warning (legacy config.toml hint) goes
	// to stderr so it survives the altscreen and is visible after exit —
	// the standard logger is discarded without --debug.
	cfg, warn, err := config.Load(o.ConfigPath)
	if err != nil {
		return err
	}
	if warn != "" {
		fmt.Fprintln(os.Stderr, warn)
	}
	style.Apply(cfg.Theme)
	style.SetNerdFont(cfg.UI.NerdFont)

	// Resolve the AWS shared config/credentials paths once (flag > env >
	// ~/.aws default); the model threads them into the profile picker and
	// every S3 client.
	awsFiles := config.ResolveAWSFiles(o.AWSConfigPath, o.AWSCredentialsPath)

	model := tui.NewLazyS3ModelWithConfig(cfg, awsFiles)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithReportFocus(),
		tea.WithMouseCellMotion(),
	)
	log.Println("Starting program")
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func Execute() {
	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// logInit redirects the standard logger's output to a debug log file when
// debug is true, or discards it otherwise. It returns a cleanup function
// the caller must defer-close after the program is done, so the file handle
// stays open for the program's lifetime (closing it on return from logInit
// would discard every log line emitted afterwards).
func logInit(debug bool) func() {
	if debug {
		f, err := tea.LogToFile("debug.log", "debug")
		if err != nil {
			fmt.Println("Error creating log file:", err)
			os.Exit(1)
		}
		return func() { _ = f.Close() }
	}

	// No file at all: io.Discard works on every platform, unlike the
	// previous /dev/null (which does not exist on Windows — its null
	// device is named NUL).
	log.SetOutput(io.Discard)
	return func() {}
}
