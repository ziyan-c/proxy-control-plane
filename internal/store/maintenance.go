package store

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type MaintenanceCleanupInput struct {
	AuditRetention        time.Duration
	AuditMaxBytes         int64
	TrafficRetention      time.Duration
	TrafficMaxBytes       int64
	TrafficDailyRetention time.Duration
	TrafficDailyMaxBytes  int64
	DryRun                bool
	Now                   time.Time
}

type MaintenanceCleanupResult struct {
	DryRun                   bool      `json:"dry_run"`
	AuditCutoff              time.Time `json:"audit_cutoff"`
	TrafficCutoff            time.Time `json:"traffic_cutoff"`
	TrafficDailyCutoff       time.Time `json:"traffic_daily_cutoff"`
	AuditRowsDeleted         int64     `json:"audit_rows_deleted"`
	AuditSizeBytes           int64     `json:"audit_size_bytes"`
	AuditMaxBytes            int64     `json:"audit_max_bytes"`
	TrafficRowsDeleted       int64     `json:"traffic_rows_deleted"`
	TrafficSizeBytes         int64     `json:"traffic_size_bytes"`
	TrafficMaxBytes          int64     `json:"traffic_max_bytes"`
	TrafficDailyRowsUpserted int64     `json:"traffic_daily_rows_upserted"`
	TrafficDailyRowsDeleted  int64     `json:"traffic_daily_rows_deleted"`
	TrafficDailySizeBytes    int64     `json:"traffic_daily_size_bytes"`
	TrafficDailyMaxBytes     int64     `json:"traffic_daily_max_bytes"`
}

func (s *Store) MaintenanceCleanup(ctx context.Context, input MaintenanceCleanupInput) (MaintenanceCleanupResult, error) {
	if input.AuditRetention <= 0 || input.TrafficRetention <= 0 || input.TrafficDailyRetention <= 0 {
		return MaintenanceCleanupResult{}, ErrInvalid
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	result := MaintenanceCleanupResult{
		DryRun:               input.DryRun,
		AuditCutoff:          now.Add(-input.AuditRetention),
		TrafficCutoff:        now.Add(-input.TrafficRetention),
		TrafficDailyCutoff:   now.Add(-input.TrafficDailyRetention),
		AuditMaxBytes:        input.AuditMaxBytes,
		TrafficMaxBytes:      input.TrafficMaxBytes,
		TrafficDailyMaxBytes: input.TrafficDailyMaxBytes,
	}

	if input.DryRun {
		return s.maintenanceCleanupDryRun(ctx, input, result)
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		trafficSize, err := tableSizePrunePlanTx(tx, "traffic_usage", input.TrafficMaxBytes)
		if err != nil {
			return err
		}
		result.TrafficSizeBytes = trafficSize.sizeBytes

		upsertResult := tx.Exec(aggregateTrafficUsageSQL, trafficSize.rowsToDelete, result.TrafficCutoff)
		if err := mapGormError(upsertResult.Error); err != nil {
			return err
		}
		result.TrafficDailyRowsUpserted = upsertResult.RowsAffected

		trafficDelete := tx.Exec(deleteTrafficUsageSQL, trafficSize.rowsToDelete, result.TrafficCutoff)
		if err := mapGormError(trafficDelete.Error); err != nil {
			return err
		}
		result.TrafficRowsDeleted = trafficDelete.RowsAffected

		auditSize, err := tableSizePrunePlanTx(tx, "audit_logs", input.AuditMaxBytes)
		if err != nil {
			return err
		}
		result.AuditSizeBytes = auditSize.sizeBytes

		auditDelete := tx.Exec(deleteAuditLogsSQL, auditSize.rowsToDelete, result.AuditCutoff)
		if err := mapGormError(auditDelete.Error); err != nil {
			return err
		}
		result.AuditRowsDeleted = auditDelete.RowsAffected

		dailySize, err := tableSizePrunePlanTx(tx, "traffic_usage_daily", input.TrafficDailyMaxBytes)
		if err != nil {
			return err
		}
		result.TrafficDailySizeBytes = dailySize.sizeBytes

		dailyDelete := tx.Exec(deleteTrafficUsageDailySQL, dailySize.rowsToDelete, result.TrafficDailyCutoff)
		if err := mapGormError(dailyDelete.Error); err != nil {
			return err
		}
		result.TrafficDailyRowsDeleted = dailyDelete.RowsAffected
		return nil
	})
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	return result, nil
}

