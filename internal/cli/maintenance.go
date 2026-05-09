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
	trafficRetention      string
	trafficDailyRetention string
	domainAccessRetention string
	authRefreshRetention  string
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
		auditRetention:        "90d",
		trafficRetention:      "7d",
		trafficDailyRetention: "30d",
		domainAccessRetention: "30d",
		authRefreshRetention:  "30d",
	}

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Aggregate old traffic details and delete expired detail/audit rows",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMaintenanceCleanup(cmd, rootOpts, serviceOpts, cleanupOpts)
		},
	}
	addServiceFlags(cmd, serviceOpts)
	cmd.Flags().StringVar(&cleanupOpts.auditRetention, "audit-retention", cleanupOpts.auditRetention, "delete audit logs older than this retention, for example 90d")
	cmd.Flags().StringVar(&cleanupOpts.trafficRetention, "traffic-retention", cleanupOpts.trafficRetention, "aggregate and delete traffic_usage rows older than this retention, for example 7d")
	cmd.Flags().StringVar(&cleanupOpts.trafficDailyRetention, "traffic-daily-retention", cleanupOpts.trafficDailyRetention, "delete traffic_usage_daily rows older than this retention, for example 30d")
	cmd.Flags().StringVar(&cleanupOpts.domainAccessRetention, "domain-access-retention", cleanupOpts.domainAccessRetention, "delete domain_access_logs rows older than this retention, for example 30d")
	cmd.Flags().StringVar(&cleanupOpts.authRefreshRetention, "auth-refresh-retention", cleanupOpts.authRefreshRetention, "delete auth_refresh_tokens after this retention from revoke/expiry time, for example 30d")
	cmd.Flags().BoolVar(&cleanupOpts.dryRun, "dry-run", cleanupOpts.dryRun, "show what would be changed without writing PostgreSQL")
	return cmd
}

func runMaintenanceCleanup(cmd *cobra.Command, rootOpts *Options, serviceOpts *serviceOptions, cleanupOpts *maintenanceCleanupOptions) error {
	auditRetention, err := parseRetentionDuration(cleanupOpts.auditRetention)
	if err != nil {
		return fmt.Errorf("--audit-retention: %w", err)
	}
	trafficRetention, err := parseRetentionDuration(cleanupOpts.trafficRetention)
	if err != nil {
		return fmt.Errorf("--traffic-retention: %w", err)
	}
	trafficDailyRetention, err := parseRetentionDuration(cleanupOpts.trafficDailyRetention)
	if err != nil {
		return fmt.Errorf("--traffic-daily-retention: %w", err)
	}
	domainAccessRetention, err := parseRetentionDuration(cleanupOpts.domainAccessRetention)
	if err != nil {
		return fmt.Errorf("--domain-access-retention: %w", err)
	}
	authRefreshRetention, err := parseRetentionDuration(cleanupOpts.authRefreshRetention)
	if err != nil {
		return fmt.Errorf("--auth-refresh-retention: %w", err)
	}

	ctx := cmd.Context()
	st, err := openStoreForCLI(ctx, cmd, rootOpts, serviceOpts)
	if err != nil {
		return err
	}
	defer st.Close()

	result, err := st.MaintenanceCleanup(ctx, store.MaintenanceCleanupInput{
		AuditRetention:        auditRetention,
		TrafficRetention:      trafficRetention,
		TrafficDailyRetention: trafficDailyRetention,
		DomainAccessRetention: domainAccessRetention,
		AuthRefreshRetention:  authRefreshRetention,
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
	fmt.Fprintf(out, "audit_cutoff=%s audit_rows=%d\n", result.AuditCutoff.Format(time.RFC3339), result.AuditRowsDeleted)
	fmt.Fprintf(out, "traffic_cutoff=%s traffic_rows=%d traffic_daily_rows=%d\n", result.TrafficCutoff.Format(time.RFC3339), result.TrafficRowsDeleted, result.TrafficDailyRowsUpserted)
	fmt.Fprintf(out, "traffic_daily_cutoff=%s traffic_daily_rows_deleted=%d\n", result.TrafficDailyCutoff.Format(time.RFC3339), result.TrafficDailyRowsDeleted)
	fmt.Fprintf(out, "domain_access_cutoff=%s domain_access_rows=%d\n", result.DomainAccessCutoff.Format(time.RFC3339), result.DomainAccessRowsDeleted)
	fmt.Fprintf(out, "auth_refresh_cutoff=%s auth_refresh_rows=%d\n", result.AuthRefreshCutoff.Format(time.RFC3339), result.AuthRefreshRowsDeleted)
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
