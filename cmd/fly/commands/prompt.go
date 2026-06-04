package commands

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// IsInteractive returns true if stdin is a real terminal that can be read from.
// Returns false when stdin is a pipe, file, /dev/null, or closed — in those
// cases calling Prompt* would block forever or read EOF and return empty.
func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	if runtime.GOOS != "windows" {
		// /dev/null is a character device on Unix but has no controlling
		// terminal. Opening /dev/tty is the canonical way to confirm a
		// real interactive session.
		f, err := os.Open("/dev/tty")
		if err != nil {
			return false
		}
		_ = f.Close()
	}
	return true
}

// IsColorTerminal returns true if stdout supports ANSI color codes and the
// user has not requested a colorless output via NO_COLOR, --no-color, or
// TERM=dumb. See https://no-color.org for the convention.
func IsColorTerminal() bool {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("NO_COLOR"))); v != "" && v != "0" && v != "false" {
		return false
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("FF_NO_COLOR"))); v != "" && v != "0" && v != "false" {
		return false
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))); v == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Prompt asks the user a question and returns their answer.
func Prompt(question, defaultVal string) string {
	if !IsInteractive() {
		return defaultVal
	}
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", question, defaultVal)
	} else {
		fmt.Printf("%s: ", question)
	}
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultVal
	}
	return answer
}

// PromptSelect asks the user to choose from a list of options.
func PromptSelect(question string, options []string, defaultVal string) string {
	if !IsInteractive() {
		return defaultVal
	}
	fmt.Printf("%s\n", question)
	for i, opt := range options {
		marker := " "
		if opt == defaultVal {
			marker = ">"
		}
		fmt.Printf("  %s %d) %s\n", marker, i+1, opt)
	}
	if defaultVal != "" {
		fmt.Printf("Choice [%s]: ", defaultVal)
	} else {
		fmt.Printf("Choice: ")
	}
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultVal
	}
	for i, opt := range options {
		if answer == fmt.Sprintf("%d", i+1) || strings.EqualFold(answer, opt) {
			return opt
		}
	}
	return answer
}

// PromptConfirm asks a yes/no question.
func PromptConfirm(question string, defaultYes bool) bool {
	if !IsInteractive() {
		return defaultYes
	}
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("%s [%s]: ", question, hint)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

// resolveBaseURL returns the API base URL, checking env then config then default.
func resolveBaseURL() string {
	if baseURL := os.Getenv("FF_API_URL"); baseURL != "" {
		return baseURL
	}
	if cfg, _ := LoadConfig(); cfg != nil && cfg.API.URL != "" {
		return cfg.API.URL
	}
	return "https://api.functionfly.com"
}

// resolveAuthSiteURL returns the dashboard / auth site base URL used for
// user-facing links (the API keys page, OAuth landing, etc.).
//
// Resolution order:
//  1. FF_AUTH_SITE_URL env var (validated; falls back to default if unsafe)
//  2. Derived from FF_API_URL when it points at a local dev host
//     (defaults the dashboard to http://localhost:3000)
//  3. https://functionfly.com
func resolveAuthSiteURL() string {
	if v := os.Getenv("FF_AUTH_SITE_URL"); v != "" {
		if u, err := url.Parse(v); err == nil && u.Scheme != "" && u.Host != "" {
			return strings.TrimRight(v, "/")
		}
		fmt.Fprintf(os.Stderr, "⚠️  Ignoring invalid FF_AUTH_SITE_URL=%q — using default\n", v)
	}
	if api := os.Getenv("FF_API_URL"); api != "" {
		if u, err := url.Parse(api); err == nil {
			host := u.Hostname()
			if host == "localhost" || host == "127.0.0.1" || host == "::1" {
				return fmt.Sprintf("%s://localhost:3000", schemeFor(u))
			}
		}
	}
	return "https://functionfly.com"
}

// schemeFor returns "http" for loopback dev URLs, otherwise "https".
func schemeFor(u *url.URL) string {
	if u.Scheme == "http" {
		return "http"
	}
	return "https"
}

// resolveExpiresAt returns the token expiry time. If the API-provided time is
// zero or far in the future (>90 days), it falls back to a default 30-day TTL.
func resolveExpiresAt(apiExpiresAt string) time.Time {
	defaultTTL := 30 * 24 * time.Hour
	if apiExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, apiExpiresAt); err == nil {
			if !t.IsZero() && t.After(time.Now()) {
				if t.Before(time.Now().Add(90 * 24 * time.Hour)) {
					return t
				}
			}
		}
	}
	return time.Now().Add(defaultTTL)
}

// openBrowser opens the given URL in the system default browser.
// It returns an error if the browser cannot be launched.
func openBrowser(targetURL string) error {
	u, err := url.Parse(targetURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open URL with unsupported scheme or missing host: %s", targetURL)
	}
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{u.String()}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", u.String()}
	default:
		cmd = "xdg-open"
		args = []string{u.String()}
	}
	err = exec.Command(cmd, args...).Run()
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return nil
}
