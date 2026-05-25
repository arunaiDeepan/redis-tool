package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is overridden at link-time:
//
//	go build -ldflags "-X github.com/redis-tool/redis-tool/cmd.Version=v0.1.0"
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print redis-tool version.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("redis-tool %s\n", Version)
	},
}

func init() { rootCmd.AddCommand(versionCmd) }
