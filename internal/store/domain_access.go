package store

import (
	"context"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
	"gorm.io/gorm"
)

type DomainAccessLogFilter struct {
	ProxyAccountID string
	ProxyNodeID    string
	Domain         string
	Since          *time.Time
	Until          *time.Time
	Limit          int
}

func (s *Store) RecordDomainAccessLogs(ctx context.Context, logs []domain.DomainAccessLog) (int, error) {
	if len(logs) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	records := make([]domain.DomainAccessLog, 0, len(logs))
	for _, log := range logs {
		if log.ProxyAccountID == "" || log.ProxyNodeID == "" || log.Domain == "" {
			return 0, ErrInvalid
		}
		if log.EventCount <= 0 || log.UploadBytes < 0 || log.DownloadBytes < 0 {
			return 0, ErrInvalid
		}
		if log.ID == "" {
			id, err := security.NewID()
			if err != nil {
				return 0, err
			}
			log.ID = id
		}
		if log.AccessedAt.IsZero() {
			log.AccessedAt = now
		} else {
			log.AccessedAt = log.AccessedAt.UTC()
		}
		if log.CreatedAt.IsZero() {
			log.CreatedAt = now
		} else {
			log.CreatedAt = log.CreatedAt.UTC()
		}
		records = append(records, log)
	}
	if err := s.db.WithContext(ctx).Omit("ProxyAccount", "ProxyNode").Create(&records).Error; err != nil {
		return 0, mapGormError(err)
	}
	return len(records), nil
}

func (s *Store) ListDomainAccessLogs(ctx context.Context, filter DomainAccessLogFilter) ([]domain.DomainAccessLog, error) {
	query := applyDomainAccessLogFilter(s.db.WithContext(ctx).Model(&domain.DomainAccessLog{}), filter)
	limit := filterLimit(filter.Limit)
	var logs []domain.DomainAccessLog
	if err := query.Order("accessed_at DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, mapGormError(err)
	}
	return logs, nil
}

func (s *Store) SummarizeDomainAccessLogs(ctx context.Context, filter DomainAccessLogFilter) ([]domain.DomainAccessSummary, error) {
	query := s.db.WithContext(ctx).
		Table("domain_access_logs").
		Select(`
proxy_account_id,
proxy_node_id,
domain,
SUM(event_count)::bigint AS event_count,
SUM(upload_bytes)::bigint AS upload_bytes,
SUM(download_bytes)::bigint AS download_bytes,
MIN(accessed_at) AS first_accessed_at,
MAX(accessed_at) AS last_accessed_at
`)
	query = applyDomainAccessLogFilter(query, filter).
		Group("proxy_account_id, proxy_node_id, domain").
		Order("SUM(event_count) DESC, SUM(upload_bytes + download_bytes) DESC")
	var rows []domain.DomainAccessSummary
	if err := query.Limit(filterLimit(filter.Limit)).Scan(&rows).Error; err != nil {
		return nil, mapGormError(err)
	}
	return rows, nil
}

func applyDomainAccessLogFilter(query *gorm.DB, filter DomainAccessLogFilter) *gorm.DB {
	if filter.ProxyAccountID != "" {
		query = query.Where("proxy_account_id = ?", filter.ProxyAccountID)
	}
	if filter.ProxyNodeID != "" {
		query = query.Where("proxy_node_id = ?", filter.ProxyNodeID)
	}
	if filter.Domain != "" {
		query = query.Where("domain = ?", filter.Domain)
	}
	if filter.Since != nil {
		query = query.Where("accessed_at >= ?", filter.Since.UTC())
	}
	if filter.Until != nil {
		query = query.Where("accessed_at < ?", filter.Until.UTC())
	}
	return query
}

func filterLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
