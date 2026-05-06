package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ziyan-c/proxy-control-plane/internal/store"
)

type maintenanceCleanupOptions struct {
	auditRetention        string
	auditMaxSize          string
	trafficRetention      string
	trafficMaxSize        string
	trafficDailyRetention string
	trafficDailyMaxSize   string
	dryRun                bool
}

func newMaintenanceCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintenance",
		Short: "Run operational maintenance tasks",
	}
	cmd.AddCommand(newMaintenanceCleanupCommand(rootOpts))
	return cmd
}

func newMaintenanceCleanupCommand(rootOpts *Options) *cobra.Command {
	serviceOpts := &serviceOptions{}
	cleanupOpts := &maintenanceCleanupOptions{
		auditRetention:        "180d",
		auditMaxSize:          "1GB",
		trafficRetention:      "7d",
		trafficMaxSize:        "1GB",
		trafficDailyRetention: "180d",
		trafficDailyMaxSize:   "2GB",
		dryRun:                true,
	}

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Aggregate old traffic details and delete expired detail/audit rows",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMaintenanceCleanup(cmd, rootOpts, serviceOpts, cleanupOpts)
		},
	}
	addServiceFlags(cmd, serviceOpts)
	cmd.Flags().StringVar(&cleanupOpts.auditRetention, "audit-retention", cleanupOpts.auditRetention, "delete audit logs older than this retention, for example 180d")
	cmd.Flags().StringVar(&cleanupOpts.auditMaxSize, "audit-max-size", cleanupOpts.auditMaxSize, "soft storage cap for audit_logs, for example 1GB; use 0 to disable")
	cmd.Flags().StringVar(&cleanupOpts.trafficRetention, "traffic-retention", cleanupOpts.trafficRetention, "aggregate and delete traffic_usage rows older than this retention, for example 7d")
	cmd.Flags().StringVar(&cleanupOpts.trafficMaxSize, "traffic-max-size", cleanupOpts.trafficMaxSize, "soft storage cap for traffic_usage details, for example 1GB; use 0 to disable")
	cmd.Flags().StringVar(&cleanupOpts.trafficDailyRetention, "traffic-daily-retention", cleanupOpts.trafficDailyRetention, "delete traffic_usage_daily rows older than this retention, for example 180d")
	cmd.Flags().StringVar(&cleanupOpts.trafficDailyMaxSize, "traffic-daily-max-size", cleanupOpts.trafficDailyMaxSize, "soft storage cap for traffic_usage_daily, for example 2GB; use 0 to disable")
	cmd.Flags().BoolVar(&cleanupOpts.dryRun, "dry-run", cleanupOpts.dryRun, "show what would be changed without writing PostgreSQL")
	return cmd
}

