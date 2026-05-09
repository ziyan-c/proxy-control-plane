package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid input")
)

type Store struct {
	db *gorm.DB
}

type ProxyNodeUpsert struct {
	Node            domain.ProxyNode
	PreserveEnabled bool
}

type ProxyNodeUpsertResult struct {
	Node    domain.ProxyNode
	Created bool
}

func Open(ctx context.Context, databaseURL string, autoCreateDatabase bool) (*Store, error) {
	db, err := openGorm(ctx, databaseURL)
	if err == nil {
		return &Store{db: db}, nil
	}

	if !autoCreateDatabase {
		return nil, err
	}

	if createErr := ensureDatabase(ctx, databaseURL); createErr != nil {
		return nil, fmt.Errorf("connect database: %w; auto-create database: %w", err, createErr)
	}

	db, err = openGorm(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func openGorm(ctx context.Context, databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return db, nil
}

func ensureDatabase(ctx context.Context, databaseURL string) error {
	targetDatabase, maintenanceURL, err := maintenanceDatabaseURL(databaseURL)
	if err != nil {
		return err
	}
	if targetDatabase == "" || targetDatabase == "postgres" {
		return nil
	}

	db, err := sql.Open("pgx", maintenanceURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, targetDatabase).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = db.ExecContext(ctx, `CREATE DATABASE `+quoteIdentifier(targetDatabase))
	return err
}

func maintenanceDatabaseURL(databaseURL string) (string, string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", "", err
	}
	targetDatabase, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil {
		return "", "", err
	}
	parsed.Path = "/postgres"
	parsed.RawPath = ""
	return targetDatabase, parsed.String(), nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (s *Store) CreateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	var err error
	if customer.ID == "" {
		customer.ID, err = security.NewID()
		if err != nil {
			return domain.Customer{}, err
		}
	}
	customer.Status = domain.CustomerStatusOrDefault(customer.Status)
	if err := s.db.WithContext(ctx).Omit("ProxyAccounts", "SubscriptionTokens", "AuthRefreshTokens").Create(&customer).Error; err != nil {
		return domain.Customer{}, mapGormError(err)
	}
	return customer, nil
}

func (s *Store) ListCustomers(ctx context.Context) ([]domain.Customer, error) {
	var customers []domain.Customer
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&customers).Error
	return customers, mapGormError(err)
}

func (s *Store) GetCustomer(ctx context.Context, id string) (domain.Customer, error) {
	var customer domain.Customer
	err := s.db.WithContext(ctx).First(&customer, "id = ?", id).Error
	if err != nil {
		return domain.Customer{}, mapGormError(err)
	}
	return customer, nil
}

func (s *Store) GetCustomerByEmail(ctx context.Context, email string) (domain.Customer, error) {
	var customer domain.Customer
	err := s.db.WithContext(ctx).First(&customer, "email = ?", email).Error
	if err != nil {
		return domain.Customer{}, mapGormError(err)
	}
	return customer, nil
}

func (s *Store) UpdateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	if _, err := s.GetCustomer(ctx, customer.ID); err != nil {
		return domain.Customer{}, err
	}
	customer.Status = domain.CustomerStatusOrDefault(customer.Status)
	if err := s.db.WithContext(ctx).Omit("ProxyAccounts", "SubscriptionTokens", "AuthRefreshTokens").Save(&customer).Error; err != nil {
		return domain.Customer{}, mapGormError(err)
	}
	return s.GetCustomer(ctx, customer.ID)
}

func (s *Store) DeleteCustomer(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, &domain.Customer{}, id)
}

func (s *Store) CreateAuthRefreshToken(ctx context.Context, token domain.AuthRefreshToken) (domain.AuthRefreshToken, error) {
	var err error
	if token.ID == "" {
		token.ID, err = security.NewID()
		if err != nil {
			return domain.AuthRefreshToken{}, err
		}
	}
	if token.PrincipalType == "" || token.Subject == "" || token.SessionVersion == "" || token.TokenHash == "" || token.ExpiresAt.IsZero() {
		return domain.AuthRefreshToken{}, ErrInvalid
	}
	if token.PrincipalType != security.PrincipalTypeAdmin && token.PrincipalType != security.PrincipalTypeCustomer {
		return domain.AuthRefreshToken{}, ErrInvalid
	}
	if token.PrincipalType == security.PrincipalTypeCustomer && token.CustomerID == nil {
		return domain.AuthRefreshToken{}, ErrInvalid
	}
	if token.PrincipalType == security.PrincipalTypeAdmin && token.CustomerID != nil {
		return domain.AuthRefreshToken{}, ErrInvalid
	}
	token.Enabled = true
	if err := s.db.WithContext(ctx).Omit("Customer").Create(&token).Error; err != nil {
		return domain.AuthRefreshToken{}, mapGormError(err)
	}
	return token, nil
}

