package cmd

import (
	"os"

	"github.com/redis-tool/redis-tool/internal/prereq"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Standalone prerequisite check (runtime + ansible).",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := prereq.Check(os.Stdout, cfg.RuntimeOverride, cfg.NoColor)
		return err
	},
}

func init() { rootCmd.AddCommand(doctorCmd) }
