package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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

func patchContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}
