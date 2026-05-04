package cli

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

type dockerOptions struct {
	build  bool
	detach bool
}

func newDockerCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Manage Docker Compose services",
	}
	cmd.AddCommand(newDockerUpCommand(rootOpts))
	return cmd
}

func newDockerUpCommand(rootOpts *Options) *cobra.Command {
	opts := &dockerOptions{
		build: true,
	}
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the Docker Compose stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDockerUp(cmd, rootOpts, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.build, "build", true, "build images before starting containers")
	cmd.Flags().BoolVarP(&opts.detach, "detach", "d", false, "start containers in the background")
	return cmd
}

func runDockerUp(cmd *cobra.Command, rootOpts *Options, opts *dockerOptions) error {
	if err := initLocal(rootOpts.ConfigDir, effectiveExampleDir(cmd, rootOpts)); err != nil {
		return err
	}

	args := []string{"compose", "-f", "docker-compose.yml", "up"}
	if opts.build {
		args = append(args, "--build")
	}
	if opts.detach {
		args = append(args, "-d")
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dockerCmd := exec.CommandContext(ctx, "docker", args...)
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	dockerCmd.Env = append(os.Environ(),
		"PCP_APP_ENV_FILE="+appEnvFile(rootOpts.ConfigDir),
	)
	return dockerCmd.Run()
}
