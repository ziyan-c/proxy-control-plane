package store

import (
	"testing"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

func TestSetNodeDefaultsInfersRuntime(t *testing.T) {
	tests := []struct {
		name      string
		node      domain.ProxyNode
		runtime   string
		transport string
		security  string
		path      string
	}{
		{
			name: "xray websocket under caddy",
			node: domain.ProxyNode{
				Transport: "ws",
			},
			runtime:   "xray",
			transport: "ws",
			security:  "tls",
			path:      "/v2ray",
		},
		{
			name: "xray reality",
			node: domain.ProxyNode{
				Security: "reality",
			},
			runtime:   "xray",
			transport: "tcp",
			security:  "reality",
		},
		{
			name:      "generic custom",
			node:      domain.ProxyNode{},
			runtime:   "custom",
			transport: "tcp",
			security:  "none",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node := tc.node
			setNodeDefaults(&node)

			if node.Runtime != tc.runtime {
				t.Fatalf("runtime = %q, want %q", node.Runtime, tc.runtime)
			}
			if node.Transport != tc.transport {
				t.Fatalf("transport = %q, want %q", node.Transport, tc.transport)
			}
			if node.Security != tc.security {
				t.Fatalf("security = %q, want %q", node.Security, tc.security)
			}
			if node.Path != tc.path {
				t.Fatalf("path = %q, want %q", node.Path, tc.path)
			}
			if node.Protocol != "vless" {
				t.Fatalf("protocol = %q, want vless", node.Protocol)
			}
			if node.Port != 443 {
				t.Fatalf("port = %d, want 443", node.Port)
			}
		})
	}
}

func TestValidNodeRuntime(t *testing.T) {
	for _, runtime := range []string{"custom", "xray"} {
		if !validNodeRuntime(runtime) {
			t.Fatalf("runtime %q should be valid", runtime)
		}
	}
	if validNodeRuntime("v2ray") {
		t.Fatal("runtime v2ray should be invalid")
	}
}

func TestValidRuntimeAPIConfig(t *testing.T) {
	if !validRuntimeAPIConfig(domain.ProxyNode{}) {
		t.Fatal("disabled runtime API with empty fields should be valid")
	}
	if validRuntimeAPIConfig(domain.ProxyNode{
		RuntimeAPIEnabled: true,
		RuntimeAPIPort:    10085,
		RuntimeInboundTag: "proxy-control-plane-vless-in",
	}) {
		t.Fatal("enabled runtime API without host should be invalid")
	}
	if !validRuntimeAPIConfig(domain.ProxyNode{
		RuntimeAPIEnabled: true,
		RuntimeAPIHost:    "10.66.0.1",
		RuntimeAPIPort:    10085,
		RuntimeInboundTag: "proxy-control-plane-vless-in",
	}) {
		t.Fatal("complete runtime API config should be valid")
	}
}

func TestMatchLegacySubscriptionNode(t *testing.T) {
	nodes := []domain.ProxyNode{
		{
			ID:         "node-1",
			Name:       "under-caddy",
			PublicHost: "proxy.example.com",
			Protocol:   "vless",
			Port:       443,
			Transport:  "ws",
			Security:   "tls",
			Path:       "/v2ray",
		},
	}

	node, ok := matchLegacySubscriptionNode(nodes, LegacySubscriptionLinkInput{
		Host:      "proxy.example.com",
		Port:      443,
		Transport: "ws",
		Security:  "tls",
		Path:      "/v2ray",
	})
	if !ok {
		t.Fatal("matchLegacySubscriptionNode() did not match")
	}
	if node.ID != "node-1" {
		t.Fatalf("node.ID = %q, want node-1", node.ID)
	}
}
