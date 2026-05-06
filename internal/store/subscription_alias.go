package store

import (
	"context"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
	subgen "github.com/ziyan-c/proxy-control-plane/internal/subscription"
)

func (s *Store) CreateSubscriptionAlias(ctx context.Context, alias domain.SubscriptionAlias) (domain.SubscriptionAlias, error) {
	if _, err := s.GetCustomer(ctx, alias.CustomerID); err != nil {
		return domain.SubscriptionAlias{}, err
	}
	var err error
	if alias.ID == "" {
		alias.ID, err = security.NewID()
		if err != nil {
			return domain.SubscriptionAlias{}, err
		}
	}
	if err := setSubscriptionAliasDefaults(&alias); err != nil {
		return domain.SubscriptionAlias{}, err
	}
	if err := s.db.WithContext(ctx).Omit("Customer").Create(&alias).Error; err != nil {
		return domain.SubscriptionAlias{}, mapGormError(err)
	}
	return alias, nil
}

func (s *Store) ListSubscriptionAliases(ctx context.Context, customerID string) ([]domain.SubscriptionAlias, error) {
	var aliases []domain.SubscriptionAlias
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if customerID != "" {
		query = query.Where("customer_id = ?", customerID)
	}
	err := query.Find(&aliases).Error
	return aliases, mapGormError(err)
}

func (s *Store) GetSubscriptionAlias(ctx context.Context, id string) (domain.SubscriptionAlias, error) {
	var alias domain.SubscriptionAlias
	err := s.db.WithContext(ctx).First(&alias, "id = ?", id).Error
	if err != nil {
		return domain.SubscriptionAlias{}, mapGormError(err)
	}
	return alias, nil
}

func (s *Store) GetSubscriptionAliasByPathHash(ctx context.Context, pathHash string) (domain.SubscriptionAlias, error) {
	var alias domain.SubscriptionAlias
	err := s.db.WithContext(ctx).First(&alias, "path_hash = ?", pathHash).Error
	if err != nil {
		return domain.SubscriptionAlias{}, mapGormError(err)
	}
	return alias, nil
}

func (s *Store) UpdateSubscriptionAlias(ctx context.Context, alias domain.SubscriptionAlias) (domain.SubscriptionAlias, error) {
	if _, err := s.GetSubscriptionAlias(ctx, alias.ID); err != nil {
		return domain.SubscriptionAlias{}, err
	}
	if err := setSubscriptionAliasDefaults(&alias); err != nil {
		return domain.SubscriptionAlias{}, err
	}
	if err := s.db.WithContext(ctx).Omit("Customer").Save(&alias).Error; err != nil {
		return domain.SubscriptionAlias{}, mapGormError(err)
	}
	return s.GetSubscriptionAlias(ctx, alias.ID)
}

func (s *Store) DeleteSubscriptionAlias(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, &domain.SubscriptionAlias{}, id)
}

func (s *Store) MarkSubscriptionAliasUsed(ctx context.Context, id string, ip string, userAgent string) error {
	return mapGormError(s.db.WithContext(ctx).Model(&domain.SubscriptionAlias{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_used_at":         time.Now().UTC(),
			"last_used_ip":         ip,
			"last_used_user_agent": userAgent,
		}).Error)
}

func setSubscriptionAliasDefaults(alias *domain.SubscriptionAlias) error {
	path, err := subgen.CanonicalAliasPath(alias.Path)
	if err != nil {
		return ErrInvalid
	}
	alias.Path = path
	hash, err := subgen.AliasDigest(path)
	if err != nil {
		return ErrInvalid
	}
	alias.PathHash = hash
	if alias.Name == "" {
		alias.Name = path
	}
	return nil
}
