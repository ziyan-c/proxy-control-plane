package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

type dockerOptions struct {
	dbProfile string
	build     bool
	detach    bool
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
	cmd.Flags().StringVar(&opts.dbProfile, "db", "", "database profile: local or remote; defaults from cli.env")
	cmd.Flags().BoolVar(&opts.build, "build", true, "build images before starting containers")
	cmd.Flags().BoolVarP(&opts.detach, "detach", "d", false, "start containers in the background")
	return cmd
}

func runDockerUp(cmd *cobra.Command, rootOpts *Options, opts *dockerOptions) error {
	if err := initLocal(rootOpts.ConfigDir, effectiveExampleDir(cmd, rootOpts)); err != nil {
		return err
	}

	dbProfile, err := resolveDBProfile(opts.dbProfile, rootOpts.ConfigDir)
	if err != nil {
		return err
	}
	composeFile, err := composeFileForDB(dbProfile)
	if err != nil {
		return err
	}
	apiEnvFile, err := dockerAPIEnvFile(rootOpts.ConfigDir, dbProfile)
	if err != nil {
		return err
	}

	args := []string{"compose", "-f", composeFile, "up"}
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
		"PCP_API_ENV_FILE="+apiEnvFile,
		"PCP_POSTGRES_ENV_FILE="+filepath.Join(rootOpts.ConfigDir, "postgres.env"),
	)
	return dockerCmd.Run()
}

func composeFileForDB(dbProfile string) (string, error) {
	switch dbProfile {
	case "local":
		return "docker-compose.yml", nil
	case "remote":
		return "docker-compose.remote-db.yml", nil
	default:
		return "", errors.New(`--db must be "local" or "remote"`)
	}
}
