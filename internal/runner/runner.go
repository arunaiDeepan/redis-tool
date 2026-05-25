// Package runner wraps ansible-playbook invocations. Inputs go in as
// --extra-vars JSON; structured outputs come back through .run/*.json files
// the playbooks drop on the control node.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Runner struct {
	AnsibleDir string // dir containing ansible.cfg
	RunDir     string // .run/ where playbooks write JSON outputs
	DryRun     bool
	Verbose    bool
	Stdout     io.Writer
	Stderr     io.Writer
}

type Result struct {
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
}

// Run invokes ansible-playbook against `playbook` (relative to AnsibleDir),
// passing `extraVars` as one --extra-vars JSON blob, and optionally limiting
// to specific hosts via `limit`.
func (r *Runner) Run(ctx context.Context, playbook string, extraVars map[string]any, limit string) (*Result, error) {
	bin, err := exec.LookPath("ansible-playbook")
	if err != nil {
		return nil, fmt.Errorf("ansible-playbook not found in PATH: %w", err)
	}

	args := []string{filepath.Join("playbooks", playbook)}
	if limit != "" {
		args = append(args, "--limit", limit)
	}
	if r.Verbose {
		args = append(args, "-v")
	}
	if len(extraVars) > 0 {
		blob, err := json.Marshal(extraVars)
		if err != nil {
			return nil, err
		}
		args = append(args, "--extra-vars", string(blob))
	}

	if r.DryRun {
		fmt.Fprintf(r.outW(), "[dry-run] ansible-playbook %s\n", strings.Join(args, " "))
		return &Result{Args: args}, nil
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = r.AnsibleDir
	cmd.Env = append(os.Environ(), "ANSIBLE_FORCE_COLOR=1")

	var outBuf, errBuf bytes.Buffer
	// Tee: live to the user's terminal AND captured for the log file / err msg.
	cmd.Stdout = io.MultiWriter(r.outW(), &outBuf)
	cmd.Stderr = io.MultiWriter(r.errW(), &errBuf)

	runErr := cmd.Run()
	res := &Result{
		Args:     args,
		ExitCode: cmd.ProcessState.ExitCode(),
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
	}
	if runErr != nil {
		return res, fmt.Errorf("ansible-playbook %s failed (exit %d): %w", playbook, res.ExitCode, runErr)
	}

	return res, nil
}

// CleanRunArtifacts removes JSON outputs from a previous run so we never read
// stale data when a playbook silently fails to write its result.
func (r *Runner) CleanRunArtifacts(names ...string) error {
	for _, n := range names {
		p := filepath.Join(r.RunDir, n)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func (r *Runner) outW() io.Writer {
	if r.Stdout != nil {
		return r.Stdout
	}

	return os.Stdout
}

func (r *Runner) errW() io.Writer {
	if r.Stderr != nil {
		return r.Stderr
	}

	return os.Stderr
}
