package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ziyan-c/proxy-control-plane/internal/config"
	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
)

func TestPatchHelpersClearNullableFields(t *testing.T) {
	c, _ := patchContext(`{"public_host":null,"expires_at":null}`)
	fields, ok := bindJSONFields(c)
	if !ok {
		t.Fatal("bindJSONFields failed")
	}

	publicHost := "proxy.example.com"
	if !patchString(c, fields, "public_host", &publicHost, true, false) {
		t.Fatal("patchString failed")
	}
	if publicHost != "" {
		t.Fatalf("publicHost = %q, want empty", publicHost)
	}

	expiresAt := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	expiresAtPtr := &expiresAt
	if !patchOptionalTime(c, fields, "expires_at", &expiresAtPtr) {
		t.Fatal("patchOptionalTime failed")
	}
	if expiresAtPtr != nil {
		t.Fatalf("expiresAtPtr = %v, want nil", expiresAtPtr)
	}
}

func TestPatchHelpersRejectInvalidPort(t *testing.T) {
	c, recorder := patchContext(`{"port":-1}`)
	fields, ok := bindJSONFields(c)
	if !ok {
		t.Fatal("bindJSONFields failed")
	}

	port := 443
	if patchInt(c, fields, "port", &port, validPort, "port must be between 1 and 65535") {
		t.Fatal("patchInt accepted invalid port")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestPatchHelpersRejectInvalidRuntimeAPIPort(t *testing.T) {
	c, recorder := patchContext(`{"runtime_api_port":70000}`)
	fields, ok := bindJSONFields(c)
	if !ok {
		t.Fatal("bindJSONFields failed")
	}

	port := 10085
	if patchInt(c, fields, "runtime_api_port", &port, validRuntimeAPIPort, "runtime_api_port must be 0 or between 1 and 65535") {
		t.Fatal("patchInt accepted invalid runtime API port")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestApplyNodePatchRejectsNullRuntime(t *testing.T) {
	c, recorder := patchContext(`{"runtime":null}`)
	fields, ok := bindJSONFields(c)
	if !ok {
		t.Fatal("bindJSONFields failed")
	}

	node := domain.ProxyNode{Runtime: "xray"}
	if applyNodePatch(c, &node, fields) {
		t.Fatal("applyNodePatch accepted null runtime")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestSyncNodesRequiresRuntime(t *testing.T) {
	c, recorder := requestContext(http.MethodPost, `{"nodes":[{"name":"node-1","hostname":"node.example.com"}]}`)

	server := &Server{}
	server.syncNodes(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "nodes[].runtime is required") {
		t.Fatalf("body = %s, want runtime error", recorder.Body.String())
	}
}

func TestSyncNodesRejectsInvalidRuntime(t *testing.T) {
	c, recorder := requestContext(http.MethodPost, `{"nodes":[{"name":"node-1","hostname":"node.example.com","runtime":"trojan"}]}`)

	server := &Server{}
	server.syncNodes(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "nodes[].runtime must be custom or xray") {
		t.Fatalf("body = %s, want runtime validation error", recorder.Body.String())
	}
}

func TestRedactLogPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "subscription token",
			path: "/sub/secret-token?fmt=raw",
			want: "/sub/<redacted>?fmt=raw",
		},
		{
			name: "admin path",
			path: "/admin/customers",
			want: "/admin/customers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactLogPath(tc.path); got != tc.want {
				t.Fatalf("redactLogPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequireAdminRejectsCustomerAccessToken(t *testing.T) {
	token, err := security.CreateAccessToken(security.AccessClaims{
		Subject: "customer-1",
		Role:    security.PrincipalTypeCustomer,
	}, "test-secret-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	server := &Server{cfg: config.Config{SecretKey: "test-secret-key"}}
	router.GET("/admin/test", server.requireAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestRequireCustomerAcceptsCustomerAccessToken(t *testing.T) {
	token, err := security.CreateAccessToken(security.AccessClaims{
		Subject: "customer-1",
		Role:    security.PrincipalTypeCustomer,
	}, "test-secret-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	server := &Server{cfg: config.Config{SecretKey: "test-secret-key"}}
	router.GET("/customer/me", server.requireCustomer(), func(c *gin.Context) {
		claims, ok := claimsFromContext(c)
		if !ok {
			t.Fatal("missing auth claims")
		}
		if claims.Subject != "customer-1" {
			t.Fatalf("subject = %q, want customer-1", claims.Subject)
		}
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/customer/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestActiveSubscriptionTokenRejectsDisabledOrExpired(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	tests := []struct {
		name  string
		token domain.SubscriptionToken
		want  bool
	}{
		{
			name:  "enabled without expiry",
			token: domain.SubscriptionToken{Enabled: true},
			want:  true,
		},
		{
			name:  "enabled future expiry",
			token: domain.SubscriptionToken{Enabled: true, ExpiresAt: &future},
			want:  true,
		},
		{
			name:  "disabled",
			token: domain.SubscriptionToken{Enabled: false, ExpiresAt: &future},
			want:  false,
		},
		{
			name:  "expired",
			token: domain.SubscriptionToken{Enabled: true, ExpiresAt: &past},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := activeSubscriptionToken(tc.token, now); got != tc.want {
				t.Fatalf("activeSubscriptionToken() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCustomerSessionVersionChangesWithEmailPasswordAndEpoch(t *testing.T) {
	server := &Server{cfg: config.Config{SecretKey: "test-secret-key"}}
	customer := domain.Customer{
		ID:           "customer-1",
		Email:        "old@example.com",
		PasswordHash: "hash-1",
		SessionEpoch: "epoch-1",
	}
	version := server.customerSessionVersion(customer)

	customer.Email = "new@example.com"
	if version == server.customerSessionVersion(customer) {
		t.Fatal("customer session version did not change after email update")
	}

	customer.Email = "old@example.com"
	customer.PasswordHash = "hash-2"
	if version == server.customerSessionVersion(customer) {
		t.Fatal("customer session version did not change after password update")
	}

	customer.PasswordHash = "hash-1"
	customer.SessionEpoch = "epoch-2"
	if version == server.customerSessionVersion(customer) {
		t.Fatal("customer session version did not change after epoch update")
	}
}

func TestBytesFromTrafficUnitsAcceptsGB(t *testing.T) {
	gb := 1.25
	bytes, err := bytesFromTrafficUnits(nil, &gb, "upload")
	if err != nil {
		t.Fatal(err)
	}
	if bytes != 1250*1000*1000 {
		t.Fatalf("bytes = %d, want %d", bytes, int64(1250*1000*1000))
	}
}

func TestBytesFromTrafficUnitsRejectsMixedUnits(t *testing.T) {
	bytesValue := int64(1)
	gb := 1.0
	if _, err := bytesFromTrafficUnits(&bytesValue, &gb, "upload"); err == nil {
		t.Fatal("bytesFromTrafficUnits accepted mixed byte and GB fields")
	}
}

func TestNormalizeAccessDomain(t *testing.T) {
	got, ok := normalizeAccessDomain("Example.COM:443")
	if !ok {
		t.Fatal("normalizeAccessDomain rejected host:port")
	}
	if got != "example.com" {
		t.Fatalf("domain = %q, want example.com", got)
	}

	if _, ok := normalizeAccessDomain("https://example.com/path"); ok {
		t.Fatal("normalizeAccessDomain accepted URL with path")
	}
}

func patchContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	return requestContext(http.MethodPatch, body)
}

func requestContext(method string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}
