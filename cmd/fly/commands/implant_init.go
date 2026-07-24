package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	DefaultImplantTemplateDir = "implants/.fci/templates/implant"
)

func NewImplantInitCmd() *cobra.Command {
	var force bool
	var templateDir string

	cmd := &cobra.Command{
		Use:     "init [name]",
		Short:   "Scaffold a new implant project",
		Long:    "Create a new FCI implant project from the default template or a custom template directory.",
		Example: "  ff implant init my-implant\n  ff implant init --template-dir ./my-templates my-implant",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantInit(args[0], templateDir, force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files")
	cmd.Flags().StringVar(&templateDir, "template-dir", DefaultImplantTemplateDir, "Template directory to scaffold from")

	return cmd
}

func runImplantInit(name, templateDir string, force bool) error {
	if !isValidImplantName(name) {
		return fmt.Errorf("invalid implant name: %q\n   → Use lowercase letters, numbers, and hyphens only", name)
	}

	templatePath := filepath.Clean(templateDir)
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return fmt.Errorf("template directory does not exist: %s", templatePath)
	}

	projectDir := filepath.Join(".", name)
	if _, err := os.Stat(projectDir); err == nil && !force {
		return fmt.Errorf("directory %q already exists\n   → Use --force to overwrite", projectDir)
	}

	if err := os.MkdirAll(projectDir, 0750); err != nil {
		return fmt.Errorf("could not create directory: %w", err)
	}

	if err := copyTemplateDir(templatePath, projectDir, name, force); err != nil {
		return fmt.Errorf("failed to scaffold implant: %w", err)
	}

	if err := updateImplantManifest(filepath.Join(projectDir, "implant.yaml"), name); err != nil {
		fmt.Printf("Warning: could not update implant.yaml: %v\n", err)
	}

	fmt.Printf("\n✅ Created %s/\n\n", name)
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", name)
	fmt.Println("  ff implant build     # Compile TypeScript and create .fci artifact")
	fmt.Println("  ff implant sign      # Sign the manifest with your key")
	fmt.Println("  ff implant publish   # Publish to the registry")
	return nil
}

func isValidImplantName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

func copyTemplateDir(src, dst, name string, force bool) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			if err := os.MkdirAll(dstPath, 0750); err != nil {
				return fmt.Errorf("create directory %s: %w", dstPath, err)
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		content := string(data)
		content = strings.ReplaceAll(content, "{{IMPLANT_NAME}}", name)

		if err := os.WriteFile(dstPath, []byte(content), info.Mode()); err != nil {
			return fmt.Errorf("write file %s: %w", dstPath, err)
		}

		fmt.Printf("  ✓ %s\n", filepath.Join(name, relPath))
		return nil
	})
}

func updateImplantManifest(manifestPath, name string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	content := string(data)
	content = strings.ReplaceAll(content, "{{IMPLANT_NAME}}", name)
	content = strings.ReplaceAll(content, "{{IMPLANT_ID}}", strings.ReplaceAll(name, "-", "_"))

	return os.WriteFile(manifestPath, []byte(content), 0600)
}
