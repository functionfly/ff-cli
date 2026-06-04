package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// IsInteractive returns true if stdin is a terminal.
func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// IsColorTerminal returns true if stdout supports ANSI color codes.
func IsColorTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Prompt asks the user a question and returns their answer.
func Prompt(question, defaultVal string) string {
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
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	err := exec.Command(cmd, args...).Run()
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return nil
}
