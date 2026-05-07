package config

import "testing"

func TestValidateServerRejectsExampleSecrets(t *testing.T) {
	cfg := Config{
		AdminEmail:               "admin@example.com",
		AdminPassword:            "change-me-admin-password",
		SecretKey:                "change-me-with-openssl-rand-base64-32",
		AccessTokenExpireMinutes: 60,
	}
	if err := cfg.ValidateServer(); err == nil {
		t.Fatal("expected default config to be rejected")
	}
}

func TestValidateServerAcceptsStrongSecrets(t *testing.T) {
	cfg := Config{
		AdminEmail:               "admin@proxy.example",
		AdminPassword:            "correct-horse-battery-staple",
		SecretKey:                "0123456789abcdef0123456789abcdef",
		DatabaseEncryptionKey:    "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		AccessTokenExpireMinutes: 60,
	}
	if err := cfg.ValidateServer(); err != nil {
		t.Fatalf("ValidateServer() error = %v", err)
	}
}

func TestValidateServerRejectsInvalidDatabaseEncryptionKey(t *testing.T) {
	cfg := Config{
		AdminEmail:               "admin@proxy.example",
		AdminPassword:            "correct-horse-battery-staple",
		SecretKey:                "0123456789abcdef0123456789abcdef",
		DatabaseEncryptionKey:    "short",
		AccessTokenExpireMinutes: 60,
	}
	if err := cfg.ValidateServer(); err == nil {
		t.Fatal("ValidateServer accepted invalid database encryption key")
	}
}
