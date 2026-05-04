package store

import (
	"context"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

func (s *Store) Migrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(
		&domain.Customer{},
		&domain.ProxyNode{},
		&domain.ProxyAccount{},
		&domain.SubscriptionToken{},
		&domain.TrafficUsage{},
		&domain.AuditLog{},
	)
}
