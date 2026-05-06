package cli

import (
	"github.com/spf13/cobra"
)

type Options struct {
	ConfigDir  string
	ExampleDir string
}

func Execute() error {
	return NewRootCommand().Execute()
}

func NewRootCommand() *cobra.Command {
	opts := &Options{
		ConfigDir:  ".local",
		ExampleDir: ".local.example",
	}

	cmd := &cobra.Command{
		Use:           "proxy-control-plane",
		Short:         "Proxy service control-plane CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVar(&opts.ConfigDir, "config-dir", opts.ConfigDir, "local configuration directory")
	cmd.PersistentFlags().StringVar(&opts.ExampleDir, "example-dir", opts.ExampleDir, "example configuration directory")

	cmd.AddCommand(
		newConfigCommand(opts),
		newServerCommand(opts),
		newDBCommand(opts),
		newImportCommand(opts),
		newDockerCommand(opts),
	)

	return cmd
}

func effectiveExampleDir(cmd *cobra.Command, opts *Options) string {
	if flagChanged(cmd, "example-dir") {
		return opts.ExampleDir
	}
	return defaultExampleDir(opts.ConfigDir)
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return flag.Changed
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
		return flag.Changed
	}
	if flag := cmd.Root().PersistentFlags().Lookup(name); flag != nil {
		return flag.Changed
	}
	return false
}
