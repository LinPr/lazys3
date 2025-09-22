package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/LinPr/lazys3/internal/tui"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/spf13/cobra"
)

type Options struct {
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

	return cmd
}

func (o *Options) Complete(args []string) error {
	return nil
}

func (o *Options) Validate() error {
	return nil
}

func (o *Options) Run() error {
	model := tui.NewLazyS3Model()
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
	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		fmt.Println("Error creating log file:", err)
		os.Exit(1)
	}
	defer f.Close()

	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
