// Package runtime detects whether podman or docker is available and exposes a
// uniform interface for the parts of the tool that need to talk to the
// container runtime (compose up/down/ps, version banner).
package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindPodman
	KindDocker
)

func (k Kind) String() string {
	switch k {
	case KindPodman:
		return "podman"
	case KindDocker:
		return "docker"
	default:
		return "unknown"
	}
}

type Runtime struct {
	Kind       Kind
	Binary     string   // e.g. "podman" or "docker"
	Version    string   // parsed semver, best effort
	ComposeCmd []string // e.g. ["podman-compose"] or ["docker","compose"]
	ComposeVer string
}

// Detect prefers Podman (per assignment) then falls back to Docker. Override
// forces one or the other if non-empty.
func Detect(override string) (*Runtime, error) {
	switch strings.ToLower(override) {
	case "podman":
		return detectPodman()
	case "docker":
		return detectDocker()
	case "":
	default:
		return nil, fmt.Errorf("unknown runtime override %q (use podman|docker)", override)
	}

	if rt, err := detectPodman(); err == nil {
		return rt, nil
	}
	if rt, err := detectDocker(); err == nil {
		return rt, nil
	}
	return nil, errors.New("no container runtime found (need podman or docker)")
}

func detectPodman() (*Runtime, error) {
	bin, err := exec.LookPath("podman")
	if err != nil {
		return nil, err
	}
	ver, _ := runVersion(bin, "--version")
	rt := &Runtime{Kind: KindPodman, Binary: bin, Version: ver}

	// Prefer the native `podman compose` subcommand if present (podman 4.7+),
	// otherwise fall back to standalone podman-compose.
	if _, err := exec.LookPath("podman-compose"); err == nil {
		rt.ComposeCmd = []string{"podman-compose"}
		rt.ComposeVer, _ = runVersion("podman-compose", "--version")
	} else {
		rt.ComposeCmd = []string{bin, "compose"}
		rt.ComposeVer = ver
	}
	return rt, nil
}

func detectDocker() (*Runtime, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, err
	}
	ver, _ := runVersion(bin, "--version")
	rt := &Runtime{Kind: KindDocker, Binary: bin, Version: ver}

	// Modern docker uses the `docker compose` plugin; only fall back to the
	// legacy docker-compose binary if the plugin is missing.
	if err := exec.Command(bin, "compose", "version").Run(); err == nil {
		rt.ComposeCmd = []string{bin, "compose"}
		rt.ComposeVer, _ = runVersion(bin, "compose", "version", "--short")
	} else if dc, err := exec.LookPath("docker-compose"); err == nil {
		rt.ComposeCmd = []string{dc}
		rt.ComposeVer, _ = runVersion(dc, "--version")
	} else {
		return nil, errors.New("docker found but no compose plugin or docker-compose binary available")
	}
	return rt, nil
}

func runVersion(bin string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
