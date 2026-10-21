// Package opencode wraps the OpenCode CLI (V2) as an agent runner.
package opencode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotFound is returned when the opencode binary is missing.
var ErrNotFound = errors.New("opencode binary not found")

// EnvBin overrides which opencode executable raven runs.
const EnvBin = "RAVEN_OPENCODE_BIN"

// DefaultBin is the default opencode executable name.
const DefaultBin = "opencode"

// MinMajor is the minimum supported OpenCode major version.
const MinMajor = 2

// Version is a parsed OpenCode version.
type Version struct {
	Raw   string
	Major int
	Minor int
	Patch int
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseVersion extracts a semantic version from `opencode --version` output.
func ParseVersion(output string) (Version, error) {
	trimmed := strings.TrimSpace(output)
	m := versionRe.FindStringSubmatch(trimmed)
	if m == nil {
		return Version{Raw: trimmed}, fmt.Errorf("cannot parse opencode version from %q", trimmed)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return Version{Raw: m[0], Major: major, Minor: minor, Patch: patch}, nil
}

// IsV2 reports whether the version is OpenCode V2 or newer.
func (v Version) IsV2() bool { return v.Major >= MinMajor }

// String returns a printable version.
func (v Version) String() string {
	if v.Raw != "" {
		return v.Raw
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// resolveName picks the binary to execute.
// Precedence: explicit name, RAVEN_OPENCODE_BIN, DefaultBin.
func resolveName(name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	if env := strings.TrimSpace(os.Getenv(EnvBin)); env != "" {
		return env
	}
	return DefaultBin
}

// ResolveBin returns the opencode executable path, honouring RAVEN_OPENCODE_BIN.
func ResolveBin(name string) (string, error) {
	resolved := resolveName(name)
	path, err := exec.LookPath(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, resolved)
	}
	return path, nil
}

// DetectVersion runs `opencode --version` and parses the result.
func DetectVersion(ctx context.Context, name string) (Version, error) {
	path, err := ResolveBin(name)
	if err != nil {
		return Version{}, err
	}
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return Version{}, fmt.Errorf("run %s --version: %w", path, err)
	}
	return ParseVersion(string(out))
}
