package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/proxy-control-plane/internal/config"
	"github.com/ziyan/proxy-control-plane/internal/domain"
	"github.com/ziyan/proxy-control-plane/internal/security"
	"github.com/ziyan/proxy-control-plane/internal/store"
	"github.com/ziyan/proxy-control-plane/internal/subscription"
)

type Server struct {
	cfg   config.Config
	store *store.SQLStore
	mux   *http.ServeMux
}

type contextKey string

const actorKey contextKey = "actor"

func New(cfg config.Config, st *store.SQLStore) http.Handler {
	server := &Server{
		cfg:   cfg,
		store: st,
		mux:   http.NewServeMux(),
	}
	server.routes()
	return server.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("POST /admin/login", s.login)

	s.mux.Handle("GET /admin/customers", s.requireAdmin(http.HandlerFunc(s.listCustomers)))
	s.mux.Handle("POST /admin/customers", s.requireAdmin(http.HandlerFunc(s.createCustomer)))
	s.mux.Handle("GET /admin/customers/{id}", s.requireAdmin(http.HandlerFunc(s.getCustomer)))
	s.mux.Handle("PATCH /admin/customers/{id}", s.requireAdmin(http.HandlerFunc(s.updateCustomer)))
	s.mux.Handle("DELETE /admin/customers/{id}", s.requireAdmin(http.HandlerFunc(s.deleteCustomer)))

	s.mux.Handle("GET /admin/nodes", s.requireAdmin(http.HandlerFunc(s.listNodes)))
	s.mux.Handle("POST /admin/nodes", s.requireAdmin(http.HandlerFunc(s.createNode)))
	s.mux.Handle("GET /admin/nodes/{id}", s.requireAdmin(http.HandlerFunc(s.getNode)))
	s.mux.Handle("PATCH /admin/nodes/{id}", s.requireAdmin(http.HandlerFunc(s.updateNode)))
	s.mux.Handle("DELETE /admin/nodes/{id}", s.requireAdmin(http.HandlerFunc(s.deleteNode)))

	s.mux.Handle("GET /admin/proxy-accounts", s.requireAdmin(http.HandlerFunc(s.listProxyAccounts)))
	s.mux.Handle("POST /admin/proxy-accounts", s.requireAdmin(http.HandlerFunc(s.createProxyAccount)))
	s.mux.Handle("GET /admin/proxy-accounts/{id}", s.requireAdmin(http.HandlerFunc(s.getProxyAccount)))
	s.mux.Handle("PATCH /admin/proxy-accounts/{id}", s.requireAdmin(http.HandlerFunc(s.updateProxyAccount)))
	s.mux.Handle("DELETE /admin/proxy-accounts/{id}", s.requireAdmin(http.HandlerFunc(s.deleteProxyAccount)))

	s.mux.Handle("GET /admin/subscription-tokens", s.requireAdmin(http.HandlerFunc(s.listSubscriptionTokens)))
	s.mux.Handle("POST /admin/subscription-tokens", s.requireAdmin(http.HandlerFunc(s.createSubscriptionToken)))
	s.mux.Handle("GET /admin/subscription-tokens/{id}", s.requireAdmin(http.HandlerFunc(s.getSubscriptionToken)))
	s.mux.Handle("PATCH /admin/subscription-tokens/{id}", s.requireAdmin(http.HandlerFunc(s.updateSubscriptionToken)))
	s.mux.Handle("POST /admin/subscription-tokens/{id}/rotate", s.requireAdmin(http.HandlerFunc(s.rotateSubscriptionToken)))

	s.mux.Handle("POST /admin/traffic-usage", s.requireAdmin(http.HandlerFunc(s.recordTrafficUsage)))

	s.mux.HandleFunc("GET /sub/{token}", s.getSubscription)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email != s.cfg.AdminEmail || !security.VerifyPassword(req.Password, s.cfg.AdminPassword) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := security.CreateAccessToken(req.Email, s.cfg.SecretKey, s.cfg.AccessTokenTTL())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create access token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "bearer"})
}

