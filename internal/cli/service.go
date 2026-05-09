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
	"github.com/ziyan-c/proxy-control-plane/internal/runtimesync"
	"github.com/ziyan-c/proxy-control-plane/internal/store"
	"github.com/ziyan-c/proxy-control-plane/internal/trafficsync"
)

type serviceOptions struct {
	envFile                          string
	listenAddr                       string
	databaseURL                      string
	autoCreateDatabase               string
	autoMigrate                      string
	runtimeSync                      string
	runtimeSyncInterval              string
	runtimeSyncTimeout               string
	runtimeSyncConcurrency           int
	trafficSync                      string
	trafficSyncInterval              string
	trafficSyncTimeout               string
	trafficSyncConcurrency           int
	maintenanceCleanup               string
	maintenanceCleanupInterval       string
	maintenanceAuditRetention        string
	maintenanceTrafficRetention      string
	maintenanceTrafficDailyRetention string
	maintenanceDomainAccessRetention string
	maintenanceAuthRefreshRetention  string
	migrationsDir                    string
	noLocalConfig                    bool
}

type serviceMode int

const (
	serviceModeServe serviceMode = iota
	serviceModeSQLMigrate
	serviceModeAutoMigrate
)

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
			return runService(cmd, rootOpts, opts, serviceModeServe)
		},
	}
	addServiceFlags(cmd, opts)
	cmd.Flags().StringVar(&opts.listenAddr, "listen", "", "API listen address, for example :9710")
	cmd.Flags().StringVar(&opts.autoMigrate, "auto-migrate", "", "true or false; run GORM AutoMigrate before serving")
	cmd.Flags().StringVar(&opts.runtimeSync, "runtime-sync", "", "true or false; enable Xray runtime user reconciliation")
	cmd.Flags().StringVar(&opts.runtimeSyncInterval, "runtime-sync-interval", "", "interval for runtime inspect and diff sync, for example 5m")
	cmd.Flags().StringVar(&opts.runtimeSyncTimeout, "runtime-sync-timeout", "", "timeout per runtime API call, for example 30s")
	cmd.Flags().IntVar(&opts.runtimeSyncConcurrency, "runtime-sync-concurrency", 0, "maximum runtime nodes to inspect in parallel")
	cmd.Flags().StringVar(&opts.trafficSync, "traffic-sync", "", "true or false; enable Xray StatsService traffic collection")
	cmd.Flags().StringVar(&opts.trafficSyncInterval, "traffic-sync-interval", "", "interval for Xray traffic stats collection, for example 5m")
	cmd.Flags().StringVar(&opts.trafficSyncTimeout, "traffic-sync-timeout", "", "timeout per Xray stats API call, for example 30s")
	cmd.Flags().IntVar(&opts.trafficSyncConcurrency, "traffic-sync-concurrency", 0, "maximum Xray nodes to collect traffic from in parallel")
	cmd.Flags().StringVar(&opts.maintenanceCleanup, "maintenance-cleanup", "", "true or false; enable periodic database maintenance cleanup")
	cmd.Flags().StringVar(&opts.maintenanceCleanupInterval, "maintenance-cleanup-interval", "", "interval for maintenance cleanup, for example 24h")
	cmd.Flags().StringVar(&opts.maintenanceAuditRetention, "maintenance-audit-retention", "", "delete audit logs older than this retention, for example 90d")
	cmd.Flags().StringVar(&opts.maintenanceTrafficRetention, "maintenance-traffic-retention", "", "aggregate and delete traffic_usage rows older than this retention, for example 7d")
	cmd.Flags().StringVar(&opts.maintenanceTrafficDailyRetention, "maintenance-traffic-daily-retention", "", "delete traffic_usage_daily rows older than this retention, for example 30d")
	cmd.Flags().StringVar(&opts.maintenanceDomainAccessRetention, "maintenance-domain-access-retention", "", "delete domain_access_logs rows older than this retention, for example 30d")
	cmd.Flags().StringVar(&opts.maintenanceAuthRefreshRetention, "maintenance-auth-refresh-retention", "", "delete auth_refresh_tokens after this retention from revoke/expiry time, for example 30d")
	return cmd
}

func newDBCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Manage database schema and data",
	}
	cmd.AddCommand(
		newDBMigrateCommand(rootOpts),
		newDBAutoMigrateCommand(rootOpts),
	)
	return cmd
}

func newDBMigrateCommand(rootOpts *Options) *cobra.Command {
	opts := &serviceOptions{}
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run versioned SQL database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, rootOpts, opts, serviceModeSQLMigrate)
		},
	}
	addServiceFlags(cmd, opts)
	cmd.Flags().StringVar(&opts.migrationsDir, "migrations-dir", "migrations", "directory containing versioned .sql migrations")
	return cmd
}

func newDBAutoMigrateCommand(rootOpts *Options) *cobra.Command {
	opts := &serviceOptions{}
	cmd := &cobra.Command{
		Use:   "automigrate",
		Short: "Run GORM AutoMigrate",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, rootOpts, opts, serviceModeAutoMigrate)
		},
	}
	addServiceFlags(cmd, opts)
	return cmd
}

func addServiceFlags(cmd *cobra.Command, opts *serviceOptions) {
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "explicit API env file; defaults to .local/app.env")
	cmd.Flags().StringVar(&opts.databaseURL, "database-url", "", "PostgreSQL connection URL")
	cmd.Flags().StringVar(&opts.autoCreateDatabase, "auto-create-database", "", "true or false; create missing target database before connecting")
	cmd.Flags().BoolVar(&opts.noLocalConfig, "no-local-config", false, "read configuration only from environment variables and direct flags")
}

