package commands

import (
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
var messageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("white"))

// ── SpinnerModel ─────────────────────────────────────────────────────────────

type SpinnerModel struct {
	spinner  spinner.Model
	message  string
	quitting bool
	done     bool
	result   error
	mu       sync.Mutex
}

func (m *SpinnerModel) Init() tea.Cmd { return m.spinner.Tick }

func (m *SpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case error:
		m.mu.Lock()
		m.result = msg
		m.done = true
		m.mu.Unlock()
		return m, tea.Quit
	}
	return m, nil
}

func (m *SpinnerModel) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("%s %s", spinnerStyle.Render(m.spinner.View()), messageStyle.Render(m.message))
}

func newSpinnerModel(message string) *SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return &SpinnerModel{spinner: s, message: message}
}

// WithSpinner runs fn with a spinner. Falls back to plain output in verbose/debug/non-interactive mode.
func WithSpinner(message string, fn func() error) error {
	if VerboseMode || DebugMode || !IsInteractive() {
		fmt.Printf("%s...\n", message)
		return fn()
	}

	model := newSpinnerModel(message)
	program := tea.NewProgram(model)

	done := make(chan error, 1)
	quit := make(chan struct{})

	go func() {
		done <- fn()
		close(quit)
	}()

	go func() {
		<-quit
		model.mu.Lock()
		model.done = true
		model.mu.Unlock()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("spinner error: %w", err)
	}
	return <-done
}

// ── ProgressModel ────────────────────────────────────────────────────────────

type ProgressModel struct {
	spinner  spinner.Model
	message  string
	current  int
	total    int
	quitting bool
	done     bool
	result   error
	mu       sync.Mutex
}

func (m *ProgressModel) Init() tea.Cmd { return m.spinner.Tick }

func (m *ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case error:
		m.mu.Lock()
		m.result = msg
		m.done = true
		m.mu.Unlock()
		return m, tea.Quit
	}
	return m, nil
}

func (m *ProgressModel) View() string {
	if m.quitting {
		return ""
	}
	percentage := 0
	if m.total > 0 {
		percentage = (m.current * 100) / m.total
	}
	barWidth := 30
	filled := 0
	if m.total > 0 {
		filled = int((int64(barWidth) * int64(m.current)) / int64(m.total))
	}
	bar := ""
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return fmt.Sprintf("%s %s [%s] %d%%",
		spinnerStyle.Render(m.spinner.View()),
		messageStyle.Render(m.message),
		lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(bar),
		percentage)
}

func newProgressModel(message string, current, total int) *ProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return &ProgressModel{spinner: s, message: message, current: current, total: total}
}

// WithProgress runs fn with a progress bar. fn receives an updater callback.
func WithProgress(message string, current, total int, fn func(updater func(int, int) error) error) error {
	if VerboseMode || DebugMode || !IsInteractive() {
		fmt.Printf("%s...\n", message)
		updater := func(c, t int) error {
			fmt.Printf("  Progress: %d/%d (%d%%)\n", c, t, (c*100)/t)
			return nil
		}
		return fn(updater)
	}

	model := newProgressModel(message, current, total)
	program := tea.NewProgram(model)

	done := make(chan error, 1)
	quit := make(chan struct{})

	updater := func(c, t int) error {
		model.mu.Lock()
		model.current = c
		model.total = t
		model.mu.Unlock()
		return nil
	}

	go func() {
		done <- fn(updater)
		close(quit)
	}()

	go func() {
		<-quit
		model.mu.Lock()
		model.done = true
		model.mu.Unlock()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("progress error: %w", err)
	}
	return <-done
}

// ── FileProgressModel ─────────────────────────────────────────────────────────

type FileProgressModel struct {
	spinner   spinner.Model
	message   string
	current   int64
	total     int64
	quitting  bool
	done      bool
	result    error
	mu        sync.Mutex
	startTime time.Time
}

func (m *FileProgressModel) Init() tea.Cmd { return m.spinner.Tick }

func (m *FileProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case error:
		m.mu.Lock()
		m.result = msg
		m.done = true
		m.mu.Unlock()
		return m, tea.Quit
	}
	return m, nil
}

func (m *FileProgressModel) View() string {
	if m.quitting {
		return ""
	}
	percentage := 0
	eta := "calculating..."
	if m.total > 0 {
		percentage = int((m.current * 100) / m.total)
		elapsed := time.Since(m.startTime)
		if m.current > 0 && elapsed.Seconds() > 0 {
			bps := float64(m.current) / elapsed.Seconds()
			rem := m.total - m.current
			etaSec := int(float64(rem) / bps)
			switch {
			case etaSec < 60:
				eta = fmt.Sprintf("%ds", etaSec)
			case etaSec < 3600:
				eta = fmt.Sprintf("%dm %ds", etaSec/60, etaSec%60)
			default:
				eta = fmt.Sprintf("%dh %dm", etaSec/3600, (etaSec%3600)/60)
			}
		}
	}
	barWidth := 30
	filled := 0
	if m.total > 0 {
		filled = int((int64(barWidth) * m.current) / m.total)
	}
	bar := ""
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return fmt.Sprintf("%s %s [%s] %d%% | %s | ETA: %s\n    %s / %s",
		spinnerStyle.Render(m.spinner.View()),
		messageStyle.Render(m.message),
		lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(bar),
		percentage,
		formatSpeed(m.current, m.startTime),
		eta,
		formatBytes(m.current),
		formatBytes(m.total))
}

func newFileProgressModel(message string, current, total int64) *FileProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return &FileProgressModel{spinner: s, message: message, current: current, total: total, startTime: time.Now()}
}

// FileProgressUpdater is a callback for file progress updates.
type FileProgressUpdater func(current, total int64)

// WithFileProgress runs fn with file-level progress tracking.
func WithFileProgress(message string, totalSize int64, fn func(FileProgressUpdater) error) error {
	if VerboseMode || DebugMode || !IsInteractive() {
		fmt.Printf("%s...\n", message)
		return fn(func(current, total int64) {
			if total > 0 {
				fmt.Printf("  Uploaded: %d / %d bytes (%d%%)\n", current, total, (current*100)/total)
			}
		})
	}

	model := newFileProgressModel(message, 0, totalSize)
	program := tea.NewProgram(model)

	done := make(chan error, 1)
	quit := make(chan struct{})

	updater := func(current, total int64) {
		model.mu.Lock()
		model.current = current
		model.total = total
		model.mu.Unlock()
	}

	go func() {
		done <- fn(updater)
		close(quit)
	}()

	go func() {
		<-quit
		model.mu.Lock()
		model.done = true
		model.mu.Unlock()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("file progress error: %w", err)
	}
	return <-done
}

// ── helpers ───────────────────────────────────────────────────────────────────

func formatSpeed(current int64, start time.Time) string {
	elapsed := time.Since(start)
	if elapsed.Seconds() == 0 || current == 0 {
		return "0 B/s"
	}
	return fmt.Sprintf("%s/s", formatBytes(int64(float64(current)/elapsed.Seconds())))
}

func formatBytes(b int64) string {
	const unit = int64(1024)
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ── SimpleSpinner ─────────────────────────────────────────────────────────────

// SimpleSpinner is a no-op spinner for non-TTY environments.
type SimpleSpinner struct{ message string }

func NewSimpleSpinner(message string) *SimpleSpinner { return &SimpleSpinner{message: message} }

func (s *SimpleSpinner) Start() {
	if VerboseMode || DebugMode || !IsInteractive() {
		fmt.Printf("%s...\n", s.message)
	}
}

func (s *SimpleSpinner) Stop() {}