func (s *Store) GetAuthRefreshTokenByHash(ctx context.Context, tokenHash string) (domain.AuthRefreshToken, error) {
	var token domain.AuthRefreshToken
	err := s.db.WithContext(ctx).First(&token, "token_hash = ?", tokenHash).Error
	if err != nil {
		return domain.AuthRefreshToken{}, mapGormError(err)
	}
	return token, nil
}

func (s *Store) RotateAuthRefreshToken(ctx context.Context, currentID string, next domain.AuthRefreshToken, usedAt time.Time, ip string, userAgent string) (domain.AuthRefreshToken, error) {
	var rotated domain.AuthRefreshToken
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current domain.AuthRefreshToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", currentID).Error; err != nil {
			return mapGormError(err)
		}
		if !current.Enabled || current.RevokedAt != nil {
			return ErrInvalid
		}

		var err error
		if next.ID == "" {
			next.ID, err = security.NewID()
			if err != nil {
				return err
			}
		}
		if next.PrincipalType == "" {
			next.PrincipalType = current.PrincipalType
		}
		if next.Subject == "" {
			next.Subject = current.Subject
		}
		if next.CustomerID == nil && current.CustomerID != nil {
			customerID := *current.CustomerID
			next.CustomerID = &customerID
		}
		if next.TokenHash == "" || next.SessionVersion == "" || next.ExpiresAt.IsZero() {
			return ErrInvalid
		}
		if next.PrincipalType != security.PrincipalTypeAdmin && next.PrincipalType != security.PrincipalTypeCustomer {
			return ErrInvalid
		}
		if next.PrincipalType == security.PrincipalTypeCustomer && next.CustomerID == nil {
			return ErrInvalid
		}
		if next.PrincipalType == security.PrincipalTypeAdmin && next.CustomerID != nil {
			return ErrInvalid
		}
		next.Enabled = true

		now := usedAt.UTC()
		current.Enabled = false
		current.LastUsedAt = &now
		current.LastUsedIP = ip
		current.LastUsedUserAgent = userAgent
		current.RevokedAt = &now
		current.ReplacedByID = next.ID
		if err := tx.Model(&domain.AuthRefreshToken{}).Where("id = ?", current.ID).Updates(map[string]any{
			"enabled":              current.Enabled,
			"last_used_at":         current.LastUsedAt,
			"last_used_ip":         current.LastUsedIP,
			"last_used_user_agent": current.LastUsedUserAgent,
			"revoked_at":           current.RevokedAt,
			"replaced_by_id":       current.ReplacedByID,
		}).Error; err != nil {
			return mapGormError(err)
		}

		if err := tx.Omit("Customer").Create(&next).Error; err != nil {
			return mapGormError(err)
		}
		rotated = next
		return nil
	})
	return rotated, err
}

func (s *Store) RevokeAuthRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt time.Time, ip string, userAgent string) error {
	now := revokedAt.UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token domain.AuthRefreshToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&token, "token_hash = ?", tokenHash).Error; err != nil {
			if errors.Is(mapGormError(err), ErrNotFound) {
				return nil
			}
			return mapGormError(err)
		}
		if token.RevokedAt != nil {
			return nil
		}
		return mapGormError(tx.Model(&domain.AuthRefreshToken{}).Where("id = ?", token.ID).Updates(map[string]any{
			"enabled":              false,
			"last_used_at":         &now,
			"last_used_ip":         ip,
			"last_used_user_agent": userAgent,
			"revoked_at":           &now,
		}).Error)
	})
	return err
}

func (s *Store) RevokeCustomerAuthRefreshTokens(ctx context.Context, customerID string, revokedAt time.Time, ip string, userAgent string) (int64, error) {
	return s.revokeAuthRefreshTokens(ctx, revokedAt, ip, userAgent, "principal_type = ? AND customer_id = ? AND revoked_at IS NULL", security.PrincipalTypeCustomer, customerID)
}

