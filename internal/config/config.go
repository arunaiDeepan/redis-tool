// Package config centralizes all tunables. Reads `redis-tool.yaml` from the
// repo root (or wherever --config points), with env-var overrides via Viper
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	// Paths (resolved to absolute at load time).
	RepoRoot   string
	AnsibleDir string
	InfraDir   string
	LogsDir    string
	OutputDir  string
	RunDir     string // ansible playbooks drop status.json/verify.json here
	SSHKeyPath string

	// Versions
	DefaultRedisVersion string

	// Topology
	NodeCount         int
	Masters           int
	ReplicasPerMaster int

	// Runtime behavior
	Verbose bool
	DryRun  bool
	JSON    bool
	Yes     bool
	NoColor bool

	// Container runtime override (empty = autodetect, prefer podman)
	RuntimeOverride string
}

// Load resolves config from CWD upward, env vars, and CLI flags
// CLI flags are bound by the cmd package before Load() is called
func Load() (*Config, error) {
	v := viper.GetViper()

	v.SetConfigName("redis-tool")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("$HOME/.config/redis-tool")

	v.SetEnvPrefix("REDIS_TOOL")
	v.AutomaticEnv()

	// Defaults - every key the rest of the binary reads has a default here so
	// a missing config file is fine.
	v.SetDefault("default_redis_version", "7.0.15")
	v.SetDefault("node_count", 6)
	v.SetDefault("masters", 3)
	v.SetDefault("replicas_per_master", 1)
	v.SetDefault("ssh_key_path", "~/.ssh/redis_tool_id_ed25519")
	v.SetDefault("ansible_dir", "ansible")
	v.SetDefault("infra_dir", "infra")
	v.SetDefault("logs_dir", "logs")
	v.SetDefault("output_dir", "output")
	v.SetDefault("run_dir", ".run")

	// Missing file is not an error - defaults are enough.
	if err := v.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	c := &Config{
		RepoRoot:            repoRoot,
		AnsibleDir:          absUnder(repoRoot, v.GetString("ansible_dir")),
		InfraDir:            absUnder(repoRoot, v.GetString("infra_dir")),
		LogsDir:             absUnder(repoRoot, v.GetString("logs_dir")),
		OutputDir:           absUnder(repoRoot, v.GetString("output_dir")),
		RunDir:              absUnder(repoRoot, v.GetString("run_dir")),
		SSHKeyPath:          expandHome(v.GetString("ssh_key_path")),
		DefaultRedisVersion: v.GetString("default_redis_version"),
		NodeCount:           v.GetInt("node_count"),
		Masters:             v.GetInt("masters"),
		ReplicasPerMaster:   v.GetInt("replicas_per_master"),
		Verbose:             v.GetBool("verbose"),
		DryRun:              v.GetBool("dry_run"),
		JSON:                v.GetBool("json"),
		Yes:                 v.GetBool("yes"),
		NoColor:             v.GetBool("no_color"),
		RuntimeOverride:     v.GetString("runtime"),
	}

	if err := os.MkdirAll(c.LogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	if err := os.MkdirAll(c.RunDir, 0o755); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	return c, nil
}

func absUnder(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}

	return filepath.Join(root, p)
}

func expandHome(p string) string {
	if len(p) > 0 && p[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}

	return p
}
