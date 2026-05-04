package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newConfigCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage local configuration",
	}
	cmd.AddCommand(newConfigInitCommand(opts))
	return cmd
}

func newConfigInitCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create missing private local config files from examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			return initLocal(opts.ConfigDir, effectiveExampleDir(cmd, opts))
		},
	}
}

func initLocal(configDir string, exampleDir string) error {
	files := map[string]string{
		"app.env": "app.env",
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	for target, template := range files {
		if err := copyTemplateIfMissing(filepath.Join(exampleDir, template), filepath.Join(configDir, target)); err != nil {
			return err
		}
	}
	return nil
}

func defaultExampleDir(configDir string) string {
	if configDir == ".local" {
		return ".local.example"
	}
	return configDir + ".example"
}

func copyTemplateIfMissing(templatePath string, targetPath string) error {
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	content, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", templatePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(targetPath, content, 0o600); err != nil {
		return fmt.Errorf("create %s: %w", targetPath, err)
	}
	fmt.Fprintf(os.Stderr, "created %s\n", targetPath)
	return nil
}
