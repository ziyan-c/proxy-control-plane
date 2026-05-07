package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
	"gorm.io/gorm"
)

type LegacyProxyAccountInput struct {
	UUID     string
	EmailTag string
	Flow     string
}

type LegacyProxyAccountImport struct {
	CustomerEmail       string
	CustomerDisplayName string
	NodeName            string
	Accounts            []LegacyProxyAccountInput
}

type LegacyProxyAccountImportResult struct {
	CustomerCreated int
	AccountCreated  int
	AccountExisting int
	NodeBindings    int
	Skipped         int
}

type LegacySubscriptionLinkInput struct {
	UUID             string
	EmailTag         string
	Flow             string
	Host             string
	Port             int
	Transport        string
	Security         string
	Path             string
	HostHeader       string
	RealityPublicKey string
	RealityShortID   string
}

type LegacySubscriptionFileImport struct {
	CustomerEmail         string
	CustomerDisplayName   string
	EmailTagPrefix        string
	SubscriptionToken     bool
	SubscriptionTokenName string
	DatabaseEncryptionKey string
	Links                 []LegacySubscriptionLinkInput
}

type LegacySubscriptionFileImportResult struct {
	CustomerCreated          int
	SubscriptionTokenCreated int
	PlainSubscriptionToken   string
	AccountCreated           int
	AccountExisting          int
	NodeBindings             int
	UnmatchedLinks           int
	Skipped                  int
}

func (s *Store) ImportLegacyProxyAccounts(ctx context.Context, input LegacyProxyAccountImport) (LegacyProxyAccountImportResult, error) {
	var result LegacyProxyAccountImportResult
	input.CustomerEmail = strings.TrimSpace(input.CustomerEmail)
	input.CustomerDisplayName = strings.TrimSpace(input.CustomerDisplayName)
	input.NodeName = strings.TrimSpace(input.NodeName)
	if input.CustomerEmail == "" || input.NodeName == "" {
		return result, ErrInvalid
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		customer, createdCustomer, err := ensureCustomerByEmailTx(tx, input.CustomerEmail, input.CustomerDisplayName)
		if err != nil {
			return err
		}
		if createdCustomer {
			result.CustomerCreated = 1
		}

		var node domain.ProxyNode
		if err := tx.First(&node, "name = ?", input.NodeName).Error; err != nil {
			return mapGormError(err)
		}

		seen := map[string]struct{}{}
		for _, accountInput := range input.Accounts {
			accountInput.UUID = strings.TrimSpace(accountInput.UUID)
			accountInput.EmailTag = strings.TrimSpace(accountInput.EmailTag)
			accountInput.Flow = strings.TrimSpace(accountInput.Flow)
			if accountInput.UUID == "" {
				result.Skipped++
				continue
			}
			if _, exists := seen[accountInput.UUID]; exists {
				result.Skipped++
				continue
			}
			seen[accountInput.UUID] = struct{}{}
			if accountInput.EmailTag == "" {
				accountInput.EmailTag = "legacy-public-" + shortValue(accountInput.UUID)
			}

			account, createdAccount, err := upsertLegacyProxyAccountByUUIDTx(tx, customer.ID, accountInput)
			if err != nil {
				return err
			}
			if createdAccount {
				result.AccountCreated++
			} else {
				result.AccountExisting++
			}

			bound, err := bindProxyAccountNodeTx(tx, account.ID, node.ID)
			if err != nil {
				return err
			}
			if bound {
				result.NodeBindings++
			}
		}
		return nil
	})
	return result, err
}

