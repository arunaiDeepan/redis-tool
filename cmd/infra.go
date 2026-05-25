package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/redis-tool/redis-tool/internal/runtime"
	"github.com/spf13/cobra"
)

var infraCmd = &cobra.Command{
	Use:   "infra",
	Short: "Manage the simulated 6-node container infrastructure.",
}

var infraUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Generate SSH keys (if needed), build the image, start all 6 nodes.",
	RunE: runWithPrereq("infra-up", func(ctx context.Context, _ *runner.Runner) error {
		if err := ensureSSHKey(); err != nil {
			return err
		}
		return composeAction(ctx, "up", "-d", "--build")
	}),
}

var infraDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop and remove all 6 containers and the network.",
	RunE: runWithPrereq("infra-down", func(ctx context.Context, _ *runner.Runner) error {
		if !confirm("This will destroy all 6 containers and the network. Continue?") {
			return fmt.Errorf("aborted")
		}
		return composeAction(ctx, "down", "-v")
	}),
}

var infraStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show container status (ps).",
	RunE: runWithPrereq("infra-status", func(ctx context.Context, _ *runner.Runner) error {
		return composeAction(ctx, "ps")
	}),
}

func init() {
	infraCmd.AddCommand(infraUpCmd, infraDownCmd, infraStatusCmd)
	rootCmd.AddCommand(infraCmd)
}

func composeAction(ctx context.Context, args ...string) error {
	rt, err := runtime.Detect(cfg.RuntimeOverride)
	if err != nil {
		return err
	}

	composeFile := filepath.Join(cfg.InfraDir, "compose.yml")
	full := append([]string{}, rt.ComposeCmd...)
	full = append(full, "-f", composeFile)
	full = append(full, args...)

	if cfg.DryRun {
		fmt.Println("[dry-run]", full)
		return nil
	}

	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	cmd.Dir = cfg.RepoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func ensureSSHKey() error {
	pub := filepath.Join(cfg.InfraDir, "keys", "redis_tool_id_ed25519.pub")
	if _, err := os.Stat(pub); err == nil {
		return nil
	}

	// Delegate to scripts/generate-keys.sh - it does the right thing and is
	// inspectable by the user. On Windows/WSL2 this requires bash.
	script := filepath.Join(cfg.RepoRoot, "scripts", "generate-keys.sh")
	cmd := exec.Command("bash", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate ssh key (%s): %w - run it manually or create the key yourself", script, err)
	}

	return nil
}
