package cli

import "testing"

func TestParseSavedSubscriptionToken(t *testing.T) {
	tokenID, token, err := parseSavedSubscriptionToken([]byte("customer_id=cust\ntoken_id=tok-1\nsubscription_path=https://example.com/sub/plain-token?fmt=raw\n"))
	if err != nil {
		t.Fatalf("parseSavedSubscriptionToken() error = %v", err)
	}
	if tokenID != "tok-1" {
		t.Fatalf("tokenID = %q, want tok-1", tokenID)
	}
	if token != "plain-token" {
		t.Fatalf("token = %q, want plain-token", token)
	}
}
