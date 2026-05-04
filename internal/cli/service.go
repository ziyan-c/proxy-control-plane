package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/ziyan-c/proxy-control-plane/internal/config"
	"github.com/ziyan-c/proxy-control-plane/internal/httpapi"
	"github.com/ziyan-c/proxy-control-plane/internal/store"
)

type serviceOptions struct {
	dbProfile          string
	envFile            string
	listenAddr         string
	databaseURL        string
	autoCreateDatabase string
	autoMigrate        string
	noLocalConfig      bool
}

func newServerCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the API server",
	}
	cmd.AddCommand(newServerServeCommand(rootOpts))
	return cmd
}

func newServerServeCommand(rootOpts *Options) *cobra.Command {
	opts := &serviceOptions{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, rootOpts, opts, true)
		},
	}
	addServiceFlags(cmd, opts)
	cmd.Flags().StringVar(&opts.listenAddr, "listen", "", "API listen address, for example :9710")
	cmd.Flags().StringVar(&opts.autoMigrate, "auto-migrate", "", "true or false; run GORM table migrations before serving")
	return cmd
}

func newDBCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Manage database schema and data",
	}
	cmd.AddCommand(newDBMigrateCommand(rootOpts))
	return cmd
}

func newDBMigrateCommand(rootOpts *Options) *cobra.Command {
	opts := &serviceOptions{}
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, rootOpts, opts, false)
		},
	}
	addServiceFlags(cmd, opts)
	return cmd
}

func addServiceFlags(cmd *cobra.Command, opts *serviceOptions) {
	cmd.Flags().StringVar(&opts.dbProfile, "db", "", "database profile: local or remote; defaults from cli.env")
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "explicit API env file; overrides --db profile selection")
	cmd.Flags().StringVar(&opts.databaseURL, "database-url", "", "PostgreSQL connection URL")
	cmd.Flags().StringVar(&opts.autoCreateDatabase, "auto-create-database", "", "true or false; create missing target database before connecting")
	cmd.Flags().BoolVar(&opts.noLocalConfig, "no-local-config", false, "read configuration only from environment variables and direct flags")
}

func runService(cmd *cobra.Command, rootOpts *Options, opts *serviceOptions, serveMode bool) error {
	if !opts.noLocalConfig || opts.envFile != "" {
		if !opts.noLocalConfig {
			if err := initLocal(rootOpts.ConfigDir, effectiveExampleDir(cmd, rootOpts)); err != nil {
				return err
			}
		}

		envFile := opts.envFile
		if envFile == "" {
			dbProfile, err := resolveDBProfile(opts.dbProfile, rootOpts.ConfigDir)
			if err != nil {
				return err
			}
			envFile, err = apiEnvFile(rootOpts.ConfigDir, dbProfile)
			if err != nil {
				return err
			}
		}
		if err := loadEnvFile(envFile, true); err != nil {
			return err
		}
	}

	cfg := config.Load()
	if err := applyServiceOptions(&cfg, opts); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.AutoCreateDatabase)
	if err != nil {
		return err
	}
	defer st.Close()

	if !serveMode {
		return st.Migrate(ctx)
	}
	if cfg.AutoMigrate {
		if err := st.Migrate(ctx); err != nil {
			return err
		}
	}
	return serve(ctx, cfg, st)
}

func applyServiceOptions(cfg *config.Config, opts *serviceOptions) error {
	if opts.listenAddr != "" {
		cfg.ListenAddr = opts.listenAddr
	}
	if opts.databaseURL != "" {
		cfg.DatabaseURL = opts.databaseURL
	}
	if opts.autoCreateDatabase != "" {
		value, err := strconv.ParseBool(opts.autoCreateDatabase)
		if err != nil {
			return fmt.Errorf("--auto-create-database must be true or false: %w", err)
		}
		cfg.AutoCreateDatabase = value
	}
	if opts.autoMigrate != "" {
		value, err := strconv.ParseBool(opts.autoMigrate)
		if err != nil {
			return fmt.Errorf("--auto-migrate must be true or false: %w", err)
		}
		cfg.AutoMigrate = value
	}
	return nil
}

func serve(ctx context.Context, cfg config.Config, st *store.Store) error {
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.New(cfg, st),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("%s listening on %s", cfg.AppName, cfg.ListenAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
