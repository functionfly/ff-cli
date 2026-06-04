package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestNewCompletionsAliasCmd(t *testing.T) {
	root := NewRootCmd()
	cmd := NewCompletionsAliasCmd(root)

	if cmd.Name() != "completions" {
		t.Errorf("Name = %q, want completions", cmd.Name())
	}
	if !cmd.Hidden {
		t.Error("completions alias should be hidden")
	}
	if cmd.Short == "" {
		t.Error("completions alias should have a short description")
	}
}

func TestCompletionCmd_Exists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"completion"})
	if err != nil {
		t.Fatal("root should have 'completion' command")
	}
	if cmd.Name() != "completion" {
		t.Errorf("Name = %q, want completion", cmd.Name())
	}
}

func TestCompletionCmd_ValidArgs(t *testing.T) {
	root := NewRootCmd()
	cmd, _, _ := root.Find([]string{"completion"})

	valid := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range valid {
		found := false
		for _, a := range cmd.ValidArgs {
			if a == shell {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ValidArgs should include %q", shell)
		}
	}
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	// Use a minimal root to avoid -o flag collision with compile subcommands
	root := &cobra.Command{Use: "ff", SilenceErrors: true}
	completion := NewCompletionCmd(root)
	root.AddCommand(completion)

	root.SetArgs([]string{"completion", "invalid"})
	err := root.Execute()
	if err == nil {
		t.Error("completion with invalid shell should return error")
	}
}