func (s *Store) RevokeAdminAuthRefreshTokens(ctx context.Context, revokedAt time.Time, ip string, userAgent string) (int64, error) {
	return s.revokeAuthRefreshTokens(ctx, revokedAt, ip, userAgent, "principal_type = ? AND revoked_at IS NULL", security.PrincipalTypeAdmin)
}

func (s *Store) revokeAuthRefreshTokens(ctx context.Context, revokedAt time.Time, ip string, userAgent string, query string, args ...any) (int64, error) {
	now := revokedAt.UTC()
	updates := map[string]any{
		"enabled":              false,
		"last_used_at":         &now,
		"last_used_ip":         ip,
		"last_used_user_agent": userAgent,
		"revoked_at":           &now,
	}
	result := s.db.WithContext(ctx).Model(&domain.AuthRefreshToken{}).Where(query, args...).Updates(updates)
	if err := mapGormError(result.Error); err != nil {
		return 0, err
	}
	return result.RowsAffected, nil
}

func (s *Store) CreateProxyNode(ctx context.Context, node domain.ProxyNode) (domain.ProxyNode, error) {
	var err error
	if node.ID == "" {
		node.ID, err = security.NewID()
		if err != nil {
			return domain.ProxyNode{}, err
		}
	}
	setNodeDefaults(&node)
	if !validNodeRuntime(node.Runtime) {
		return domain.ProxyNode{}, ErrInvalid
	}
	if !validPort(node.Port) {
		return domain.ProxyNode{}, ErrInvalid
	}
	if !validRuntimeAPIConfig(node) {
		return domain.ProxyNode{}, ErrInvalid
	}
	if err := s.db.WithContext(ctx).Omit("ProxyAccounts").Create(&node).Error; err != nil {
		return domain.ProxyNode{}, mapGormError(err)
	}
	return node, nil
}

func (s *Store) ListProxyNodes(ctx context.Context) ([]domain.ProxyNode, error) {
	var nodes []domain.ProxyNode
	err := s.db.WithContext(ctx).Order("name ASC").Find(&nodes).Error
	return nodes, mapGormError(err)
}

func (s *Store) GetProxyNode(ctx context.Context, id string) (domain.ProxyNode, error) {
	var node domain.ProxyNode
	err := s.db.WithContext(ctx).First(&node, "id = ?", id).Error
	if err != nil {
		return domain.ProxyNode{}, mapGormError(err)
	}
	return node, nil
}

func (s *Store) GetProxyNodeByName(ctx context.Context, name string) (domain.ProxyNode, error) {
	var node domain.ProxyNode
	err := s.db.WithContext(ctx).First(&node, "name = ?", name).Error
	if err != nil {
		return domain.ProxyNode{}, mapGormError(err)
	}
	return node, nil
}

func (s *Store) UpsertProxyNodeByName(ctx context.Context, node domain.ProxyNode, preserveEnabled bool) (domain.ProxyNode, bool, error) {
	results, err := s.UpsertProxyNodesByName(ctx, []ProxyNodeUpsert{
		{
			Node:            node,
			PreserveEnabled: preserveEnabled,
		},
	})
	if err != nil {
		return domain.ProxyNode{}, false, err
	}
	return results[0].Node, results[0].Created, nil
}

