package store

import (
	"context"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
)

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