func runService(cmd *cobra.Command, rootOpts *Options, opts *serviceOptions, mode serviceMode) error {
	if !opts.noLocalConfig || opts.envFile != "" {
		if !opts.noLocalConfig {
			if err := initLocal(rootOpts.ConfigDir, effectiveExampleDir(cmd, rootOpts)); err != nil {
				return err
			}
		}

		envFile := opts.envFile
		if envFile == "" {
			envFile = appEnvFile(rootOpts.ConfigDir)
		}
		if err := loadEnvFile(envFile, true); err != nil {
			return err
		}
	}

	cfg := config.Load()
	if err := applyServiceOptions(&cfg, opts); err != nil {
		return err
	}
	if mode == serviceModeServe {
		if err := cfg.ValidateServer(); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.AutoCreateDatabase)
	if err != nil {
		return err
	}
	defer st.Close()

	switch mode {
	case serviceModeSQLMigrate:
		results, err := st.ApplySQLMigrations(ctx, opts.migrationsDir)
		if err != nil {
			return err
		}
		printMigrationResults(results)
		return nil
	case serviceModeAutoMigrate:
		return st.AutoMigrate(ctx)
	case serviceModeServe:
		if cfg.AutoMigrate {
			if err := st.AutoMigrate(ctx); err != nil {
				return err
			}
		}
		return serve(ctx, cfg, st)
	default:
		return fmt.Errorf("unknown service mode %d", mode)
	}
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
	if opts.runtimeSync != "" {
		value, err := strconv.ParseBool(opts.runtimeSync)
		if err != nil {
			return fmt.Errorf("--runtime-sync must be true or false: %w", err)
		}
		cfg.RuntimeSyncEnabled = value
	}
	if opts.runtimeSyncInterval != "" {
		value, err := time.ParseDuration(opts.runtimeSyncInterval)
		if err != nil {
			return fmt.Errorf("--runtime-sync-interval must be a duration such as 5m: %w", err)
		}
		cfg.RuntimeSyncInterval = value
	}
	if opts.runtimeSyncTimeout != "" {
		value, err := time.ParseDuration(opts.runtimeSyncTimeout)
		if err != nil {
			return fmt.Errorf("--runtime-sync-timeout must be a duration such as 30s: %w", err)
		}
		cfg.RuntimeSyncTimeout = value
	}
	if opts.runtimeSyncConcurrency > 0 {
		cfg.RuntimeSyncConcurrency = opts.runtimeSyncConcurrency
	}
	if opts.trafficSync != "" {
		value, err := strconv.ParseBool(opts.trafficSync)
		if err != nil {
			return fmt.Errorf("--traffic-sync must be true or false: %w", err)
		}
		cfg.TrafficSyncEnabled = value
	}
	if opts.trafficSyncInterval != "" {
		value, err := time.ParseDuration(opts.trafficSyncInterval)
		if err != nil {
			return fmt.Errorf("--traffic-sync-interval must be a duration such as 5m: %w", err)
		}
		cfg.TrafficSyncInterval = value
	}
	if opts.trafficSyncTimeout != "" {
		value, err := time.ParseDuration(opts.trafficSyncTimeout)
		if err != nil {
			return fmt.Errorf("--traffic-sync-timeout must be a duration such as 30s: %w", err)
		}
		cfg.TrafficSyncTimeout = value
	}
	if opts.trafficSyncConcurrency > 0 {
		cfg.TrafficSyncConcurrency = opts.trafficSyncConcurrency
	}
	if opts.maintenanceCleanup != "" {
		value, err := strconv.ParseBool(opts.maintenanceCleanup)
		if err != nil {
			return fmt.Errorf("--maintenance-cleanup must be true or false: %w", err)
		}
		cfg.MaintenanceCleanupEnabled = value
	}
	if opts.maintenanceCleanupInterval != "" {
		value, err := time.ParseDuration(opts.maintenanceCleanupInterval)
		if err != nil {
			return fmt.Errorf("--maintenance-cleanup-interval must be a duration such as 24h: %w", err)
		}
		cfg.MaintenanceCleanupInterval = value
	}
	if opts.maintenanceAuditRetention != "" {
		cfg.MaintenanceAuditRetention = opts.maintenanceAuditRetention
	}
	if opts.maintenanceTrafficRetention != "" {
		cfg.MaintenanceTrafficRetention = opts.maintenanceTrafficRetention
	}
	if opts.maintenanceTrafficDailyRetention != "" {
		cfg.MaintenanceTrafficDailyRetention = opts.maintenanceTrafficDailyRetention
	}
	if opts.maintenanceDomainAccessRetention != "" {
		cfg.MaintenanceDomainAccessRetention = opts.maintenanceDomainAccessRetention
	}
	if opts.maintenanceAuthRefreshRetention != "" {
		cfg.MaintenanceAuthRefreshRetention = opts.maintenanceAuthRefreshRetention
	}
	return nil
}

func printMigrationResults(results []store.MigrationResult) {
	if len(results) == 0 {
		log.Println("no SQL migration files found")
		return
	}
	applied := 0
	for _, result := range results {
		if result.Applied {
			applied++
			log.Printf("applied SQL migration %s", result.Name)
		} else {
			log.Printf("skipped SQL migration %s", result.Name)
		}
	}
	log.Printf("SQL migrations complete: %d applied, %d skipped", applied, len(results)-applied)
}

func serve(ctx context.Context, cfg config.Config, st *store.Store) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if cfg.RuntimeSyncEnabled {
		syncer := runtimesync.New(st, runtimesync.XrayClient{Timeout: cfg.RuntimeSyncTimeout}, runtimesync.Options{
			Interval:    cfg.RuntimeSyncInterval,
			Timeout:     cfg.RuntimeSyncTimeout,
			Concurrency: cfg.RuntimeSyncConcurrency,
		})
		go syncer.Run(serveCtx)
	}
	if cfg.TrafficSyncEnabled {
		syncer := trafficsync.New(st, runtimesync.XrayClient{Timeout: cfg.TrafficSyncTimeout}, trafficsync.Options{
			Interval:    cfg.TrafficSyncInterval,
			Timeout:     cfg.TrafficSyncTimeout,
			Concurrency: cfg.TrafficSyncConcurrency,
			Reset:       true,
		})
		go syncer.Run(serveCtx)
	}
	if cfg.MaintenanceCleanupEnabled {
		interval, input, err := maintenanceCleanupInputFromConfig(cfg)
		if err != nil {
			return err
		}
		go runMaintenanceCleanupLoop(serveCtx, st, interval, input)
	}

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