func (s *Store) maintenanceCleanupDryRun(ctx context.Context, input MaintenanceCleanupInput, result MaintenanceCleanupResult) (MaintenanceCleanupResult, error) {
	tx := s.db.WithContext(ctx)

	trafficSize, err := tableSizePrunePlanTx(tx, "traffic_usage", input.TrafficMaxBytes)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficSizeBytes = trafficSize.sizeBytes
	trafficRows, err := countTrafficRowsForCleanupTx(tx, trafficSize.rowsToDelete, result.TrafficCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficRowsDeleted = trafficRows
	dailyRows, err := countTrafficDailyGroupsForCleanupTx(tx, trafficSize.rowsToDelete, result.TrafficCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficDailyRowsUpserted = dailyRows

	auditSize, err := tableSizePrunePlanTx(tx, "audit_logs", input.AuditMaxBytes)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.AuditSizeBytes = auditSize.sizeBytes
	auditRows, err := countAuditRowsForCleanupTx(tx, auditSize.rowsToDelete, result.AuditCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.AuditRowsDeleted = auditRows

	dailySize, err := tableSizePrunePlanTx(tx, "traffic_usage_daily", input.TrafficDailyMaxBytes)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficDailySizeBytes = dailySize.sizeBytes
	dailyDeleteRows, err := countTrafficDailyRowsForCleanupTx(tx, dailySize.rowsToDelete, result.TrafficDailyCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficDailyRowsDeleted = dailyDeleteRows
	return result, nil
}

type sizePrunePlan struct {
	sizeBytes    int64
	rowCount     int64
	rowsToDelete int64
}

func tableSizePrunePlanTx(tx *gorm.DB, table string, maxBytes int64) (sizePrunePlan, error) {
	var plan sizePrunePlan
	if maxBytes <= 0 {
		return plan, nil
	}
	sizeSQL, countSQL, ok := tableSizeQueries(table)
	if !ok {
		return sizePrunePlan{}, ErrInvalid
	}
	if err := tx.Raw(sizeSQL).Scan(&plan.sizeBytes).Error; err != nil {
		return sizePrunePlan{}, mapGormError(err)
	}
	if plan.sizeBytes <= maxBytes {
		return plan, nil
	}
	if err := tx.Raw(countSQL).Scan(&plan.rowCount).Error; err != nil {
		return sizePrunePlan{}, mapGormError(err)
	}
	if plan.rowCount == 0 {
		return plan, nil
	}
	bytesPerRow := (plan.sizeBytes + plan.rowCount - 1) / plan.rowCount
	if bytesPerRow <= 0 {
		return plan, nil
	}
	rowsToKeep := maxBytes / bytesPerRow
	if rowsToKeep >= plan.rowCount {
		return plan, nil
	}
	plan.rowsToDelete = plan.rowCount - rowsToKeep
	return plan, nil
}

func tableSizeQueries(table string) (string, string, bool) {
	switch table {
	case "audit_logs":
		return `SELECT pg_total_relation_size('audit_logs')`, `SELECT COUNT(*) FROM audit_logs`, true
	case "traffic_usage":
		return `SELECT pg_total_relation_size('traffic_usage')`, `SELECT COUNT(*) FROM traffic_usage`, true
	case "traffic_usage_daily":
		return `SELECT pg_total_relation_size('traffic_usage_daily')`, `SELECT COUNT(*) FROM traffic_usage_daily`, true
	default:
		return "", "", false
	}
}

func countTrafficRowsForCleanupTx(tx *gorm.DB, sizeRows int64, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
WITH size_doomed AS (
    SELECT id
    FROM traffic_usage
    ORDER BY recorded_at ASC, id ASC
    LIMIT ?
),
doomed AS (
    SELECT id FROM traffic_usage WHERE recorded_at < ?
    UNION
    SELECT id FROM size_doomed
)
SELECT COUNT(*) FROM doomed
`, sizeRows, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countTrafficDailyGroupsForCleanupTx(tx *gorm.DB, sizeRows int64, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
WITH size_doomed AS (
    SELECT id
    FROM traffic_usage
    ORDER BY recorded_at ASC, id ASC
    LIMIT ?
),
doomed AS (
    SELECT id, proxy_account_id, proxy_node_id, recorded_at
    FROM traffic_usage
    WHERE recorded_at < ?
    UNION
    SELECT traffic_usage.id, traffic_usage.proxy_account_id, traffic_usage.proxy_node_id, traffic_usage.recorded_at
    FROM traffic_usage
    JOIN size_doomed ON size_doomed.id = traffic_usage.id
)
SELECT COUNT(*)
FROM (
    SELECT 1
    FROM doomed
    GROUP BY proxy_account_id, proxy_node_id, recorded_at::date
) AS daily_groups
`, sizeRows, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countAuditRowsForCleanupTx(tx *gorm.DB, sizeRows int64, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
WITH size_doomed AS (
    SELECT id
    FROM audit_logs
    ORDER BY created_at ASC, id ASC
    LIMIT ?
),
doomed AS (
    SELECT id FROM audit_logs WHERE created_at < ?
    UNION
    SELECT id FROM size_doomed
)
SELECT COUNT(*) FROM doomed
`, sizeRows, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countTrafficDailyRowsForCleanupTx(tx *gorm.DB, sizeRows int64, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
WITH size_doomed AS (
    SELECT proxy_account_id, proxy_node_id, day
    FROM traffic_usage_daily
    ORDER BY day ASC, proxy_account_id ASC, proxy_node_id ASC
    LIMIT ?
),
doomed AS (
    SELECT proxy_account_id, proxy_node_id, day
    FROM traffic_usage_daily
    WHERE day < ?::date
    UNION
    SELECT proxy_account_id, proxy_node_id, day FROM size_doomed
)
SELECT COUNT(*) FROM doomed
`, sizeRows, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

const aggregateTrafficUsageSQL = `
WITH size_doomed AS (
    SELECT id
    FROM traffic_usage
    ORDER BY recorded_at ASC, id ASC
    LIMIT ?
),
doomed AS (
    SELECT id, proxy_account_id, proxy_node_id, upload_bytes, download_bytes, recorded_at
    FROM traffic_usage
    WHERE recorded_at < ?
    UNION
    SELECT traffic_usage.id, traffic_usage.proxy_account_id, traffic_usage.proxy_node_id, traffic_usage.upload_bytes, traffic_usage.download_bytes, traffic_usage.recorded_at
    FROM traffic_usage
    JOIN size_doomed ON size_doomed.id = traffic_usage.id
),
aggregated AS (
    SELECT
        proxy_account_id,
        proxy_node_id,
        recorded_at::date AS day,
        SUM(upload_bytes)::bigint AS upload_bytes,
        SUM(download_bytes)::bigint AS download_bytes
    FROM doomed
    GROUP BY proxy_account_id, proxy_node_id, recorded_at::date
)
INSERT INTO traffic_usage_daily (
    proxy_account_id,
    proxy_node_id,
    day,
    upload_bytes,
    download_bytes,
    created_at,
    updated_at
)
SELECT
    proxy_account_id,
    proxy_node_id,
    day,
    upload_bytes,
    download_bytes,
    now(),
    now()
FROM aggregated
ON CONFLICT (proxy_account_id, proxy_node_id, day) DO UPDATE
SET
    upload_bytes = traffic_usage_daily.upload_bytes + EXCLUDED.upload_bytes,
    download_bytes = traffic_usage_daily.download_bytes + EXCLUDED.download_bytes,
    updated_at = now()
`

const deleteTrafficUsageSQL = `
WITH size_doomed AS (
    SELECT id
    FROM traffic_usage
    ORDER BY recorded_at ASC, id ASC
    LIMIT ?
),
doomed AS (
    SELECT id FROM traffic_usage WHERE recorded_at < ?
    UNION
    SELECT id FROM size_doomed
)
DELETE FROM traffic_usage
WHERE id IN (SELECT id FROM doomed)
`

const deleteAuditLogsSQL = `
WITH size_doomed AS (
    SELECT id
    FROM audit_logs
    ORDER BY created_at ASC, id ASC
    LIMIT ?
),
doomed AS (
    SELECT id FROM audit_logs WHERE created_at < ?
    UNION
    SELECT id FROM size_doomed
)
DELETE FROM audit_logs
WHERE id IN (SELECT id FROM doomed)
`

const deleteTrafficUsageDailySQL = `
WITH size_doomed AS (
    SELECT proxy_account_id, proxy_node_id, day
    FROM traffic_usage_daily
    ORDER BY day ASC, proxy_account_id ASC, proxy_node_id ASC
    LIMIT ?
),
doomed AS (
    SELECT proxy_account_id, proxy_node_id, day
    FROM traffic_usage_daily
    WHERE day < ?::date
    UNION
    SELECT proxy_account_id, proxy_node_id, day FROM size_doomed
)
DELETE FROM traffic_usage_daily daily
USING doomed
WHERE daily.proxy_account_id = doomed.proxy_account_id
  AND daily.proxy_node_id = doomed.proxy_node_id
  AND daily.day = doomed.day
`
