package store

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type MaintenanceCleanupInput struct {
	AuditRetention        time.Duration
	TrafficRetention      time.Duration
	TrafficDailyRetention time.Duration
	DomainAccessRetention time.Duration
	AuthRefreshRetention  time.Duration
	DryRun                bool
	Now                   time.Time
}

type MaintenanceCleanupResult struct {
	DryRun                   bool      `json:"dry_run"`
	AuditCutoff              time.Time `json:"audit_cutoff"`
	TrafficCutoff            time.Time `json:"traffic_cutoff"`
	TrafficDailyCutoff       time.Time `json:"traffic_daily_cutoff"`
	DomainAccessCutoff       time.Time `json:"domain_access_cutoff"`
	AuthRefreshCutoff        time.Time `json:"auth_refresh_cutoff"`
	AuditRowsDeleted         int64     `json:"audit_rows_deleted"`
	TrafficRowsDeleted       int64     `json:"traffic_rows_deleted"`
	TrafficDailyRowsUpserted int64     `json:"traffic_daily_rows_upserted"`
	TrafficDailyRowsDeleted  int64     `json:"traffic_daily_rows_deleted"`
	DomainAccessRowsDeleted  int64     `json:"domain_access_rows_deleted"`
	AuthRefreshRowsDeleted   int64     `json:"auth_refresh_rows_deleted"`
}

func (s *Store) MaintenanceCleanup(ctx context.Context, input MaintenanceCleanupInput) (MaintenanceCleanupResult, error) {
	if input.AuditRetention <= 0 || input.TrafficRetention <= 0 || input.TrafficDailyRetention <= 0 || input.DomainAccessRetention <= 0 || input.AuthRefreshRetention <= 0 {
		return MaintenanceCleanupResult{}, ErrInvalid
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	result := MaintenanceCleanupResult{
		DryRun:             input.DryRun,
		AuditCutoff:        now.Add(-input.AuditRetention),
		TrafficCutoff:      now.Add(-input.TrafficRetention),
		TrafficDailyCutoff: now.Add(-input.TrafficDailyRetention),
		DomainAccessCutoff: now.Add(-input.DomainAccessRetention),
		AuthRefreshCutoff:  now.Add(-input.AuthRefreshRetention),
	}

	if input.DryRun {
		return s.maintenanceCleanupDryRun(ctx, result)
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		upsertResult := tx.Exec(aggregateTrafficUsageSQL, result.TrafficCutoff)
		if err := mapGormError(upsertResult.Error); err != nil {
			return err
		}
		result.TrafficDailyRowsUpserted = upsertResult.RowsAffected

		trafficDelete := tx.Exec(deleteTrafficUsageSQL, result.TrafficCutoff)
		if err := mapGormError(trafficDelete.Error); err != nil {
			return err
		}
		result.TrafficRowsDeleted = trafficDelete.RowsAffected

		auditDelete := tx.Exec(deleteAuditLogsSQL, result.AuditCutoff)
		if err := mapGormError(auditDelete.Error); err != nil {
			return err
		}
		result.AuditRowsDeleted = auditDelete.RowsAffected

		domainAccessDelete := tx.Exec(deleteDomainAccessLogsSQL, result.DomainAccessCutoff)
		if err := mapGormError(domainAccessDelete.Error); err != nil {
			return err
		}
		result.DomainAccessRowsDeleted = domainAccessDelete.RowsAffected

		dailyDelete := tx.Exec(deleteTrafficUsageDailySQL, result.TrafficDailyCutoff)
		if err := mapGormError(dailyDelete.Error); err != nil {
			return err
		}
		result.TrafficDailyRowsDeleted = dailyDelete.RowsAffected

		authRefreshDelete := tx.Exec(deleteAuthRefreshTokensSQL, result.AuthRefreshCutoff, result.AuthRefreshCutoff)
		if err := mapGormError(authRefreshDelete.Error); err != nil {
			return err
		}
		result.AuthRefreshRowsDeleted = authRefreshDelete.RowsAffected
		return nil
	})
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	return result, nil
}

func (s *Store) maintenanceCleanupDryRun(ctx context.Context, result MaintenanceCleanupResult) (MaintenanceCleanupResult, error) {
	tx := s.db.WithContext(ctx)

	trafficRows, err := countTrafficRowsForCleanupTx(tx, result.TrafficCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficRowsDeleted = trafficRows
	dailyRows, err := countTrafficDailyGroupsForCleanupTx(tx, result.TrafficCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficDailyRowsUpserted = dailyRows

	auditRows, err := countAuditRowsForCleanupTx(tx, result.AuditCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.AuditRowsDeleted = auditRows

	domainAccessRows, err := countDomainAccessRowsForCleanupTx(tx, result.DomainAccessCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.DomainAccessRowsDeleted = domainAccessRows

	dailyDeleteRows, err := countTrafficDailyRowsForCleanupTx(tx, result.TrafficDailyCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.TrafficDailyRowsDeleted = dailyDeleteRows

	authRefreshRows, err := countAuthRefreshRowsForCleanupTx(tx, result.AuthRefreshCutoff)
	if err != nil {
		return MaintenanceCleanupResult{}, err
	}
	result.AuthRefreshRowsDeleted = authRefreshRows
	return result, nil
}

func countTrafficRowsForCleanupTx(tx *gorm.DB, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
SELECT COUNT(*)
FROM traffic_usage
WHERE recorded_at < ?
`, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countTrafficDailyGroupsForCleanupTx(tx *gorm.DB, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
SELECT COUNT(*)
FROM (
    SELECT 1
    FROM traffic_usage
    WHERE recorded_at < ?
    GROUP BY proxy_account_id, proxy_node_id, recorded_at::date
) AS daily_groups
`, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countAuditRowsForCleanupTx(tx *gorm.DB, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
SELECT COUNT(*)
FROM audit_logs
WHERE created_at < ?
`, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countTrafficDailyRowsForCleanupTx(tx *gorm.DB, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
SELECT COUNT(*)
FROM traffic_usage_daily
WHERE day < ?::date
`, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countDomainAccessRowsForCleanupTx(tx *gorm.DB, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
SELECT COUNT(*)
FROM domain_access_logs
WHERE accessed_at < ?
`, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

func countAuthRefreshRowsForCleanupTx(tx *gorm.DB, cutoff time.Time) (int64, error) {
	var count int64
	err := tx.Raw(`
SELECT COUNT(*)
FROM auth_refresh_tokens
WHERE expires_at < ? OR revoked_at < ?
`, cutoff, cutoff).Scan(&count).Error
	return count, mapGormError(err)
}

const aggregateTrafficUsageSQL = `
WITH aggregated AS (
    SELECT
        proxy_account_id,
        proxy_node_id,
        recorded_at::date AS day,
        SUM(upload_bytes)::bigint AS upload_bytes,
        SUM(download_bytes)::bigint AS download_bytes
    FROM traffic_usage
    WHERE recorded_at < ?
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
DELETE FROM traffic_usage
WHERE recorded_at < ?
`

const deleteAuditLogsSQL = `
DELETE FROM audit_logs
WHERE created_at < ?
`

const deleteTrafficUsageDailySQL = `
DELETE FROM traffic_usage_daily
WHERE day < ?::date
`

const deleteDomainAccessLogsSQL = `
DELETE FROM domain_access_logs
WHERE accessed_at < ?
`

const deleteAuthRefreshTokensSQL = `
DELETE FROM auth_refresh_tokens
WHERE expires_at < ? OR revoked_at < ?
`