func maintenanceCleanupInputFromConfig(cfg config.Config) (time.Duration, store.MaintenanceCleanupInput, error) {
	auditRetention, err := parseRetentionDuration(cfg.MaintenanceAuditRetention)
	if err != nil {
		return 0, store.MaintenanceCleanupInput{}, fmt.Errorf("PCP_MAINTENANCE_AUDIT_RETENTION: %w", err)
	}
	trafficRetention, err := parseRetentionDuration(cfg.MaintenanceTrafficRetention)
	if err != nil {
		return 0, store.MaintenanceCleanupInput{}, fmt.Errorf("PCP_MAINTENANCE_TRAFFIC_RETENTION: %w", err)
	}
	trafficDailyRetention, err := parseRetentionDuration(cfg.MaintenanceTrafficDailyRetention)
	if err != nil {
		return 0, store.MaintenanceCleanupInput{}, fmt.Errorf("PCP_MAINTENANCE_TRAFFIC_DAILY_RETENTION: %w", err)
	}
	domainAccessRetention, err := parseRetentionDuration(cfg.MaintenanceDomainAccessRetention)
	if err != nil {
		return 0, store.MaintenanceCleanupInput{}, fmt.Errorf("PCP_MAINTENANCE_DOMAIN_ACCESS_RETENTION: %w", err)
	}
	authRefreshRetention, err := parseRetentionDuration(cfg.MaintenanceAuthRefreshRetention)
	if err != nil {
		return 0, store.MaintenanceCleanupInput{}, fmt.Errorf("PCP_MAINTENANCE_AUTH_REFRESH_RETENTION: %w", err)
	}
	return cfg.MaintenanceCleanupInterval, store.MaintenanceCleanupInput{
		AuditRetention:        auditRetention,
		TrafficRetention:      trafficRetention,
		TrafficDailyRetention: trafficDailyRetention,
		DomainAccessRetention: domainAccessRetention,
		AuthRefreshRetention:  authRefreshRetention,
	}, nil
}

func runMaintenanceCleanupLoop(ctx context.Context, st *store.Store, interval time.Duration, input store.MaintenanceCleanupInput) {
	log.Printf("maintenance cleanup enabled: interval=%s traffic_retention=%s traffic_daily_retention=%s domain_access_retention=%s auth_refresh_retention=%s audit_retention=%s",
		interval,
		input.TrafficRetention,
		input.TrafficDailyRetention,
		input.DomainAccessRetention,
		input.AuthRefreshRetention,
		input.AuditRetention,
	)
	runMaintenanceCleanupOnce(ctx, st, input)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("maintenance cleanup stopped")
			return
		case <-ticker.C:
			runMaintenanceCleanupOnce(ctx, st, input)
		}
	}
}

func runMaintenanceCleanupOnce(ctx context.Context, st *store.Store, input store.MaintenanceCleanupInput) {
	result, err := st.MaintenanceCleanup(ctx, input)
	if err != nil {
		log.Printf("maintenance cleanup failed: %v", err)
		return
	}
	log.Printf("maintenance cleanup complete: traffic_rows=%d traffic_daily_rows=%d traffic_daily_deleted=%d domain_access_rows=%d auth_refresh_rows=%d audit_rows=%d",
		result.TrafficRowsDeleted,
		result.TrafficDailyRowsUpserted,
		result.TrafficDailyRowsDeleted,
		result.DomainAccessRowsDeleted,
		result.AuthRefreshRowsDeleted,
		result.AuditRowsDeleted,
	)
}