func runMaintenanceCleanup(cmd *cobra.Command, rootOpts *Options, serviceOpts *serviceOptions, cleanupOpts *maintenanceCleanupOptions) error {
	auditRetention, err := parseRetentionDuration(cleanupOpts.auditRetention)
	if err != nil {
		return fmt.Errorf("--audit-retention: %w", err)
	}
	auditMaxBytes, err := parseSizeBytes(cleanupOpts.auditMaxSize)
	if err != nil {
		return fmt.Errorf("--audit-max-size: %w", err)
	}
	trafficRetention, err := parseRetentionDuration(cleanupOpts.trafficRetention)
	if err != nil {
		return fmt.Errorf("--traffic-retention: %w", err)
	}
	trafficMaxBytes, err := parseSizeBytes(cleanupOpts.trafficMaxSize)
	if err != nil {
		return fmt.Errorf("--traffic-max-size: %w", err)
	}
	trafficDailyRetention, err := parseRetentionDuration(cleanupOpts.trafficDailyRetention)
	if err != nil {
		return fmt.Errorf("--traffic-daily-retention: %w", err)
	}
	trafficDailyMaxBytes, err := parseSizeBytes(cleanupOpts.trafficDailyMaxSize)
	if err != nil {
		return fmt.Errorf("--traffic-daily-max-size: %w", err)
	}

	ctx := cmd.Context()
	st, err := openStoreForCLI(ctx, cmd, rootOpts, serviceOpts)
	if err != nil {
		return err
	}
	defer st.Close()

	result, err := st.MaintenanceCleanup(ctx, store.MaintenanceCleanupInput{
		AuditRetention:        auditRetention,
		AuditMaxBytes:         auditMaxBytes,
		TrafficRetention:      trafficRetention,
		TrafficMaxBytes:       trafficMaxBytes,
		TrafficDailyRetention: trafficDailyRetention,
		TrafficDailyMaxBytes:  trafficDailyMaxBytes,
		DryRun:                cleanupOpts.dryRun,
	})
	if err != nil {
		return err
	}

	status := "complete"
	if result.DryRun {
		status = "dry run complete"
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "maintenance cleanup %s\n", status)
	fmt.Fprintf(out, "audit_cutoff=%s audit_rows=%d audit_size_bytes=%d audit_max_bytes=%d\n", result.AuditCutoff.Format(time.RFC3339), result.AuditRowsDeleted, result.AuditSizeBytes, result.AuditMaxBytes)
	fmt.Fprintf(out, "traffic_cutoff=%s traffic_rows=%d traffic_size_bytes=%d traffic_max_bytes=%d traffic_daily_rows=%d\n", result.TrafficCutoff.Format(time.RFC3339), result.TrafficRowsDeleted, result.TrafficSizeBytes, result.TrafficMaxBytes, result.TrafficDailyRowsUpserted)
	fmt.Fprintf(out, "traffic_daily_cutoff=%s traffic_daily_rows_deleted=%d traffic_daily_size_bytes=%d traffic_daily_max_bytes=%d\n", result.TrafficDailyCutoff.Format(time.RFC3339), result.TrafficDailyRowsDeleted, result.TrafficDailySizeBytes, result.TrafficDailyMaxBytes)
	return nil
}

func parseRetentionDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("value is required")
	}
	if strings.HasSuffix(value, "d") {
		daysText := strings.TrimSpace(strings.TrimSuffix(value, "d"))
		days, err := strconv.ParseInt(daysText, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day retention %q", value)
		}
		if days <= 0 {
			return 0, fmt.Errorf("retention must be greater than zero")
		}
		const day = 24 * time.Hour
		const maxDurationDays = int64(1<<63-1) / int64(day)
		if days > maxDurationDays {
			return 0, fmt.Errorf("retention is too large")
		}
		return time.Duration(days) * day, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q; use values like 90d, 720h, or 30m", value)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("retention must be greater than zero")
	}
	return duration, nil
}

func parseSizeBytes(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("value is required")
	}
	if value == "0" {
		return 0, nil
	}

	upper := strings.ToUpper(value)
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"KIB", 1024},
		{"MIB", 1024 * 1024},
		{"GIB", 1024 * 1024 * 1024},
		{"TIB", 1024 * 1024 * 1024 * 1024},
		{"KB", 1024},
		{"MB", 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"TB", 1024 * 1024 * 1024 * 1024},
		{"B", 1},
	}
	for _, unit := range units {
		if !strings.HasSuffix(upper, unit.suffix) {
			continue
		}
		numberText := strings.TrimSpace(value[:len(value)-len(unit.suffix)])
		if numberText == "" {
			return 0, fmt.Errorf("invalid size %q", value)
		}
		number, err := strconv.ParseInt(numberText, 10, 64)
		if err != nil || number < 0 {
			return 0, fmt.Errorf("invalid size %q", value)
		}
		if number > (1<<63-1)/unit.multiplier {
			return 0, fmt.Errorf("size is too large")
		}
		return number * unit.multiplier, nil
	}

	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("invalid size %q; use values like 2GB, 512MB, or 0", value)
	}
	return number, nil
}
