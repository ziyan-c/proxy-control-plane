package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ziyan/proxy-control-plane/internal/domain"
	"github.com/ziyan/proxy-control-plane/internal/security"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type SQLStore struct {
	db *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*SQLStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLStore{db: db}, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLStore) CreateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM customers WHERE email = $1`, customer.Email).Scan(&existingID)
	if err == nil {
		return domain.Customer{}, ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.Customer{}, err
	}
	if customer.ID == "" {
		customer.ID, err = security.NewID()
		if err != nil {
			return domain.Customer{}, err
		}
	}
	if customer.Status == "" {
		customer.Status = "active"
	}
	return scanCustomer(s.db.QueryRowContext(
		ctx,
		`INSERT INTO customers (id, email, display_name, status, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, email, display_name, status, expires_at, created_at, updated_at`,
		customer.ID,
		customer.Email,
		nullString(customer.DisplayName),
		customer.Status,
		nullTime(customer.ExpiresAt),
	))
}

func (s *SQLStore) ListCustomers(ctx context.Context) ([]domain.Customer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, display_name, status, expires_at, created_at, updated_at FROM customers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var customers []domain.Customer
	for rows.Next() {
		customer, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		customers = append(customers, customer)
	}
	return customers, rows.Err()
}

func (s *SQLStore) GetCustomer(ctx context.Context, id string) (domain.Customer, error) {
	customer, err := scanCustomer(s.db.QueryRowContext(ctx, `SELECT id, email, display_name, status, expires_at, created_at, updated_at FROM customers WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Customer{}, ErrNotFound
	}
	return customer, err
}

func (s *SQLStore) UpdateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	updated, err := scanCustomer(s.db.QueryRowContext(
		ctx,
		`UPDATE customers
		 SET email = $2, display_name = $3, status = $4, expires_at = $5, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, email, display_name, status, expires_at, created_at, updated_at`,
		customer.ID,
		customer.Email,
		nullString(customer.DisplayName),
		customer.Status,
		nullTime(customer.ExpiresAt),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Customer{}, ErrNotFound
	}
	return updated, err
}

func (s *SQLStore) DeleteCustomer(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "customers", id)
}

func (s *SQLStore) CreateProxyNode(ctx context.Context, node domain.ProxyNode) (domain.ProxyNode, error) {
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM proxy_nodes WHERE name = $1`, node.Name).Scan(&existingID)
	if err == nil {
		return domain.ProxyNode{}, ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.ProxyNode{}, err
	}
	if node.ID == "" {
		node.ID, err = security.NewID()
		if err != nil {
			return domain.ProxyNode{}, err
		}
	}
	setNodeDefaults(&node)
	return scanProxyNode(s.db.QueryRowContext(
		ctx,
		`INSERT INTO proxy_nodes
		 (id, name, hostname, public_host, region, protocol, port, transport, security, sni, fingerprint, alpn, path, host_header, reality_public_key, reality_short_id, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		 RETURNING id, name, hostname, public_host, region, protocol, port, transport, security, sni, fingerprint, alpn, path, host_header, reality_public_key, reality_short_id, enabled, created_at, updated_at`,
		node.ID, node.Name, node.Hostname, nullString(node.PublicHost), nullString(node.Region), node.Protocol, node.Port, node.Transport, node.Security,
		nullString(node.SNI), nullString(node.Fingerprint), nullString(node.ALPN), nullString(node.Path), nullString(node.HostHeader), nullString(node.RealityPublicKey), nullString(node.RealityShortID), node.Enabled,
	))
}

func (s *SQLStore) ListProxyNodes(ctx context.Context) ([]domain.ProxyNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, hostname, public_host, region, protocol, port, transport, security, sni, fingerprint, alpn, path, host_header, reality_public_key, reality_short_id, enabled, created_at, updated_at FROM proxy_nodes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []domain.ProxyNode
	for rows.Next() {
		node, err := scanProxyNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *SQLStore) GetProxyNode(ctx context.Context, id string) (domain.ProxyNode, error) {
	node, err := scanProxyNode(s.db.QueryRowContext(ctx, `SELECT id, name, hostname, public_host, region, protocol, port, transport, security, sni, fingerprint, alpn, path, host_header, reality_public_key, reality_short_id, enabled, created_at, updated_at FROM proxy_nodes WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProxyNode{}, ErrNotFound
	}
	return node, err
}