func (s *Store) ImportLegacySubscriptionFile(ctx context.Context, input LegacySubscriptionFileImport) (LegacySubscriptionFileImportResult, error) {
	var result LegacySubscriptionFileImportResult
	input.CustomerEmail = strings.TrimSpace(input.CustomerEmail)
	input.CustomerDisplayName = strings.TrimSpace(input.CustomerDisplayName)
	input.EmailTagPrefix = strings.TrimSpace(input.EmailTagPrefix)
	input.SubscriptionTokenName = strings.TrimSpace(input.SubscriptionTokenName)
	if input.CustomerEmail == "" {
		return result, ErrInvalid
	}
	if input.EmailTagPrefix == "" {
		input.EmailTagPrefix = "legacy-public"
	}
	if input.SubscriptionTokenName == "" {
		input.SubscriptionTokenName = "legacy-public"
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		customer, createdCustomer, err := ensureCustomerByEmailTx(tx, input.CustomerEmail, input.CustomerDisplayName)
		if err != nil {
			return err
		}
		if createdCustomer {
			result.CustomerCreated = 1
		}

		if input.SubscriptionToken {
			createdToken, plainToken, err := ensureSubscriptionTokenTx(tx, customer.ID, input.SubscriptionTokenName, input.DatabaseEncryptionKey)
			if err != nil {
				return err
			}
			if createdToken {
				result.SubscriptionTokenCreated = 1
				result.PlainSubscriptionToken = plainToken
			}
		}

		var nodes []domain.ProxyNode
		if err := tx.Order("name ASC").Find(&nodes).Error; err != nil {
			return mapGormError(err)
		}

		seenLinks := map[string]struct{}{}
		for _, link := range input.Links {
			link.UUID = strings.TrimSpace(link.UUID)
			link.EmailTag = strings.TrimSpace(link.EmailTag)
			link.Flow = strings.TrimSpace(link.Flow)
			link.Host = strings.TrimSpace(link.Host)
			link.Transport = strings.TrimSpace(link.Transport)
			link.Security = strings.TrimSpace(link.Security)
			link.Path = strings.TrimSpace(link.Path)
			link.HostHeader = strings.TrimSpace(link.HostHeader)
			link.RealityPublicKey = strings.TrimSpace(link.RealityPublicKey)
			link.RealityShortID = strings.TrimSpace(link.RealityShortID)
			if link.UUID == "" {
				result.Skipped++
				continue
			}
			key := legacySubscriptionLinkKey(link)
			if _, exists := seenLinks[key]; exists {
				result.Skipped++
				continue
			}
			seenLinks[key] = struct{}{}
			if link.EmailTag == "" {
				link.EmailTag = input.EmailTagPrefix + "-" + shortValue(link.UUID)
			}

			account, createdAccount, err := upsertLegacyProxyAccountByUUIDTx(tx, customer.ID, LegacyProxyAccountInput{
				UUID:     link.UUID,
				EmailTag: link.EmailTag,
				Flow:     link.Flow,
			})
			if err != nil {
				return err
			}
			if createdAccount {
				result.AccountCreated++
			} else {
				result.AccountExisting++
			}

			node, ok := matchLegacySubscriptionNode(nodes, link)
			if !ok {
				result.UnmatchedLinks++
				continue
			}
			bound, err := bindProxyAccountNodeTx(tx, account.ID, node.ID)
			if err != nil {
				return err
			}
			if bound {
				result.NodeBindings++
			}
		}
		return nil
	})
	return result, err
}

func ensureCustomerByEmailTx(tx *gorm.DB, email string, displayName string) (domain.Customer, bool, error) {
	var customer domain.Customer
	err := tx.First(&customer, "email = ?", email).Error
	if err == nil {
		return customer, false, nil
	}
	mapped := mapGormError(err)
	if !errors.Is(mapped, ErrNotFound) {
		return domain.Customer{}, false, mapped
	}

	id, err := security.NewID()
	if err != nil {
		return domain.Customer{}, false, err
	}
	if displayName == "" {
		displayName = "Legacy public accounts"
	}
	customer = domain.Customer{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		Status:      "active",
	}
	if err := tx.Omit("ProxyAccounts", "SubscriptionTokens").Create(&customer).Error; err != nil {
		return domain.Customer{}, false, mapGormError(err)
	}
	return customer, true, nil
}

