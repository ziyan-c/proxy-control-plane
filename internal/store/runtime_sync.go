package store

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

func (s *Store) ListRuntimeSyncNodes(ctx context.Context) ([]domain.ProxyNode, error) {
	var nodes []domain.ProxyNode
	err := s.db.WithContext(ctx).
		Where("enabled = ? AND runtime = ? AND runtime_api_enabled = ?", true, "xray", true).
		Order("name ASC").
		Find(&nodes).Error
	return nodes, mapGormError(err)
}

func (s *Store) ListRuntimeTargetUsers(ctx context.Context, nodeID string, now time.Time) ([]domain.RuntimeUser, error) {
	type row struct {
		ProxyAccountID string
		UUID           string
		EmailTag       string
		Flow           string
	}

	var rows []row
	err := s.db.WithContext(ctx).
		Table("proxy_accounts AS pa").
		Select("pa.id AS proxy_account_id, pa.uuid, pa.email_tag, pa.flow").
		Joins("JOIN proxy_account_nodes AS pan ON pan.proxy_account_id = pa.id").
		Joins("JOIN customers AS c ON c.id = pa.customer_id").
		Where("pan.proxy_node_id = ?", nodeID).
		Where("pa.protocol = ?", "vless").
		Where("pa.enabled = ?", true).
		Where("c.status = ?", "active").
		Where("(pa.expires_at IS NULL OR pa.expires_at > ?)", now).
		Where("(c.expires_at IS NULL OR c.expires_at > ?)", now).
		Order("pa.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, mapGormError(err)
	}

	users := make([]domain.RuntimeUser, 0, len(rows))
	for _, row := range rows {
		accountID := strings.TrimSpace(row.ProxyAccountID)
		users = append(users, domain.RuntimeUser{
			ProxyAccountID: accountID,
			Email:          domain.RuntimeProxyAccountEmail(accountID),
			UUID:           strings.TrimSpace(row.UUID),
			Flow:           strings.TrimSpace(row.Flow),
		})
	}
	return users, nil
}

func (s *Store) MarkProxyNodeRuntimeSync(ctx context.Context, nodeID string, syncedAt time.Time, syncErr string) error {
	updates := map[string]any{
		"last_runtime_sync_error": truncateRuntimeSyncError(syncErr),
	}
	if strings.TrimSpace(syncErr) == "" {
		updates["last_runtime_sync_at"] = syncedAt
	}
	result := s.db.WithContext(ctx).
		Model(&domain.ProxyNode{}).
		Where("id = ?", nodeID).
		Updates(updates)
	if err := mapGormError(result.Error); err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func truncateRuntimeSyncError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 1000 {
		return value
	}
	return value[:1000]
}