type customerRequest struct {
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type customerPatch struct {
	Email       *string    `json:"email"`
	DisplayName *string    `json:"display_name"`
	Status      *string    `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

func (s *Server) createCustomer(w http.ResponseWriter, r *http.Request) {
	var req customerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	customer, err := s.store.CreateCustomer(r.Context(), domain.Customer{
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Status:      req.Status,
		ExpiresAt:   req.ExpiresAt,
	})
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "customer.created", customer.ID)
	writeJSON(w, http.StatusCreated, customer)
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	customers, err := s.store.ListCustomers(r.Context())
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, customers)
}

func (s *Server) getCustomer(w http.ResponseWriter, r *http.Request) {
	customer, err := s.store.GetCustomer(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, customer)
}

func (s *Server) updateCustomer(w http.ResponseWriter, r *http.Request) {
	current, err := s.store.GetCustomer(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	var req customerPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email != nil {
		current.Email = *req.Email
	}
	if req.DisplayName != nil {
		current.DisplayName = *req.DisplayName
	}
	if req.Status != nil {
		current.Status = *req.Status
	}
	if req.ExpiresAt != nil {
		current.ExpiresAt = req.ExpiresAt
	}
	updated, err := s.store.UpdateCustomer(r.Context(), current)
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "customer.updated", updated.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	if handleStoreError(w, s.store.DeleteCustomer(r.Context(), r.PathValue("id"))) {
		return
	}
	s.audit(r, "customer.deleted", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

type nodeRequest struct {
	Name             string `json:"name"`
	Hostname         string `json:"hostname"`
	PublicHost       string `json:"public_host"`
	Region           string `json:"region"`
	Protocol         string `json:"protocol"`
	Port             int    `json:"port"`
	Transport        string `json:"transport"`
	Security         string `json:"security"`
	SNI              string `json:"sni"`
	Fingerprint      string `json:"fingerprint"`
	ALPN             string `json:"alpn"`
	Path             string `json:"path"`
	HostHeader       string `json:"host_header"`
	RealityPublicKey string `json:"reality_public_key"`
	RealityShortID   string `json:"reality_short_id"`
	Enabled          *bool  `json:"enabled"`
}

type nodePatch = nodeRequest

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Hostname == "" {
		writeError(w, http.StatusBadRequest, "name and hostname are required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	node, err := s.store.CreateProxyNode(r.Context(), domain.ProxyNode{
		Name:             req.Name,
		Hostname:         req.Hostname,
		PublicHost:       req.PublicHost,
		Region:           req.Region,
		Protocol:         req.Protocol,
		Port:             req.Port,
		Transport:        req.Transport,
		Security:         req.Security,
		SNI:              req.SNI,
		Fingerprint:      req.Fingerprint,
		ALPN:             req.ALPN,
		Path:             req.Path,
		HostHeader:       req.HostHeader,
		RealityPublicKey: req.RealityPublicKey,
		RealityShortID:   req.RealityShortID,
		Enabled:          enabled,
	})
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "proxy_node.created", node.ID)
	writeJSON(w, http.StatusCreated, node)
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListProxyNodes(r.Context())
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	node, err := s.store.GetProxyNode(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	current, err := s.store.GetProxyNode(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	var req nodePatch
	if !decodeJSON(w, r, &req) {
		return
	}
	applyNodePatch(&current, req)
	updated, err := s.store.UpdateProxyNode(r.Context(), current)
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "proxy_node.updated", updated.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	if handleStoreError(w, s.store.DeleteProxyNode(r.Context(), r.PathValue("id"))) {
		return
	}
	s.audit(r, "proxy_node.deleted", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

type proxyAccountRequest struct {
	CustomerID        string     `json:"customer_id"`
	Protocol          string     `json:"protocol"`
	UUID              string     `json:"uuid"`
	EmailTag          string     `json:"email_tag"`
	Flow              string     `json:"flow"`
	Enabled           *bool      `json:"enabled"`
	ExpiresAt         *time.Time `json:"expires_at"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
	NodeIDs           []string   `json:"node_ids"`
}

type proxyAccountPatch struct {
	CustomerID        *string    `json:"customer_id"`
	Protocol          *string    `json:"protocol"`
	UUID              *string    `json:"uuid"`
	EmailTag          *string    `json:"email_tag"`
	Flow              *string    `json:"flow"`
	Enabled           *bool      `json:"enabled"`
	ExpiresAt         *time.Time `json:"expires_at"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
	NodeIDs           *[]string  `json:"node_ids"`
}

func (s *Server) createProxyAccount(w http.ResponseWriter, r *http.Request) {
	var req proxyAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.CustomerID == "" || req.EmailTag == "" {
		writeError(w, http.StatusBadRequest, "customer_id and email_tag are required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	account, err := s.store.CreateProxyAccount(r.Context(), domain.ProxyAccount{
		CustomerID:        req.CustomerID,
		Protocol:          req.Protocol,
		UUID:              req.UUID,
		EmailTag:          req.EmailTag,
		Flow:              req.Flow,
		Enabled:           enabled,
		ExpiresAt:         req.ExpiresAt,
		TrafficLimitBytes: req.TrafficLimitBytes,
		NodeIDs:           req.NodeIDs,
	})
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "proxy_account.created", account.ID)
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) listProxyAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListProxyAccounts(r.Context())
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) getProxyAccount(w http.ResponseWriter, r *http.Request) {
	account, err := s.store.GetProxyAccount(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) updateProxyAccount(w http.ResponseWriter, r *http.Request) {
	current, err := s.store.GetProxyAccount(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	var req proxyAccountPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.CustomerID != nil {
		current.CustomerID = *req.CustomerID
	}
	if req.Protocol != nil {
		current.Protocol = *req.Protocol
	}
	if req.UUID != nil {
		current.UUID = *req.UUID
	}
	if req.EmailTag != nil {
		current.EmailTag = *req.EmailTag
	}
	if req.Flow != nil {
		current.Flow = *req.Flow
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if req.ExpiresAt != nil {
		current.ExpiresAt = req.ExpiresAt
	}
	if req.TrafficLimitBytes != nil {
		current.TrafficLimitBytes = req.TrafficLimitBytes
	}
	if req.NodeIDs != nil {
		current.NodeIDs = *req.NodeIDs
	}
	updated, err := s.store.UpdateProxyAccount(r.Context(), current)
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "proxy_account.updated", updated.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteProxyAccount(w http.ResponseWriter, r *http.Request) {
	if handleStoreError(w, s.store.DeleteProxyAccount(r.Context(), r.PathValue("id"))) {
		return
	}
	s.audit(r, "proxy_account.deleted", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

type subscriptionTokenRequest struct {
	CustomerID string     `json:"customer_id"`
	Name       string     `json:"name"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

type subscriptionTokenPatch struct {
	Name      *string    `json:"name"`
	Enabled   *bool      `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (s *Server) createSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	var req subscriptionTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id is required")
		return
	}
	rawToken, err := security.NewRandomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create subscription token")
		return
	}
	token, err := s.store.CreateSubscriptionToken(r.Context(), domain.SubscriptionToken{
		CustomerID: req.CustomerID,
		Name:       req.Name,
		TokenHash:  security.TokenDigest(rawToken),
		Enabled:    true,
		ExpiresAt:  req.ExpiresAt,
	})
	if handleStoreError(w, err) {
		return
	}
	token.PlainToken = rawToken
	w.Header().Set("X-Subscription-Token", rawToken)
	s.audit(r, "subscription_token.created", token.ID)
	writeJSON(w, http.StatusCreated, token)
}

func (s *Server) listSubscriptionTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListSubscriptionTokens(r.Context(), r.URL.Query().Get("customer_id"))
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) getSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.store.GetSubscriptionToken(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) updateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	current, err := s.store.GetSubscriptionToken(r.Context(), r.PathValue("id"))
	if handleStoreError(w, err) {
		return
	}
	var req subscriptionTokenPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		current.Name = *req.Name
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if req.ExpiresAt != nil {
		current.ExpiresAt = req.ExpiresAt
	}
	updated, err := s.store.UpdateSubscriptionToken(r.Context(), current)
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "subscription_token.updated", updated.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) rotateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	rawToken, err := security.NewRandomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not rotate subscription token")
		return
	}
	token, err := s.store.RotateSubscriptionToken(r.Context(), r.PathValue("id"), security.TokenDigest(rawToken))
	if handleStoreError(w, err) {
		return
	}
	token.PlainToken = rawToken
	w.Header().Set("X-Subscription-Token", rawToken)
	s.audit(r, "subscription_token.rotated", token.ID)
	writeJSON(w, http.StatusOK, token)
}

type trafficUsageRequest struct {
	ProxyAccountID string    `json:"proxy_account_id"`
	ProxyNodeID    string    `json:"proxy_node_id"`
	UploadBytes    int64     `json:"upload_bytes"`
	DownloadBytes  int64     `json:"download_bytes"`
	RecordedAt     time.Time `json:"recorded_at"`
}

func (s *Server) recordTrafficUsage(w http.ResponseWriter, r *http.Request) {
	var req trafficUsageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ProxyAccountID == "" || req.ProxyNodeID == "" {
		writeError(w, http.StatusBadRequest, "proxy_account_id and proxy_node_id are required")
		return
	}
	if req.UploadBytes < 0 || req.DownloadBytes < 0 {
		writeError(w, http.StatusBadRequest, "traffic bytes cannot be negative")
		return
	}
	usage, err := s.store.RecordTrafficUsage(r.Context(), domain.TrafficUsage{
		ProxyAccountID: req.ProxyAccountID,
		ProxyNodeID:    req.ProxyNodeID,
		UploadBytes:    req.UploadBytes,
		DownloadBytes:  req.DownloadBytes,
		RecordedAt:     req.RecordedAt,
	})
	if handleStoreError(w, err) {
		return
	}
	s.audit(r, "traffic_usage.recorded", usage.ID)
	writeJSON(w, http.StatusCreated, usage)
}

func (s *Server) getSubscription(w http.ResponseWriter, r *http.Request) {
	fmtParam := r.URL.Query().Get("fmt")
	if fmtParam == "" {
		fmtParam = "v2ray"
	}
	if fmtParam != "v2ray" && fmtParam != "raw" {
		writeError(w, http.StatusBadRequest, "fmt must be v2ray or raw")
		return
	}

	token, err := s.store.GetSubscriptionTokenByHash(r.Context(), security.TokenDigest(r.PathValue("token")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "subscription not found")
		return
	}
	if handleStoreError(w, err) {
		return
	}
	if !token.Enabled {
		writeError(w, http.StatusNotFound, "subscription not found")
		return
	}
	now := time.Now().UTC()
	if token.ExpiresAt != nil && token.ExpiresAt.Before(now) {
		writeError(w, http.StatusForbidden, "subscription expired")
		return
	}
	customer, accounts, err := s.store.SubscriptionData(r.Context(), token.CustomerID)
	if handleStoreError(w, err) {
		return
	}
	body := subscription.Build(customer, accounts, fmtParam, now)
	_ = s.store.MarkSubscriptionUsed(r.Context(), token.ID, clientIP(r), r.UserAgent())
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		subject, ok := security.VerifyAccessToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), s.cfg.SecretKey)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey, subject)))
	})
}

func (s *Server) audit(r *http.Request, action string, metadata any) {
	_ = s.store.WriteAudit(r.Context(), actorFromContext(r.Context()), action, metadata)
}

func actorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey).(string)
	return actor
}

func applyNodePatch(node *domain.ProxyNode, req nodePatch) {
	if req.Name != "" {
		node.Name = req.Name
	}
	if req.Hostname != "" {
		node.Hostname = req.Hostname
	}
	if req.PublicHost != "" {
		node.PublicHost = req.PublicHost
	}
	if req.Region != "" {
		node.Region = req.Region
	}
	if req.Protocol != "" {
		node.Protocol = req.Protocol
	}
	if req.Port != 0 {
		node.Port = req.Port
	}
	if req.Transport != "" {
		node.Transport = req.Transport
	}
	if req.Security != "" {
		node.Security = req.Security
	}
	if req.SNI != "" {
		node.SNI = req.SNI
	}
	if req.Fingerprint != "" {
		node.Fingerprint = req.Fingerprint
	}
	if req.ALPN != "" {
		node.ALPN = req.ALPN
	}
	if req.Path != "" {
		node.Path = req.Path
	}
	if req.HostHeader != "" {
		node.HostHeader = req.HostHeader
	}
	if req.RealityPublicKey != "" {
		node.RealityPublicKey = req.RealityPublicKey
	}
	if req.RealityShortID != "" {
		node.RealityShortID = req.RealityShortID
	}
	if req.Enabled != nil {
		node.Enabled = *req.Enabled
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func handleStoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return true
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "already exists")
		return true
	}
	writeError(w, http.StatusInternalServerError, "internal server error")
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
