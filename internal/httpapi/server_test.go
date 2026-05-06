package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ziyan-c/proxy-control-plane/internal/domain"
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
			name: "legacy subscription path",
			path: "/legacy-sub/v2ray/PUBLIC-29451172-2d7b-48e7-a43f-35e5b1e0199a",
			want: "/legacy-sub/<redacted>",
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
