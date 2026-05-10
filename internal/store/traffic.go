package store

import (
	"context"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
)

type TrafficUsageTotalFilter struct {
	CustomerID     string
	ProxyAccountID string
	Since          *time.Time
	Until          *time.Time
}

func (s *Store) RecordTrafficUsageBatch(ctx context.Context, nodeID string, deltas []domain.TrafficDelta, recordedAt time.Time) (int, error) {
	if nodeID == "" || len(deltas) == 0 {
		return 0, nil
	}
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}

	accountIDs := make([]string, 0, len(deltas))
	seen := make(map[string]struct{}, len(deltas))
	for _, delta := range deltas {
		if delta.ProxyAccountID == "" || (delta.UploadBytes == 0 && delta.DownloadBytes == 0) {
			continue
		}
		if _, ok := seen[delta.ProxyAccountID]; ok {
			continue
		}
		seen[delta.ProxyAccountID] = struct{}{}
		accountIDs = append(accountIDs, delta.ProxyAccountID)
	}
	if len(accountIDs) == 0 {
		return 0, nil
	}

	var boundAccountIDs []string
	if err := s.db.WithContext(ctx).
		Table("proxy_account_nodes").
		Select("proxy_account_id").
		Where("proxy_node_id = ?", nodeID).
		Where("proxy_account_id IN ?", accountIDs).
		Scan(&boundAccountIDs).Error; err != nil {
		return 0, mapGormError(err)
	}
	bound := make(map[string]struct{}, len(boundAccountIDs))
	for _, accountID := range boundAccountIDs {
		bound[accountID] = struct{}{}
	}

	usages := make([]domain.TrafficUsage, 0, len(deltas))
	for _, delta := range deltas {
		if _, ok := bound[delta.ProxyAccountID]; !ok {
			continue
		}
		if delta.UploadBytes == 0 && delta.DownloadBytes == 0 {
			continue
		}
		id, err := security.NewID()
		if err != nil {
			return 0, err
		}
		usages = append(usages, domain.TrafficUsage{
			ID:             id,
			ProxyAccountID: delta.ProxyAccountID,
			ProxyNodeID:    nodeID,
			UploadBytes:    delta.UploadBytes,
			DownloadBytes:  delta.DownloadBytes,
			RecordedAt:     recordedAt,
		})
	}
	if len(usages) == 0 {
		return 0, nil
	}
	if err := s.db.WithContext(ctx).Omit("ProxyAccount", "ProxyNode").Create(&usages).Error; err != nil {
		return 0, mapGormError(err)
	}
	return len(usages), nil
}

func (s *Store) TrafficUsageTotal(ctx context.Context, filter TrafficUsageTotalFilter) (domain.TrafficUsageTotal, error) {
	detail, err := s.trafficUsageDetailTotal(ctx, filter)
	if err != nil {
		return domain.TrafficUsageTotal{}, err
	}
	daily, err := s.trafficUsageDailyTotal(ctx, filter)
	if err != nil {
		return domain.TrafficUsageTotal{}, err
	}
	return domain.TrafficUsageTotal{
		CustomerID:     filter.CustomerID,
		ProxyAccountID: filter.ProxyAccountID,
		UploadBytes:    detail.UploadBytes + daily.UploadBytes,
		DownloadBytes:  detail.DownloadBytes + daily.DownloadBytes,
	}, nil
}

type trafficUsageSum struct {
	UploadBytes   int64
	DownloadBytes int64
}

func (s *Store) trafficUsageDetailTotal(ctx context.Context, filter TrafficUsageTotalFilter) (trafficUsageSum, error) {
	var sum trafficUsageSum
	query := s.db.WithContext(ctx).
		Table("traffic_usage AS tu").
		Joins("JOIN proxy_accounts AS pa ON pa.id = tu.proxy_account_id").
		Select("COALESCE(SUM(tu.upload_bytes), 0)::bigint AS upload_bytes, COALESCE(SUM(tu.download_bytes), 0)::bigint AS download_bytes")
	if filter.CustomerID != "" {
		query = query.Where("pa.customer_id = ?", filter.CustomerID)
	}
	if filter.ProxyAccountID != "" {
		query = query.Where("tu.proxy_account_id = ?", filter.ProxyAccountID)
	}
	if filter.Since != nil {
		query = query.Where("tu.recorded_at >= ?", filter.Since.UTC())
	}
	if filter.Until != nil {
		query = query.Where("tu.recorded_at < ?", filter.Until.UTC())
	}
	err := query.Scan(&sum).Error
	return sum, mapGormError(err)
}

func (s *Store) trafficUsageDailyTotal(ctx context.Context, filter TrafficUsageTotalFilter) (trafficUsageSum, error) {
	var sum trafficUsageSum
	query := s.db.WithContext(ctx).
		Table("traffic_usage_daily AS tud").
		Joins("JOIN proxy_accounts AS pa ON pa.id = tud.proxy_account_id").
		Select("COALESCE(SUM(tud.upload_bytes), 0)::bigint AS upload_bytes, COALESCE(SUM(tud.download_bytes), 0)::bigint AS download_bytes")
	if filter.CustomerID != "" {
		query = query.Where("pa.customer_id = ?", filter.CustomerID)
	}
	if filter.ProxyAccountID != "" {
		query = query.Where("tud.proxy_account_id = ?", filter.ProxyAccountID)
	}
	if filter.Since != nil {
		query = query.Where("tud.day >= ?::date", filter.Since.UTC())
	}
	if filter.Until != nil {
		query = query.Where("tud.day < ?::date", filter.Until.UTC())
	}
	err := query.Scan(&sum).Error
	return sum, mapGormError(err)
}
