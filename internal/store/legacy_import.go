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

func shortValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[len(value)-8:]
}