func (s *Store) UpsertProxyNodesByName(ctx context.Context, inputs []ProxyNodeUpsert) ([]ProxyNodeUpsertResult, error) {
	results := make([]ProxyNodeUpsertResult, 0, len(inputs))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			node, created, err := upsertProxyNodeByNameTx(tx, input.Node, input.PreserveEnabled)
			if err != nil {
				return err
			}
			results = append(results, ProxyNodeUpsertResult{
				Node:    node,
				Created: created,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func upsertProxyNodeByNameTx(tx *gorm.DB, node domain.ProxyNode, preserveEnabled bool) (domain.ProxyNode, bool, error) {
	if strings.TrimSpace(node.Name) == "" {
		return domain.ProxyNode{}, false, ErrInvalid
	}
	setNodeDefaults(&node)
	if !validNodeRuntime(node.Runtime) {
		return domain.ProxyNode{}, false, ErrInvalid
	}
	if !validPort(node.Port) {
		return domain.ProxyNode{}, false, ErrInvalid
	}
	if !validRuntimeAPIConfig(node) {
		return domain.ProxyNode{}, false, ErrInvalid
	}

	var existing domain.ProxyNode
	err := tx.First(&existing, "name = ?", node.Name).Error
	if err != nil {
		mapped := mapGormError(err)
		if !errors.Is(mapped, ErrNotFound) {
			return domain.ProxyNode{}, false, mapped
		}
		var idErr error
		if node.ID == "" {
			node.ID, idErr = security.NewID()
			if idErr != nil {
				return domain.ProxyNode{}, false, idErr
			}
		}
		if preserveEnabled {
			node.Enabled = true
		}
		if err := tx.Omit("ProxyAccounts").Create(&node).Error; err != nil {
			return domain.ProxyNode{}, false, mapGormError(err)
		}
		return node, true, nil
	}

	if preserveEnabled {
		node.Enabled = existing.Enabled
	}
	node.ID = existing.ID
	node.CreatedAt = existing.CreatedAt
	if err := tx.Omit("ProxyAccounts").Save(&node).Error; err != nil {
		return domain.ProxyNode{}, false, mapGormError(err)
	}
	return node, false, nil
}

func (s *Store) UpdateProxyNode(ctx context.Context, node domain.ProxyNode) (domain.ProxyNode, error) {
	if _, err := s.GetProxyNode(ctx, node.ID); err != nil {
		return domain.ProxyNode{}, err
	}
	setNodeDefaults(&node)
	if !validNodeRuntime(node.Runtime) {
		return domain.ProxyNode{}, ErrInvalid
	}
	if !validPort(node.Port) {
		return domain.ProxyNode{}, ErrInvalid
	}
	if !validRuntimeAPIConfig(node) {
		return domain.ProxyNode{}, ErrInvalid
	}
	if err := s.db.WithContext(ctx).Omit("ProxyAccounts").Save(&node).Error; err != nil {
		return domain.ProxyNode{}, mapGormError(err)
	}
	return s.GetProxyNode(ctx, node.ID)
}

func (s *Store) DeleteProxyNode(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, &domain.ProxyNode{}, id)
}

func (s *Store) CreateProxyAccount(ctx context.Context, account domain.ProxyAccount) (domain.ProxyAccount, error) {
	if _, err := s.GetCustomer(ctx, account.CustomerID); err != nil {
		return domain.ProxyAccount{}, err
	}

	var err error
	if account.ID == "" {
		account.ID, err = security.NewID()
		if err != nil {
			return domain.ProxyAccount{}, err
		}
	}
	if account.UUID == "" {
		account.UUID, err = security.NewID()
		if err != nil {
			return domain.ProxyAccount{}, err
		}
	}
	if account.Protocol == "" {
		account.Protocol = "vless"
	}

	nodes, err := s.loadNodesByIDs(ctx, account.NodeIDs)
	if err != nil {
		return domain.ProxyAccount{}, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Nodes", "Customer").Create(&account).Error; err != nil {
			return mapGormError(err)
		}
		if len(nodes) > 0 {
			if err := tx.Model(&account).Association("Nodes").Replace(nodes); err != nil {
				return mapGormError(err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	return s.GetProxyAccount(ctx, account.ID)
}

func (s *Store) ListProxyAccounts(ctx context.Context) ([]domain.ProxyAccount, error) {
	return s.listProxyAccounts(ctx, "")
}

func (s *Store) ListProxyAccountsByCustomer(ctx context.Context, customerID string) ([]domain.ProxyAccount, error) {
	return s.listProxyAccounts(ctx, customerID)
}

func (s *Store) listProxyAccounts(ctx context.Context, customerID string) ([]domain.ProxyAccount, error) {
	var accounts []domain.ProxyAccount
	query := s.db.WithContext(ctx).Preload("Nodes").Order("created_at DESC")
	if customerID != "" {
		query = query.Where("customer_id = ?", customerID)
	}
	if err := query.Find(&accounts).Error; err != nil {
		return nil, mapGormError(err)
	}
	for i := range accounts {
		fillNodeIDs(&accounts[i])
	}
	return accounts, nil
}

func (s *Store) GetProxyAccount(ctx context.Context, id string) (domain.ProxyAccount, error) {
	var account domain.ProxyAccount
	err := s.db.WithContext(ctx).Preload("Nodes").First(&account, "id = ?", id).Error
	if err != nil {
		return domain.ProxyAccount{}, mapGormError(err)
	}
	fillNodeIDs(&account)
	return account, nil
}

func (s *Store) UpdateProxyAccount(ctx context.Context, account domain.ProxyAccount) (domain.ProxyAccount, error) {
	if _, err := s.GetProxyAccount(ctx, account.ID); err != nil {
		return domain.ProxyAccount{}, err
	}
	nodes, err := s.loadNodesByIDs(ctx, account.NodeIDs)
	if err != nil {
		return domain.ProxyAccount{}, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Nodes", "Customer").Save(&account).Error; err != nil {
			return mapGormError(err)
		}
		if account.NodeIDs != nil {
			if err := tx.Model(&account).Association("Nodes").Replace(nodes); err != nil {
				return mapGormError(err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	return s.GetProxyAccount(ctx, account.ID)
}

func (s *Store) DeleteProxyAccount(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, &domain.ProxyAccount{}, id)
}

func (s *Store) CreateSubscriptionToken(ctx context.Context, token domain.SubscriptionToken) (domain.SubscriptionToken, error) {
	if _, err := s.GetCustomer(ctx, token.CustomerID); err != nil {
		return domain.SubscriptionToken{}, err
	}
	var err error
	if token.ID == "" {
		token.ID, err = security.NewID()
		if err != nil {
			return domain.SubscriptionToken{}, err
		}
	}
	if token.Name == "" {
		token.Name = "default"
	}
	if err := s.db.WithContext(ctx).Omit("Customer").Create(&token).Error; err != nil {
		return domain.SubscriptionToken{}, mapGormError(err)
	}
	return token, nil
}

func (s *Store) ListSubscriptionTokens(ctx context.Context, customerID string) ([]domain.SubscriptionToken, error) {
	var tokens []domain.SubscriptionToken
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if customerID != "" {
		query = query.Where("customer_id = ?", customerID)
	}
	err := query.Find(&tokens).Error
	return tokens, mapGormError(err)
}

func (s *Store) GetSubscriptionToken(ctx context.Context, id string) (domain.SubscriptionToken, error) {
	var token domain.SubscriptionToken
	err := s.db.WithContext(ctx).First(&token, "id = ?", id).Error
	if err != nil {
		return domain.SubscriptionToken{}, mapGormError(err)
	}
	return token, nil
}

func (s *Store) GetSubscriptionTokenByHash(ctx context.Context, tokenHash string) (domain.SubscriptionToken, error) {
	var token domain.SubscriptionToken
	err := s.db.WithContext(ctx).First(&token, "token_hash = ?", tokenHash).Error
	if err != nil {
		return domain.SubscriptionToken{}, mapGormError(err)
	}
	return token, nil
}

func (s *Store) UpdateSubscriptionToken(ctx context.Context, token domain.SubscriptionToken) (domain.SubscriptionToken, error) {
	if _, err := s.GetSubscriptionToken(ctx, token.ID); err != nil {
		return domain.SubscriptionToken{}, err
	}
	if err := s.db.WithContext(ctx).Omit("Customer").Save(&token).Error; err != nil {
		return domain.SubscriptionToken{}, mapGormError(err)
	}
	return s.GetSubscriptionToken(ctx, token.ID)
}

func (s *Store) RotateSubscriptionToken(ctx context.Context, id string, tokenHash string, encryptedToken string) (domain.SubscriptionToken, error) {
	result := s.db.WithContext(ctx).Model(&domain.SubscriptionToken{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"token_hash":           tokenHash,
			"encrypted_token":      encryptedToken,
			"last_used_at":         nil,
			"last_used_ip":         "",
			"last_used_user_agent": "",
		})
	if err := mapGormError(result.Error); err != nil {
		return domain.SubscriptionToken{}, err
	}
	if result.RowsAffected == 0 {
		return domain.SubscriptionToken{}, ErrNotFound
	}
	return s.GetSubscriptionToken(ctx, id)
}

func (s *Store) MarkSubscriptionUsed(ctx context.Context, id string, ip string, userAgent string) error {
	return mapGormError(s.db.WithContext(ctx).Model(&domain.SubscriptionToken{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_used_at":         time.Now().UTC(),
			"last_used_ip":         ip,
			"last_used_user_agent": userAgent,
		}).Error)
}

func (s *Store) RecordTrafficUsage(ctx context.Context, usage domain.TrafficUsage) (domain.TrafficUsage, error) {
	var err error
	if usage.ID == "" {
		usage.ID, err = security.NewID()
		if err != nil {
			return domain.TrafficUsage{}, err
		}
	}
	if usage.RecordedAt.IsZero() {
		usage.RecordedAt = time.Now().UTC()
	}
	if err := s.db.WithContext(ctx).Omit("ProxyAccount", "ProxyNode").Create(&usage).Error; err != nil {
		return domain.TrafficUsage{}, mapGormError(err)
	}
	return usage, nil
}

func (s *Store) WriteAudit(ctx context.Context, actor string, action string, metadata any) error {
	id, err := security.NewID()
	if err != nil {
		return err
	}
	metadataJSON := ""
	if metadata != nil {
		switch value := metadata.(type) {
		case string:
			metadataJSON = value
		default:
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			metadataJSON = string(data)
		}
	}
	audit := domain.AuditLog{
		ID:           id,
		Actor:        actor,
		Action:       action,
		MetadataJSON: metadataJSON,
		CreatedAt:    time.Now().UTC(),
	}
	return mapGormError(s.db.WithContext(ctx).Create(&audit).Error)
}

func (s *Store) SubscriptionData(ctx context.Context, customerID string) (domain.Customer, []domain.ProxyAccount, error) {
	customer, err := s.GetCustomer(ctx, customerID)
	if err != nil {
		return domain.Customer{}, nil, err
	}
	accounts, err := s.ListProxyAccountsByCustomer(ctx, customerID)
	if err != nil {
		return domain.Customer{}, nil, err
	}
	return customer, accounts, nil
}

func setNodeDefaults(node *domain.ProxyNode) {
	node.Runtime = normalizeNodeRuntime(node.Runtime)
	if node.Runtime == "" {
		node.Runtime = inferNodeRuntime(*node)
	}
	if node.Protocol == "" {
		node.Protocol = "vless"
	}
	if node.Runtime == "xray" {
		if node.Transport == "" {
			if node.Path != "" {
				node.Transport = "ws"
			} else {
				node.Transport = "tcp"
			}
		}
		if node.Security == "" {
			if node.Transport == "ws" || node.Path != "" {
				node.Security = "tls"
			} else {
				node.Security = "reality"
			}
		}
		if node.Transport == "ws" && node.Path == "" {
			node.Path = "/v2ray"
		}
	}
	if node.Port == 0 {
		node.Port = 443
	}
	if node.Transport == "" {
		node.Transport = "tcp"
	}
	if node.Security == "" {
		node.Security = "none"
	}
	if node.RuntimeAPIEnabled && node.RuntimeInboundTag == "" {
		node.RuntimeInboundTag = "proxy-control-plane-vless-in"
	}
}

func inferNodeRuntime(node domain.ProxyNode) string {
	if strings.EqualFold(node.Transport, "ws") || node.Path == "/v2ray" {
		return "xray"
	}
	if strings.EqualFold(node.Security, "reality") || node.RealityPublicKey != "" || node.RealityShortID != "" {
		return "xray"
	}
	return "custom"
}

func normalizeNodeRuntime(runtime string) string {
	return strings.ToLower(strings.TrimSpace(runtime))
}

func validNodeRuntime(runtime string) bool {
	switch runtime {
	case "custom", "xray":
		return true
	default:
		return false
	}
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validRuntimeAPIPort(port int) bool {
	return port == 0 || validPort(port)
}

func validRuntimeAPIConfig(node domain.ProxyNode) bool {
	if !validRuntimeAPIPort(node.RuntimeAPIPort) {
		return false
	}
	if !node.RuntimeAPIEnabled {
		return true
	}
	return strings.TrimSpace(node.RuntimeAPIHost) != "" &&
		validPort(node.RuntimeAPIPort) &&
		strings.TrimSpace(node.RuntimeInboundTag) != ""
}

func (s *Store) loadNodesByIDs(ctx context.Context, nodeIDs []string) ([]domain.ProxyNode, error) {
	nodeIDs = uniqueStrings(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	var nodes []domain.ProxyNode
	if err := s.db.WithContext(ctx).Where("id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
		return nil, mapGormError(err)
	}
	if len(nodes) != len(nodeIDs) {
		return nil, ErrNotFound
	}
	return nodes, nil
}

func fillNodeIDs(account *domain.ProxyAccount) {
	account.NodeIDs = make([]string, 0, len(account.Nodes))
	for _, node := range account.Nodes {
		account.NodeIDs = append(account.NodeIDs, node.ID)
	}
}

func deleteByID(ctx context.Context, db *gorm.DB, model any, id string) error {
	result := db.WithContext(ctx).Delete(model, "id = ?", id)
	if err := mapGormError(result.Error); err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func mapGormError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrConflict
	}
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return ErrInvalid
	}
	return err
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
