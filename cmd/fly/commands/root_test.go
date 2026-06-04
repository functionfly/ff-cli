package commands

import (
	"bytes"
	"testing"

	"github.com/functionfly/ff-cli/internal/version"
)

// Smoke test: version subcommand runs without error.
func TestRootCmd_Version(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"version", "--short"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	_ = version.Short()
}

func TestRootCmd_VersionJSON(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"version", "--json"})
	// printJSON writes to os.Stdout, so we just verify no error
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
}

func TestRootCmd_Help(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"--help"})
	out := bytes.NewBuffer(nil)
	root.SetOut(out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte("ff")) {
		t.Errorf("--help output should mention ff: %s", got)
	}
}

func TestRootCmd_HasDoctorCmd(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatal("root should have 'doctor' command")
	}
	if cmd.Short == "" {
		t.Error("doctor command should have a short description")
	}
}

func TestRootCmd_HasChangelogCmd(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"changelog"})
	if err != nil {
		t.Fatal("root should have 'changelog' command")
	}
	if cmd.Short == "" {
		t.Error("changelog command should have a short description")
	}
}

func TestRootCmd_HasCompletionsAlias(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"completions"})
	if err != nil {
		t.Fatal("root should have 'completions' command")
	}
	if !cmd.Hidden {
		t.Error("completions command should be hidden")
	}
}

func TestRootCmd_DoctorRuns(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"doctor", "--json"})
	// printJSON writes to os.Stdout; just verify no error
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor --json returned error: %v", err)
	}
}

func TestRootCmd_ChangelogRuns(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"changelog", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("changelog --json returned error: %v", err)
	}
}
