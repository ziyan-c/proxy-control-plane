package subscription

import (
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

func Build(customer domain.Customer, accounts []domain.ProxyAccount, fmt string, now time.Time) string {
	if customer.Status != "active" || !isActive(customer.ExpiresAt, now) {
		return ""
	}

	lines := make([]string, 0)
	for _, account := range accounts {
		if account.CustomerID != customer.ID || account.Protocol != "vless" || !account.Enabled || !isActive(account.ExpiresAt, now) {
			continue
		}
		for _, node := range account.Nodes {
			if !node.Enabled || node.Protocol != "vless" {
				continue
			}
			lines = append(lines, vlessURI(account, node))
		}
	}

	raw := strings.Join(lines, "\n")
	if fmt == "raw" {
		return raw
	}
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func isActive(expiresAt *time.Time, now time.Time) bool {
	return expiresAt == nil || !expiresAt.Before(now)
}

func vlessURI(account domain.ProxyAccount, node domain.ProxyNode) string {
	host := node.PublicHost
	if host == "" {
		host = node.Hostname
	}

	params := url.Values{}
	params.Set("encryption", "none")
	params.Set("type", valueOr(node.Transport, "tcp"))
	params.Set("security", valueOr(node.Security, "none"))
	setIfNotEmpty(params, "flow", account.Flow)
	setIfNotEmpty(params, "sni", node.SNI)
	setIfNotEmpty(params, "fp", node.Fingerprint)
	setIfNotEmpty(params, "alpn", node.ALPN)
	setIfNotEmpty(params, "path", node.Path)
	setIfNotEmpty(params, "host", node.HostHeader)
	setIfNotEmpty(params, "pbk", node.RealityPublicKey)
	setIfNotEmpty(params, "sid", node.RealityShortID)

	label := url.QueryEscape(node.Name + "-" + account.EmailTag)
	return "vless://" + account.UUID + "@" + host + ":" + intString(node.Port) + "?" + params.Encode() + "#" + label
}

func setIfNotEmpty(params url.Values, key string, value string) {
	if value != "" {
		params.Set(key, value)
	}
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func intString(value int) string {
	if value <= 0 {
		return "443"
	}
	var buf [20]byte
	i := len(buf)
	n := value
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
