// Package cmd is the Cobra command tree for redis-tool.
//
// Every subcommand goes through `runWithPrereq` so the dependency check fires
// before any real work. Global flags (--verbose, --dry-run, --json, --yes,
// --no-color, --config) live here.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/redis-tool/redis-tool/internal/config"
	"github.com/redis-tool/redis-tool/internal/logging"
	"github.com/redis-tool/redis-tool/internal/prereq"
	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfg    *config.Config
	logger *logging.Logger
)

var rootCmd = &cobra.Command{
	Use:   "redis-tool",
	Short: "Provision, operate, and rolling-upgrade a Redis Cluster via Ansible.",
	Long: `redis-tool wraps Ansible to manage a 6-node Redis Cluster in containers.
The cluster runs as 3 masters + 3 replicas; upgrades are rolling with zero
client-visible downtime and verified data integrity.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return err
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags bound to viper so config file values still apply.
	pf := rootCmd.PersistentFlags()
	pf.Bool("verbose", false, "verbose output (passes -v to ansible-playbook)")
	pf.Bool("dry-run", false, "print ansible-playbook commands without running them")
	pf.Bool("json", false, "machine-readable JSON output where supported")
	pf.Bool("yes", false, "skip confirmation prompts")
	pf.Bool("no-color", false, "disable colored output")
	pf.String("runtime", "", "container runtime override: podman|docker")
	pf.String("config", "", "config file path (default: ./redis-tool.yaml)")
	_ = viper.BindPFlag("verbose", pf.Lookup("verbose"))
	_ = viper.BindPFlag("dry_run", pf.Lookup("dry-run"))
	_ = viper.BindPFlag("json", pf.Lookup("json"))
	_ = viper.BindPFlag("yes", pf.Lookup("yes"))
	_ = viper.BindPFlag("no_color", pf.Lookup("no-color"))
	_ = viper.BindPFlag("runtime", pf.Lookup("runtime"))
}

// runWithPrereq wires the prereq gate, logger, and runner before invoking `fn`.
// Every subcommand (except `doctor` and `version`) goes through this.
func runWithPrereq(cmdName string, fn func(ctx context.Context, r *runner.Runner) error) func(*cobra.Command, []string) error {
	return func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Prereq check first - mandated by the assignment.
		if _, err := prereq.Check(os.Stdout, cfg.RuntimeOverride, cfg.NoColor); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout)

		var err error
		logger, err = logging.New(cfg.LogsDir, cmdName, cfg.Verbose)
		if err != nil {
			return err
		}
		defer logger.Close()
		logger.Step(ctx, cmdName, "start", "args", args)

		r := &runner.Runner{
			AnsibleDir: cfg.AnsibleDir,
			RunDir:     cfg.RunDir,
			DryRun:     cfg.DryRun,
			Verbose:    cfg.Verbose,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		}

		if err := fn(ctx, r); err != nil {
			logger.Step(ctx, cmdName, "error", "err", err.Error())
			return err
		}
		logger.Step(ctx, cmdName, "ok")
		fmt.Fprintf(os.Stderr, "Log: %s\n", logger.Path())
		return nil
	}
}

// confirm prompts unless --yes was passed.
func confirm(prompt string) bool {
	if cfg != nil && cfg.Yes {
		return true
	}
	fmt.Printf("%s [y/N] ", prompt)

	var resp string
	_, _ = fmt.Fscanln(os.Stdin, &resp)

	return resp == "y" || resp == "Y" || resp == "yes"
}

// discard sink for places that don't need output capture.
var _ io.Writer = io.Discard
