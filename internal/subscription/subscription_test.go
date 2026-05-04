package subscription

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/proxy-control-plane/internal/domain"
)

func TestBuildVLESSSubscription(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	customer := domain.Customer{
		ID:     "customer-1",
		Email:  "alice@example.com",
		Status: "active",
	}
	accounts := []domain.ProxyAccount{
		{
			ID:         "account-1",
			CustomerID: "customer-1",
			Protocol:   "vless",
			UUID:       "00000000-0000-4000-8000-000000000001",
			EmailTag:   "alice-phone",
			Flow:       "xtls-rprx-vision",
			Enabled:    true,
			Nodes: []domain.ProxyNode{
				{
					ID:               "node-1",
					Name:             "fr",
					Hostname:         "internal.example.com",
					PublicHost:       "proxy.example.com",
					Protocol:         "vless",
					Port:             443,
					Transport:        "tcp",
					Security:         "reality",
					SNI:              "www.example.com",
					Fingerprint:      "chrome",
					RealityPublicKey: "public-key",
					RealityShortID:   "abcd",
					Enabled:          true,
				},
			},
		},
	}

	raw := Build(customer, accounts, "raw", now)
	if !strings.Contains(raw, "vless://00000000-0000-4000-8000-000000000001@proxy.example.com:443") {
		t.Fatalf("raw subscription missing vless uri: %s", raw)
	}
	if !strings.Contains(raw, "security=reality") {
		t.Fatalf("raw subscription missing reality security: %s", raw)
	}
	if !strings.Contains(raw, "flow=xtls-rprx-vision") {
		t.Fatalf("raw subscription missing flow: %s", raw)
	}
	if !strings.Contains(raw, "fr-alice-phone") {
		t.Fatalf("raw subscription missing label: %s", raw)
	}

	encoded := Build(customer, accounts, "v2ray", now)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("encoded subscription is not base64: %v", err)
	}
	if string(decoded) != raw {
		t.Fatalf("decoded subscription = %q, want %q", string(decoded), raw)
	}
}

func TestBuildReturnsEmptyForInactiveCustomer(t *testing.T) {
	customer := domain.Customer{ID: "customer-1", Status: "disabled"}
	body := Build(customer, nil, "raw", time.Now().UTC())
	if body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}
