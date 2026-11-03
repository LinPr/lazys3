// Package cmd wires the lazys3 cobra entry point to the TUI.
package cmd

import (
	"fmt"
	"io"
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/config"
	"github.com/LinPr/lazys3/internal/tui"
	"github.com/LinPr/lazys3/internal/tui/components/style"
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
	// Profile is the --profile value: when set, the TUI selects that
	// profile as soon as the AWS shared config has been read instead of
	// waiting for the user to pick one.
	Profile string
	// Start is the optional positional s3://bucket/prefix argument; it
	// overrides the selected profile's configured start location.
	Start string
	// startLoc is the parsed Start value (filled by Complete).
	startLoc *tui.StartLocation
}

func NewRootCmd() *cobra.Command {
	o := &Options{}
	cmd := &cobra.Command{
		Use:   "lazys3 [s3://bucket/prefix/]",
		Short: "A tui tool to operate S3 assets.",
		Long: `A terminal UI for S3-compatible object storage.

The optional positional argument opens a bucket/prefix directly at startup,
without calling ListBuckets. It is useful for bucket-scoped credentials that
cannot list buckets; --profile also skips the interactive profile picker.`,
		Version: Version,
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
	cmd.PersistentFlags().StringVar(&o.Profile, "profile", "", "AWS profile to select on startup (default: interactive picker)")

	return cmd
}

// Complete parses the optional positional start location. An invalid URI
// is a hard error before the TUI starts, so a typo cannot silently open
// the profile picker as if nothing had been passed.
func (o *Options) Complete(args []string) error {
	if len(args) == 1 {
		o.Start = args[0]
	}
	if o.Start == "" {
		return nil
	}
	loc, err := tui.ParseStartLocation(o.Start)
	if err != nil {
		return err
	}
	o.startLoc = &loc
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
	style.Apply(cfg.Theme.Mode, cfg.Theme)
	style.SetNerdFont(cfg.UI.NerdFont)

	// Resolve the AWS shared config/credentials paths once (flag > env >
	// ~/.aws default); the model threads them into the profile picker and
	// every S3 client.
	awsFiles := config.ResolveAWSFiles(o.AWSConfigPath, o.AWSCredentialsPath)

	model := tui.NewLazyS3ModelWithOptions(cfg, awsFiles, tui.Options{
		Profile: o.Profile,
		Start:   o.startLoc,
	})
	// Alt screen, focus reporting and cell-motion mouse moved from
	// tea.NewProgram options to tea.View fields in bubbletea v2.0.8;
	// they are set in the model's View (internal/tui/tui.go).
	p := tea.NewProgram(model)
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
