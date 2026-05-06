package runtimesync

import "testing"

func TestParseManagedTrafficStat(t *testing.T) {
	accountID, direction, ok := parseManagedTrafficStat("user>>>pcp-account-1@proxy-control-plane>>>traffic>>>uplink")
	if !ok {
		t.Fatal("parseManagedTrafficStat() did not match managed stat")
	}
	if accountID != "account-1" || direction != "uplink" {
		t.Fatalf("got accountID=%q direction=%q", accountID, direction)
	}
}

func TestParseManagedTrafficStatRejectsUnmanagedAndOnlineStats(t *testing.T) {
	tests := []string{
		"user>>>alice@example.com>>>traffic>>>uplink",
		"user>>>pcp-account-1@proxy-control-plane>>>online",
		"inbound>>>proxy-control-plane-vless-in>>>traffic>>>uplink",
	}
	for _, tc := range tests {
		if _, _, ok := parseManagedTrafficStat(tc); ok {
			t.Fatalf("parseManagedTrafficStat(%q) matched, want reject", tc)
		}
	}
}
