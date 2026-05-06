package subscription

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
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

func TestParseVLESSLinksAcceptsBase64Subscription(t *testing.T) {
	raw := "vless://00000000-0000-4000-8000-000000000001@example.com:443?encryption=none&type=ws&security=tls&path=%2Fv2ray&host=example.com#legacy"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw + "\n"))

	result, err := ParseLinks([]byte(encoded))
	if err != nil {
		t.Fatalf("ParseLinks() error = %v", err)
	}
	links := result.VLESSLinks
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	link := links[0]
	if link.UUID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("UUID = %q", link.UUID)
	}
	if link.Transport != "ws" || link.Security != "tls" || link.Path != "/v2ray" || link.HostHeader != "example.com" {
		t.Fatalf("parsed link = %+v", link)
	}
	if link.EmailTag != "legacy" {
		t.Fatalf("EmailTag = %q, want legacy", link.EmailTag)
	}
}

func TestParseLinksCountsUnsupportedURIs(t *testing.T) {
	raw := strings.Join([]string{
		"trojan://password@example.com:443",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?encryption=none",
	}, "\n")

	result, err := ParseLinks([]byte(raw))
	if err != nil {
		t.Fatalf("ParseLinks() error = %v", err)
	}
	if len(result.VLESSLinks) != 1 {
		t.Fatalf("len(VLESSLinks) = %d, want 1", len(result.VLESSLinks))
	}
	if result.UnsupportedURIs != 1 {
		t.Fatalf("UnsupportedURIs = %d, want 1", result.UnsupportedURIs)
	}
}

func TestCanonicalAliasPath(t *testing.T) {
	path, err := CanonicalAliasPath("https://example.com/public/vless.txt?x=1")
	if err != nil {
		t.Fatalf("CanonicalAliasPath() error = %v", err)
	}
	if path != "/public/vless.txt" {
		t.Fatalf("path = %q, want /public/vless.txt", path)
	}
	if _, err := CanonicalAliasPath("/"); err == nil {
		t.Fatal("CanonicalAliasPath(/) succeeded, want error")
	}
}