func (s *SQLStore) UpdateProxyNode(ctx context.Context, node domain.ProxyNode) (domain.ProxyNode, error) {
	setNodeDefaults(&node)
	updated, err := scanProxyNode(s.db.QueryRowContext(
		ctx,
		`UPDATE proxy_nodes
		 SET name = $2, hostname = $3, public_host = $4, region = $5, protocol = $6, port = $7, transport = $8, security = $9,
		     sni = $10, fingerprint = $11, alpn = $12, path = $13, host_header = $14, reality_public_key = $15, reality_short_id = $16,
		     enabled = $17, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, name, hostname, public_host, region, protocol, port, transport, security, sni, fingerprint, alpn, path, host_header, reality_public_key, reality_short_id, enabled, created_at, updated_at`,
		node.ID, node.Name, node.Hostname, nullString(node.PublicHost), nullString(node.Region), node.Protocol, node.Port, node.Transport, node.Security,
		nullString(node.SNI), nullString(node.Fingerprint), nullString(node.ALPN), nullString(node.Path), nullString(node.HostHeader), nullString(node.RealityPublicKey), nullString(node.RealityShortID), node.Enabled,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProxyNode{}, ErrNotFound
	}
	return updated, err
}

func (s *SQLStore) DeleteProxyNode(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "proxy_nodes", id)
}

