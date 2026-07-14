package config

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var runnerWindowUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func CanonicalThreadID(value string) (string, error) {
	thread := value
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "ampcode.com" {
			return "", fmt.Errorf("thread must be an Amp thread ID or https://ampcode.com/threads/... URL")
		}
		const prefix = "/threads/"
		if !strings.HasPrefix(parsed.Path, prefix) || strings.Contains(strings.TrimPrefix(parsed.Path, prefix), "/") {
			return "", fmt.Errorf("thread must be an Amp thread ID or https://ampcode.com/threads/... URL")
		}
		thread = strings.TrimPrefix(parsed.Path, prefix)
	}
	if !strings.HasPrefix(thread, "T-") || len(thread) == 2 || strings.ContainsAny(thread, "\t\n\r/ ?#") {
		return "", fmt.Errorf("invalid Amp thread ID %q", value)
	}
	return thread, nil
}

func CanonicalWorkdir(value string) (string, error) {
	if err := validateField("workdir", value); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(ExpandHome(value))
	if err != nil {
		return "", fmt.Errorf("resolve workdir %q: %w", value, err)
	}
	return filepath.Clean(abs), nil
}

// RunnerWindow derives the private tmux window name from canonical runner
// identity. The hash makes equal basenames collision-safe across the machine.
func RunnerWindow(workdir string) string {
	canonical, err := CanonicalWorkdir(workdir)
	if err != nil {
		canonical = filepath.Clean(workdir)
	}
	base := runnerWindowUnsafe.ReplaceAllString(filepath.Base(canonical), "-")
	base = strings.Trim(base, "-")
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "workdir"
	}
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("runner-%s-%x", base, sum[:6])
}
