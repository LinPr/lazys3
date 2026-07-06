// Package cmd wires the lazys3 cobra entry point to the TUI.
package cmd

import (
	"fmt"
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
				os.Exit(1)
			}

			if err := o.Validate(); err != nil {
				os.Exit(1)
			}

			if err := o.Run(); err != nil {
				os.Exit(1)
			}
		},
	}

	cmd.PersistentFlags().BoolVar(&o.Debug, "debug", false, "Enable debug mode")

	return cmd
}

func (o *Options) Complete(args []string) error {
	return nil
}

func (o *Options) Validate() error {
	return nil
}

func (o *Options) Run() error {
	cleanup := logInit(o.Debug)
	defer cleanup()

	// The config is loaded once and its theme applied to the shared style
	// vars BEFORE any component is constructed (delegates copy the styles
	// at construction time).
	cfg := config.Load()
	style.Apply(cfg.Theme)
	style.SetNerdFont(cfg.UI.NerdFont)

	model := tui.NewLazyS3ModelWithConfig(cfg)
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
// debug is true, or to /dev/null otherwise. It returns a cleanup function
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

	f, err := tea.LogToFile("/dev/null", "debug")
	if err != nil {
		fmt.Println("Error creating log file:", err)
		os.Exit(1)
	}
	return func() { _ = f.Close() }
}