func (s *SQLStore) CreateProxyAccount(ctx context.Context, account domain.ProxyAccount) (domain.ProxyAccount, error) {
	if _, err := s.GetCustomer(ctx, account.CustomerID); err != nil {
		return domain.ProxyAccount{}, err
	}
	if account.ID == "" {
		id, err := security.NewID()
		if err != nil {
			return domain.ProxyAccount{}, err
		}
		account.ID = id
	}
	if account.UUID == "" {
		id, err := security.NewID()
		if err != nil {
			return domain.ProxyAccount{}, err
		}
		account.UUID = id
	}
	if account.Protocol == "" {
		account.Protocol = "vless"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO proxy_accounts (id, customer_id, protocol, uuid, email_tag, flow, enabled, expires_at, traffic_limit_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		account.ID, account.CustomerID, account.Protocol, account.UUID, account.EmailTag, nullString(account.Flow), account.Enabled, nullTime(account.ExpiresAt), nullInt64(account.TrafficLimitBytes),
	)
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	if err := replaceAccountNodesTx(ctx, tx, account.ID, account.NodeIDs); err != nil {
		return domain.ProxyAccount{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProxyAccount{}, err
	}
	return s.GetProxyAccount(ctx, account.ID)
}

func (s *SQLStore) ListProxyAccounts(ctx context.Context) ([]domain.ProxyAccount, error) {
	return s.listProxyAccounts(ctx, "")
}

func (s *SQLStore) ListProxyAccountsByCustomer(ctx context.Context, customerID string) ([]domain.ProxyAccount, error) {
	return s.listProxyAccounts(ctx, customerID)
}

func (s *SQLStore) listProxyAccounts(ctx context.Context, customerID string) ([]domain.ProxyAccount, error) {
	query := `SELECT id, customer_id, protocol, uuid, email_tag, flow, enabled, expires_at, traffic_limit_bytes, created_at, updated_at FROM proxy_accounts`
	args := []any{}
	if customerID != "" {
		query += ` WHERE customer_id = $1`
		args = append(args, customerID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.ProxyAccount
	for rows.Next() {
		account, err := scanProxyAccount(rows)
		if err != nil {
			return nil, err
		}
		if err := s.loadAccountNodes(ctx, &account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *SQLStore) GetProxyAccount(ctx context.Context, id string) (domain.ProxyAccount, error) {
	account, err := scanProxyAccount(s.db.QueryRowContext(ctx, `SELECT id, customer_id, protocol, uuid, email_tag, flow, enabled, expires_at, traffic_limit_bytes, created_at, updated_at FROM proxy_accounts WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProxyAccount{}, ErrNotFound
	}
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	if err := s.loadAccountNodes(ctx, &account); err != nil {
		return domain.ProxyAccount{}, err
	}
	return account, nil
}

func (s *SQLStore) UpdateProxyAccount(ctx context.Context, account domain.ProxyAccount) (domain.ProxyAccount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(
		ctx,
		`UPDATE proxy_accounts
		 SET customer_id = $2, protocol = $3, uuid = $4, email_tag = $5, flow = $6, enabled = $7, expires_at = $8, traffic_limit_bytes = $9,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1`,
		account.ID, account.CustomerID, account.Protocol, account.UUID, account.EmailTag, nullString(account.Flow), account.Enabled, nullTime(account.ExpiresAt), nullInt64(account.TrafficLimitBytes),
	)
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.ProxyAccount{}, err
	}
	if affected == 0 {
		return domain.ProxyAccount{}, ErrNotFound
	}
	if account.NodeIDs != nil {
		if err := replaceAccountNodesTx(ctx, tx, account.ID, account.NodeIDs); err != nil {
			return domain.ProxyAccount{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.ProxyAccount{}, err
	}
	return s.GetProxyAccount(ctx, account.ID)
}

func (s *SQLStore) DeleteProxyAccount(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "proxy_accounts", id)
}

func (s *SQLStore) CreateSubscriptionToken(ctx context.Context, token domain.SubscriptionToken) (domain.SubscriptionToken, error) {
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
	return scanSubscriptionToken(s.db.QueryRowContext(
		ctx,
		`INSERT INTO subscription_tokens (id, customer_id, name, token_hash, enabled, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, customer_id, name, token_hash, enabled, expires_at, created_at, updated_at, last_used_at, last_used_ip, last_used_user_agent`,
		token.ID, token.CustomerID, token.Name, token.TokenHash, token.Enabled, nullTime(token.ExpiresAt),
	))
}

func (s *SQLStore) ListSubscriptionTokens(ctx context.Context, customerID string) ([]domain.SubscriptionToken, error) {
	query := `SELECT id, customer_id, name, token_hash, enabled, expires_at, created_at, updated_at, last_used_at, last_used_ip, last_used_user_agent FROM subscription_tokens`
	args := []any{}
	if customerID != "" {
		query += ` WHERE customer_id = $1`
		args = append(args, customerID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []domain.SubscriptionToken
	for rows.Next() {
		token, err := scanSubscriptionToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (s *SQLStore) GetSubscriptionToken(ctx context.Context, id string) (domain.SubscriptionToken, error) {
	token, err := scanSubscriptionToken(s.db.QueryRowContext(ctx, `SELECT id, customer_id, name, token_hash, enabled, expires_at, created_at, updated_at, last_used_at, last_used_ip, last_used_user_agent FROM subscription_tokens WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SubscriptionToken{}, ErrNotFound
	}
	return token, err
}

func (s *SQLStore) GetSubscriptionTokenByHash(ctx context.Context, tokenHash string) (domain.SubscriptionToken, error) {
	token, err := scanSubscriptionToken(s.db.QueryRowContext(ctx, `SELECT id, customer_id, name, token_hash, enabled, expires_at, created_at, updated_at, last_used_at, last_used_ip, last_used_user_agent FROM subscription_tokens WHERE token_hash = $1`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SubscriptionToken{}, ErrNotFound
	}
	return token, err
}

func (s *SQLStore) UpdateSubscriptionToken(ctx context.Context, token domain.SubscriptionToken) (domain.SubscriptionToken, error) {
	updated, err := scanSubscriptionToken(s.db.QueryRowContext(
		ctx,
		`UPDATE subscription_tokens
		 SET name = $2, enabled = $3, expires_at = $4, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, customer_id, name, token_hash, enabled, expires_at, created_at, updated_at, last_used_at, last_used_ip, last_used_user_agent`,
		token.ID, token.Name, token.Enabled, nullTime(token.ExpiresAt),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SubscriptionToken{}, ErrNotFound
	}
	return updated, err
}

func (s *SQLStore) RotateSubscriptionToken(ctx context.Context, id string, tokenHash string) (domain.SubscriptionToken, error) {
	updated, err := scanSubscriptionToken(s.db.QueryRowContext(
		ctx,
		`UPDATE subscription_tokens
		 SET token_hash = $2, updated_at = CURRENT_TIMESTAMP, last_used_at = NULL, last_used_ip = NULL, last_used_user_agent = NULL
		 WHERE id = $1
		 RETURNING id, customer_id, name, token_hash, enabled, expires_at, created_at, updated_at, last_used_at, last_used_ip, last_used_user_agent`,
		id, tokenHash,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SubscriptionToken{}, ErrNotFound
	}
	return updated, err
}

func (s *SQLStore) MarkSubscriptionUsed(ctx context.Context, id string, ip string, userAgent string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE subscription_tokens
		 SET last_used_at = CURRENT_TIMESTAMP, last_used_ip = $2, last_used_user_agent = $3
		 WHERE id = $1`,
		id, nullString(ip), nullString(userAgent),
	)
	return err
}

func (s *SQLStore) RecordTrafficUsage(ctx context.Context, usage domain.TrafficUsage) (domain.TrafficUsage, error) {
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
	return scanTrafficUsage(s.db.QueryRowContext(
		ctx,
		`INSERT INTO traffic_usage (id, proxy_account_id, proxy_node_id, upload_bytes, download_bytes, recorded_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, proxy_account_id, proxy_node_id, upload_bytes, download_bytes, recorded_at`,
		usage.ID, usage.ProxyAccountID, usage.ProxyNodeID, usage.UploadBytes, usage.DownloadBytes, usage.RecordedAt,
	))
}

func (s *SQLStore) WriteAudit(ctx context.Context, actor string, action string, metadata any) error {
	id, err := security.NewID()
	if err != nil {
		return err
	}
	metadataJSON := ""
	if metadata != nil {
		switch v := metadata.(type) {
		case string:
			metadataJSON = v
		default:
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			metadataJSON = string(data)
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_logs (id, actor, action, metadata_json) VALUES ($1, $2, $3, $4)`, id, nullString(actor), action, nullString(metadataJSON))
	return err
}

func (s *SQLStore) SubscriptionData(ctx context.Context, customerID string) (domain.Customer, []domain.ProxyAccount, error) {
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
	if node.Protocol == "" {
		node.Protocol = "vless"
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
}

func replaceAccountNodesTx(ctx context.Context, tx *sql.Tx, accountID string, nodeIDs []string) error {
	nodeIDs = uniqueStrings(nodeIDs)
	for _, nodeID := range nodeIDs {
		var exists string
		err := tx.QueryRowContext(ctx, `SELECT id FROM proxy_nodes WHERE id = $1`, nodeID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: proxy node %s", ErrNotFound, nodeID)
		}
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_account_nodes WHERE proxy_account_id = $1`, accountID); err != nil {
		return err
	}
	for _, nodeID := range nodeIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO proxy_account_nodes (proxy_account_id, proxy_node_id) VALUES ($1, $2)`, accountID, nodeID); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) loadAccountNodes(ctx context.Context, account *domain.ProxyAccount) error {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT n.id, n.name, n.hostname, n.public_host, n.region, n.protocol, n.port, n.transport, n.security,
		        n.sni, n.fingerprint, n.alpn, n.path, n.host_header, n.reality_public_key, n.reality_short_id,
		        n.enabled, n.created_at, n.updated_at
		 FROM proxy_nodes n
		 JOIN proxy_account_nodes an ON an.proxy_node_id = n.id
		 WHERE an.proxy_account_id = $1
		 ORDER BY n.name`,
		account.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	account.NodeIDs = nil
	account.Nodes = nil
	for rows.Next() {
		node, err := scanProxyNode(rows)
		if err != nil {
			return err
		}
		account.NodeIDs = append(account.NodeIDs, node.ID)
		account.Nodes = append(account.Nodes, node)
	}
	return rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCustomer(row scanner) (domain.Customer, error) {
	var customer domain.Customer
	var displayName sql.NullString
	var expiresAt sql.NullTime
	if err := row.Scan(&customer.ID, &customer.Email, &displayName, &customer.Status, &expiresAt, &customer.CreatedAt, &customer.UpdatedAt); err != nil {
		return domain.Customer{}, err
	}
	customer.DisplayName = stringFromNull(displayName)
	customer.ExpiresAt = timeFromNull(expiresAt)
	return customer, nil
}

func scanProxyNode(row scanner) (domain.ProxyNode, error) {
	var node domain.ProxyNode
	var publicHost, region, sni, fingerprint, alpn, path, hostHeader, realityPublicKey, realityShortID sql.NullString
	if err := row.Scan(
		&node.ID, &node.Name, &node.Hostname, &publicHost, &region, &node.Protocol, &node.Port, &node.Transport, &node.Security,
		&sni, &fingerprint, &alpn, &path, &hostHeader, &realityPublicKey, &realityShortID,
		&node.Enabled, &node.CreatedAt, &node.UpdatedAt,
	); err != nil {
		return domain.ProxyNode{}, err
	}
	node.PublicHost = stringFromNull(publicHost)
	node.Region = stringFromNull(region)
	node.SNI = stringFromNull(sni)
	node.Fingerprint = stringFromNull(fingerprint)
	node.ALPN = stringFromNull(alpn)
	node.Path = stringFromNull(path)
	node.HostHeader = stringFromNull(hostHeader)
	node.RealityPublicKey = stringFromNull(realityPublicKey)
	node.RealityShortID = stringFromNull(realityShortID)
	return node, nil
}

func scanProxyAccount(row scanner) (domain.ProxyAccount, error) {
	var account domain.ProxyAccount
	var flow sql.NullString
	var expiresAt sql.NullTime
	var trafficLimit sql.NullInt64
	if err := row.Scan(&account.ID, &account.CustomerID, &account.Protocol, &account.UUID, &account.EmailTag, &flow, &account.Enabled, &expiresAt, &trafficLimit, &account.CreatedAt, &account.UpdatedAt); err != nil {
		return domain.ProxyAccount{}, err
	}
	account.Flow = stringFromNull(flow)
	account.ExpiresAt = timeFromNull(expiresAt)
	account.TrafficLimitBytes = int64FromNull(trafficLimit)
	return account, nil
}

func scanSubscriptionToken(row scanner) (domain.SubscriptionToken, error) {
	var token domain.SubscriptionToken
	var expiresAt, lastUsedAt sql.NullTime
	var lastUsedIP, lastUsedUserAgent sql.NullString
	if err := row.Scan(&token.ID, &token.CustomerID, &token.Name, &token.TokenHash, &token.Enabled, &expiresAt, &token.CreatedAt, &token.UpdatedAt, &lastUsedAt, &lastUsedIP, &lastUsedUserAgent); err != nil {
		return domain.SubscriptionToken{}, err
	}
	token.ExpiresAt = timeFromNull(expiresAt)
	token.LastUsedAt = timeFromNull(lastUsedAt)
	token.LastUsedIP = stringFromNull(lastUsedIP)
	token.LastUsedUserAgent = stringFromNull(lastUsedUserAgent)
	return token, nil
}

func scanTrafficUsage(row scanner) (domain.TrafficUsage, error) {
	var usage domain.TrafficUsage
	if err := row.Scan(&usage.ID, &usage.ProxyAccountID, &usage.ProxyNodeID, &usage.UploadBytes, &usage.DownloadBytes, &usage.RecordedAt); err != nil {
		return domain.TrafficUsage{}, err
	}
	return usage, nil
}

func deleteByID(ctx context.Context, db *sql.DB, table string, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func stringFromNull(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

func timeFromNull(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func nullInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func int64FromNull(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
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