func upsertLegacyProxyAccountByUUIDTx(tx *gorm.DB, customerID string, input LegacyProxyAccountInput) (domain.ProxyAccount, bool, error) {
	var account domain.ProxyAccount
	err := tx.First(&account, "uuid = ?", input.UUID).Error
	if err == nil {
		if account.CustomerID != customerID {
			return domain.ProxyAccount{}, false, fmt.Errorf("%w: existing proxy account UUID ending %s belongs to another customer", ErrInvalid, shortValue(input.UUID))
		}
		if strings.TrimSpace(account.Flow) != strings.TrimSpace(input.Flow) {
			return domain.ProxyAccount{}, false, fmt.Errorf("%w: existing proxy account flow does not match imported Xray client flow for UUID ending %s", ErrInvalid, shortValue(input.UUID))
		}
		return account, false, nil
	}
	mapped := mapGormError(err)
	if !errors.Is(mapped, ErrNotFound) {
		return domain.ProxyAccount{}, false, mapped
	}

	id, err := security.NewID()
	if err != nil {
		return domain.ProxyAccount{}, false, err
	}
	account = domain.ProxyAccount{
		ID:         id,
		CustomerID: customerID,
		Protocol:   "vless",
		UUID:       input.UUID,
		EmailTag:   input.EmailTag,
		Flow:       input.Flow,
		Enabled:    true,
	}
	if err := tx.Omit("Nodes", "Customer").Create(&account).Error; err != nil {
		return domain.ProxyAccount{}, false, mapGormError(err)
	}
	return account, true, nil
}

func bindProxyAccountNodeTx(tx *gorm.DB, accountID string, nodeID string) (bool, error) {
	result := tx.Exec(
		"INSERT INTO proxy_account_nodes (proxy_account_id, proxy_node_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
		accountID,
		nodeID,
	)
	if err := mapGormError(result.Error); err != nil {
		return false, err
	}
	return result.RowsAffected > 0, nil
}

func ensureSubscriptionTokenTx(tx *gorm.DB, customerID string, name string, databaseEncryptionKey string) (bool, string, error) {
	var count int64
	if err := tx.Model(&domain.SubscriptionToken{}).Where("customer_id = ?", customerID).Count(&count).Error; err != nil {
		return false, "", mapGormError(err)
	}
	if count > 0 {
		return false, "", nil
	}

	id, err := security.NewID()
	if err != nil {
		return false, "", err
	}
	plainToken, err := security.NewRandomToken()
	if err != nil {
		return false, "", err
	}
	encryptedToken, err := security.EncryptStringWithBase64Key(databaseEncryptionKey, plainToken)
	if err != nil {
		return false, "", err
	}
	token := domain.SubscriptionToken{
		ID:             id,
		CustomerID:     customerID,
		Name:           name,
		TokenHash:      security.TokenDigest(plainToken),
		EncryptedToken: encryptedToken,
		Enabled:        true,
	}
	if err := tx.Omit("Customer").Create(&token).Error; err != nil {
		return false, "", mapGormError(err)
	}
	return true, plainToken, nil
}

func legacySubscriptionLinkKey(link LegacySubscriptionLinkInput) string {
	parts := []string{
		strings.TrimSpace(link.UUID),
		strings.ToLower(strings.TrimSpace(link.Host)),
		fmt.Sprintf("%d", link.Port),
		strings.ToLower(strings.TrimSpace(link.Transport)),
		strings.ToLower(strings.TrimSpace(link.Security)),
		strings.TrimSpace(link.Path),
	}
	return strings.Join(parts, "|")
}

func matchLegacySubscriptionNode(nodes []domain.ProxyNode, link LegacySubscriptionLinkInput) (domain.ProxyNode, bool) {
	bestScore := -1
	var best domain.ProxyNode
	for _, node := range nodes {
		if !strings.EqualFold(node.Protocol, "vless") {
			continue
		}
		if !matchesNodeHost(node, link.Host) {
			continue
		}
		if link.Port > 0 && node.Port != link.Port {
			continue
		}
		score := 10
		if link.Transport != "" {
			if !strings.EqualFold(node.Transport, link.Transport) {
				continue
			}
			score += 3
		}
		if link.Security != "" {
			if !strings.EqualFold(node.Security, link.Security) {
				continue
			}
			score += 3
		}
		if link.Path != "" {
			if node.Path != link.Path {
				continue
			}
			score += 3
		}
		if link.HostHeader != "" && node.HostHeader == link.HostHeader {
			score++
		}
		if link.RealityPublicKey != "" {
			if node.RealityPublicKey != link.RealityPublicKey {
				continue
			}
			score += 3
		}
		if link.RealityShortID != "" && node.RealityShortID == link.RealityShortID {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = node
		}
	}
	return best, bestScore >= 0
}

func matchesNodeHost(node domain.ProxyNode, host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(node.PublicHost, host) || strings.EqualFold(node.Hostname, host) {
		return true
	}
	return false
}

func shortValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[len(value)-8:]
}
