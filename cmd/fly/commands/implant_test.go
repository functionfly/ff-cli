package commands

import (
	"testing"
)

func TestNewImplantCmd(t *testing.T) {
	cmd := NewImplantCmd()

	if cmd.Name() != "implant" {
		t.Errorf("Name = %q, want implant", cmd.Name())
	}
	if cmd.Short == "" {
		t.Error("implant should have a short description")
	}
	if cmd.Long == "" {
		t.Error("implant should have a long description")
	}

	expectedAliases := []string{"fci"}
	for _, alias := range expectedAliases {
		found := false
		for _, a := range cmd.Aliases {
			if a == alias {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Aliases should include %q", alias)
		}
	}

	subcommands := []string{"init", "build", "sign", "publish", "validate", "list", "diff"}
	for _, sub := range subcommands {
		_, _, err := cmd.Find([]string{sub})
		if err != nil {
			t.Errorf("implant should have subcommand %q", sub)
		}
	}
}

func TestImplantCmd_Exists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"implant"})
	if err != nil {
		t.Fatal("root should have 'implant' command")
	}
	if cmd.Name() != "implant" {
		t.Errorf("Name = %q, want implant", cmd.Name())
	}
}

func TestNewImplantValidateCmd(t *testing.T) {
	cmd := NewImplantValidateCmd()

	if cmd.Name() != "validate" {
		t.Errorf("Name = %q, want validate", cmd.Name())
	}
	if cmd.Short == "" {
		t.Error("validate should have a short description")
	}
	if cmd.Args != nil {
		if err := cmd.Args(nil, []string{"test.fci"}); err != nil {
			t.Errorf("validate should accept exactly 0 or 1 args, got error: %v", err)
		}
	}
}

func TestNewImplantListCmd(t *testing.T) {
	cmd := NewImplantListCmd()

	if cmd.Name() != "list" {
		t.Errorf("Name = %q, want list", cmd.Name())
	}
	if cmd.Short == "" {
		t.Error("list should have a short description")
	}
	if cmd.Args != nil {
		if err := cmd.Args(nil, []string{"./dist"}); err != nil {
			t.Errorf("list should accept 0 or 1 args, got error: %v", err)
		}
	}
}

func TestNewImplantDiffCmd(t *testing.T) {
	cmd := NewImplantDiffCmd()

	if cmd.Name() != "diff" {
		t.Errorf("Name = %q, want diff", cmd.Name())
	}
	if cmd.Short == "" {
		t.Error("diff should have a short description")
	}
	if cmd.Args == nil {
		t.Error("diff should have args validation")
	}
}

func TestImplantValidateCmd_HasStrictFlag(t *testing.T) {
	cmd := NewImplantValidateCmd()

	if !cmd.Flags().Lookup("strict").Changed {
		cmd.Flags().Set("strict", "false")
	}
	if cmd.Flags().Lookup("strict") == nil {
		t.Error("validate should have --strict flag")
	}
}

func TestImplantListCmd_HasLocalAndFormatFlags(t *testing.T) {
	cmd := NewImplantListCmd()

	if cmd.Flags().Lookup("local") == nil {
		t.Error("list should have --local flag")
	}
	if cmd.Flags().Lookup("format") == nil {
		t.Error("list should have --format flag")
	}
}

func TestImplantDiffCmd_HasFormatFlag(t *testing.T) {
	cmd := NewImplantDiffCmd()

	if cmd.Flags().Lookup("format") == nil {
		t.Error("diff should have --format flag")
	}
	if cmd.Flags().Lookup("published") == nil {
		t.Error("diff should have --published flag")
	}
}

func TestImplantCmd_FCICAlias(t *testing.T) {
	cmd := NewImplantCmd()

	found := false
	for _, a := range cmd.Aliases {
		if a == "fci" {
			found = true
			break
		}
	}
	if !found {
		t.Error("implant should have 'fci' alias")
	}
}
