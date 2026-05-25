// Package prereq runs the mandatory dependency check that fires before any
// real command logic. Exit non-zero with actionable install hints on failure
package prereq

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/redis-tool/redis-tool/internal/runtime"
)

const minAnsibleMajor, minAnsibleMinor = 2, 14

type Result struct {
	Runtime        *runtime.Runtime
	AnsibleVersion string
	OK             bool
	Messages       []string // human-readable lines (✓/✗)
}

// Check runs the full prereq battery. `out` receives the lines as they're
// produced so users see progress on slow detections
func Check(out io.Writer, runtimeOverride string, noColor bool) (*Result, error) {
	r := &Result{OK: true}

	ok := color.New(color.FgGreen).SprintFunc()
	bad := color.New(color.FgRed).SprintFunc()
	if noColor {
		color.NoColor = true
		ok = fmt.Sprint
		bad = fmt.Sprint
	}

	// 1. Container runtime
	rt, rtErr := runtime.Detect(runtimeOverride)
	if rtErr != nil {
		r.OK = false
		line := fmt.Sprintf("%s Container runtime not found (Docker or Podman)\n", bad("✗"))
		fmt.Fprint(out, line)
		fmt.Fprintln(out, "   Install Podman (recommended): https://podman.io/docs/installation")
		fmt.Fprintln(out, "   Install Docker:               https://docs.docker.com/engine/install/")
		r.Messages = append(r.Messages, line)
	} else {
		r.Runtime = rt
		line := fmt.Sprintf("%s %s %s found\n", ok("✓"), rt.Kind, prettyVer(rt.Version, string(rt.Kind.String())))
		fmt.Fprint(out, line)

		r.Messages = append(r.Messages, line)
		if rt.ComposeVer != "" {
			line = fmt.Sprintf("%s %s compose available (%s)\n", ok("✓"), rt.Kind, prettyVer(rt.ComposeVer, string(rt.Kind.String())))
			fmt.Fprint(out, line)
			r.Messages = append(r.Messages, line)
		}
	}

	// 2. Ansible
	ver, aErr := ansibleVersion()
	if aErr != nil {
		r.OK = false
		fmt.Fprintf(out, "%s Ansible not found\n", bad("✗"))
		fmt.Fprintln(out, "   Install: pip install ansible  (or use your OS package manager)")
	} else if !versionAtLeast(ver, minAnsibleMajor, minAnsibleMinor) {
		r.OK = false
		fmt.Fprintf(out, "%s Ansible %s found - need %d.%d+\n", bad("✗"), ver, minAnsibleMajor, minAnsibleMinor)
		fmt.Fprintln(out, "   Upgrade: pip install --upgrade ansible")
	} else {
		r.AnsibleVersion = ver
		fmt.Fprintf(out, "%s Ansible %s found\n", ok("✓"), ver)
	}

	if !r.OK {
		fmt.Fprintln(out, "\nPlease install the missing dependencies and retry.")
		return r, errors.New("prerequisites not met")
	}
	fmt.Fprintln(out, "Proceeding...")
	return r, nil
}

// prettyVer trims a raw version banner like "podman version 5.6.1" or
// "Docker version 29.3.1, build ..." down to just the semver
func prettyVer(raw, runtimeName string) string {
	semver := regexp.MustCompile(`[0-9]+\.[0-9]+(?:\.[0-9]+)?`).FindString(raw)
	if semver == "" {
		return raw
	}

	return semver
}

var ansibleVerRe = regexp.MustCompile(`(?:ansible-playbook|ansible)\s*\[?core\s*([0-9]+)\.([0-9]+)(?:\.([0-9]+))?`)

func ansibleVersion() (string, error) {
	bin, err := exec.LookPath("ansible-playbook")
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	cmd := exec.Command(bin, "--version")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}

	first := strings.SplitN(out.String(), "\n", 2)[0]
	m := ansibleVerRe.FindStringSubmatch(first)
	if len(m) < 3 {
		// Older ansible-core prints "ansible-playbook 2.16.4"
		fallback := regexp.MustCompile(`([0-9]+)\.([0-9]+)\.?([0-9]+)?`).FindStringSubmatch(first)
		if len(fallback) >= 3 {
			m = fallback
		} else {
			return "", fmt.Errorf("could not parse ansible version from: %q", first)
		}
	}

	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch := "0"
	if len(m) > 3 && m[3] != "" {
		patch = m[3]
	}

	return fmt.Sprintf("%d.%d.%s", major, minor, patch), nil
}

func versionAtLeast(ver string, major, minor int) bool {
	parts := strings.SplitN(ver, ".", 3)
	if len(parts) < 2 {
		return false
	}

	a, _ := strconv.Atoi(parts[0])
	b, _ := strconv.Atoi(parts[1])
	if a != major {
		return a > major
	}

	return b >= minor
}
