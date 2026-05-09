package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ziyan-c/proxy-control-plane/internal/config"
	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
	"github.com/ziyan-c/proxy-control-plane/internal/store"
	"github.com/ziyan-c/proxy-control-plane/internal/subscription"
)

type Server struct {
	cfg   config.Config
	store *store.Store
}

func New(cfg config.Config, st *store.Store) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.LoggerWithFormatter(redactingGinLogFormatter), gin.Recovery())

	server := &Server{
		cfg:   cfg,
		store: st,
	}
	server.routes(router)
	return router
}

func (s *Server) routes(router *gin.Engine) {
	router.GET("/health", s.health)
	router.POST("/auth/refresh", s.refreshAuth)
	router.POST("/auth/logout", s.logoutAuth)
	router.POST("/admin/login", s.login)
	router.POST("/customer/login", s.customerLogin)
	router.GET("/sub/:token", s.getSubscription)

	admin := router.Group("/admin", s.requireAdmin())
	admin.GET("/customers", s.listCustomers)
	admin.POST("/customers", s.createCustomer)
	admin.GET("/customers/:id", s.getCustomer)
	admin.PATCH("/customers/:id", s.updateCustomer)
	admin.DELETE("/customers/:id", s.deleteCustomer)

	admin.GET("/nodes", s.listNodes)
	admin.POST("/nodes", s.createNode)
	admin.POST("/nodes/sync", s.syncNodes)
	admin.GET("/nodes/:id", s.getNode)
	admin.PATCH("/nodes/:id", s.updateNode)
	admin.DELETE("/nodes/:id", s.deleteNode)

	admin.GET("/proxy-accounts", s.listProxyAccounts)
	admin.POST("/proxy-accounts", s.createProxyAccount)
	admin.GET("/proxy-accounts/:id", s.getProxyAccount)
	admin.PATCH("/proxy-accounts/:id", s.updateProxyAccount)
	admin.DELETE("/proxy-accounts/:id", s.deleteProxyAccount)

	admin.GET("/subscription-tokens", s.listSubscriptionTokens)
	admin.POST("/subscription-tokens", s.createSubscriptionToken)
	admin.GET("/subscription-tokens/:id", s.getSubscriptionToken)
	admin.PATCH("/subscription-tokens/:id", s.updateSubscriptionToken)
	admin.POST("/subscription-tokens/:id/rotate", s.rotateSubscriptionToken)

	admin.POST("/traffic-usage", s.recordTrafficUsage)
	admin.GET("/domain-access-logs", s.listDomainAccessLogs)
	admin.POST("/domain-access-logs", s.recordDomainAccessLogs)
	admin.GET("/domain-access-summary", s.summarizeDomainAccessLogs)

	customer := router.Group("/customer", s.requireCustomer())
	customer.GET("/me", s.customerMe)
	customer.GET("/subscription-tokens", s.customerSubscriptionTokens)
}

func redactingGinLogFormatter(param gin.LogFormatterParams) string {
	param.Path = redactLogPath(param.Path)
	switch {
	case param.Latency > time.Minute:
		param.Latency = param.Latency.Truncate(time.Second * 10)
	case param.Latency > time.Second:
		param.Latency = param.Latency.Truncate(time.Millisecond * 10)
	case param.Latency > time.Millisecond:
		param.Latency = param.Latency.Truncate(time.Microsecond * 10)
	}

	return fmt.Sprintf("[GIN] %v | %3d | %8v | %15s | %-7s %#v\n%s",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		param.StatusCode,
		param.Latency,
		param.ClientIP,
		param.Method,
		param.Path,
		param.ErrorMessage,
	)
}

func redactLogPath(path string) string {
	rawQuery := ""
	if i := strings.Index(path, "?"); i >= 0 {
		rawQuery = path[i:]
		path = path[:i]
	}
	switch {
	case path == "/sub" || strings.HasPrefix(path, "/sub/"):
		return "/sub/<redacted>" + rawQuery
	default:
		return path + rawQuery
	}
}

func (s *Server) health(c *gin.Context) {
	if err := s.store.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type authPrincipalResponse struct {
	Type           string `json:"type"`
	Email          string `json:"email"`
	CustomerID     string `json:"customer_id,omitempty"`
	SessionVersion string `json:"-"`
}

type authResponse struct {
	AccessToken           string                `json:"access_token"`
	RefreshToken          string                `json:"refresh_token"`
	TokenType             string                `json:"token_type"`
	ExpiresIn             int64                 `json:"expires_in"`
	RefreshTokenExpiresAt time.Time             `json:"refresh_token_expires_at"`
	Principal             authPrincipalResponse `json:"principal"`
}

func (s *Server) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Email != s.cfg.AdminEmail || !security.VerifyPassword(req.Password, s.cfg.AdminPassword) {
		writeError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	resp, err := s.issueAuthTokens(c, security.PrincipalTypeAdmin, "", req.Email, s.adminSessionVersion())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create auth tokens")
		return
	}
	c.Set("actor", security.PrincipalTypeAdmin+":"+req.Email)
	s.audit(c, "auth.admin_login", req.Email)
	c.JSON(http.StatusOK, resp)
}

func (s *Server) customerLogin(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	customer, err := s.store.GetCustomerByEmail(c.Request.Context(), req.Email)
	if errors.Is(err, store.ErrNotFound) || err == nil && !customerCanLogin(customer, time.Now().UTC()) {
		writeError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if handleStoreError(c, err) {
		return
	}
	if customer.PasswordHash == "" || !security.VerifyPassword(req.Password, customer.PasswordHash) {
		writeError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	resp, err := s.issueAuthTokens(c, security.PrincipalTypeCustomer, customer.ID, customer.Email, s.customerSessionVersion(customer))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create auth tokens")
		return
	}
	c.Set("actor", security.PrincipalTypeCustomer+":"+customer.ID)
	s.audit(c, "auth.customer_login", customer.ID)
	c.JSON(http.StatusOK, resp)
}

func (s *Server) refreshAuth(c *gin.Context) {
	var req refreshRequest
	if !bindJSON(c, &req) {
		return
	}
	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		writeError(c, http.StatusBadRequest, "refresh_token is required")
		return
	}

	current, err := s.store.GetAuthRefreshTokenByHash(c.Request.Context(), security.TokenDigest(req.RefreshToken))
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if handleStoreError(c, err) {
		return
	}
	principal, ok := s.refreshPrincipal(c, current)
	if !ok {
		return
	}

	rawRefresh, err := security.NewRandomToken()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create refresh token")
		return
	}
	refreshExpiresAt := time.Now().UTC().Add(s.cfg.RefreshTokenTTL())
	next := domain.AuthRefreshToken{
		PrincipalType:     principal.Type,
		Subject:           authSubject(principal.Type, principal.CustomerID),
		SessionVersion:    principal.SessionVersion,
		TokenHash:         security.TokenDigest(rawRefresh),
		Enabled:           true,
		ExpiresAt:         refreshExpiresAt,
		LastUsedIP:        clientIP(c),
		LastUsedUserAgent: c.Request.UserAgent(),
	}
	if principal.CustomerID != "" {
		customerID := principal.CustomerID
		next.CustomerID = &customerID
	}
	next, err = s.store.RotateAuthRefreshToken(c.Request.Context(), current.ID, next, time.Now().UTC(), clientIP(c), c.Request.UserAgent())
	if errors.Is(err, store.ErrInvalid) {
		writeError(c, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if handleStoreError(c, err) {
		return
	}

	resp, err := s.authResponse(principal.Type, principal.CustomerID, principal.Email, rawRefresh, next.ExpiresAt)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create access token")
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) logoutAuth(c *gin.Context) {
	var req refreshRequest
	if !bindJSON(c, &req) {
		return
	}
	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		writeError(c, http.StatusBadRequest, "refresh_token is required")
		return
	}
	if err := s.store.RevokeAuthRefreshTokenByHash(c.Request.Context(), security.TokenDigest(req.RefreshToken), time.Now().UTC(), clientIP(c), c.Request.UserAgent()); handleStoreError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) issueAuthTokens(c *gin.Context, principalType string, customerID string, email string, sessionVersion string) (authResponse, error) {
	rawRefresh, err := security.NewRandomToken()
	if err != nil {
		return authResponse{}, err
	}
	refreshExpiresAt := time.Now().UTC().Add(s.cfg.RefreshTokenTTL())
	token := domain.AuthRefreshToken{
		PrincipalType:     principalType,
		Subject:           authSubject(principalType, customerID),
		SessionVersion:    sessionVersion,
		TokenHash:         security.TokenDigest(rawRefresh),
		Enabled:           true,
		ExpiresAt:         refreshExpiresAt,
		LastUsedIP:        clientIP(c),
		LastUsedUserAgent: c.Request.UserAgent(),
	}
	if customerID != "" {
		token.CustomerID = &customerID
	}
	token, err = s.store.CreateAuthRefreshToken(c.Request.Context(), token)
	if err != nil {
		return authResponse{}, err
	}
	return s.authResponse(principalType, customerID, email, rawRefresh, token.ExpiresAt)
}

func (s *Server) authResponse(principalType string, customerID string, email string, refreshToken string, refreshExpiresAt time.Time) (authResponse, error) {
	accessToken, err := security.CreateAccessToken(security.AccessClaims{
		Subject: authSubject(principalType, customerID),
		Role:    principalType,
	}, s.cfg.SecretKey, s.cfg.AccessTokenTTL())
	if err != nil {
		return authResponse{}, err
	}
	return authResponse{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		TokenType:             "bearer",
		ExpiresIn:             int64(s.cfg.AccessTokenTTL().Seconds()),
		RefreshTokenExpiresAt: refreshExpiresAt.UTC(),
		Principal: authPrincipalResponse{
			Type:       principalType,
			Email:      email,
			CustomerID: customerID,
		},
	}, nil
}

func (s *Server) refreshPrincipal(c *gin.Context, token domain.AuthRefreshToken) (authPrincipalResponse, bool) {
	now := time.Now().UTC()
	if !token.Enabled || token.RevokedAt != nil || !token.ExpiresAt.After(now) {
		writeError(c, http.StatusUnauthorized, "invalid refresh token")
		return authPrincipalResponse{}, false
	}
	switch token.PrincipalType {
	case security.PrincipalTypeAdmin:
		if token.Subject != security.PrincipalSubjectConfiguredAdmin || token.SessionVersion != s.adminSessionVersion() {
			writeError(c, http.StatusUnauthorized, "invalid refresh token")
			return authPrincipalResponse{}, false
		}
		return authPrincipalResponse{Type: security.PrincipalTypeAdmin, Email: s.cfg.AdminEmail, SessionVersion: token.SessionVersion}, true
	case security.PrincipalTypeCustomer:
		if token.CustomerID == nil || token.Subject != *token.CustomerID {
			writeError(c, http.StatusUnauthorized, "invalid refresh token")
			return authPrincipalResponse{}, false
		}
		customer, err := s.store.GetCustomer(c.Request.Context(), *token.CustomerID)
		if errors.Is(err, store.ErrNotFound) || err == nil && (!customerCanLogin(customer, now) || customer.PasswordHash == "") {
			writeError(c, http.StatusUnauthorized, "invalid refresh token")
			return authPrincipalResponse{}, false
		}
		if handleStoreError(c, err) {
			return authPrincipalResponse{}, false
		}
		sessionVersion := s.customerSessionVersion(customer)
		if token.SessionVersion != sessionVersion {
			writeError(c, http.StatusUnauthorized, "invalid refresh token")
			return authPrincipalResponse{}, false
		}
		return authPrincipalResponse{Type: security.PrincipalTypeCustomer, Email: customer.Email, CustomerID: customer.ID, SessionVersion: sessionVersion}, true
	default:
		writeError(c, http.StatusUnauthorized, "invalid refresh token")
		return authPrincipalResponse{}, false
	}
}

func customerCanLogin(customer domain.Customer, now time.Time) bool {
	return domain.CustomerStatusIsActive(customer.Status) && (customer.ExpiresAt == nil || customer.ExpiresAt.After(now))
}

func authSubject(principalType string, customerID string) string {
	if principalType == security.PrincipalTypeAdmin {
		return security.PrincipalSubjectConfiguredAdmin
	}
	return customerID
}

func (s *Server) adminSessionVersion() string {
	return security.AuthSessionVersion(s.cfg.SecretKey, "admin", s.cfg.AdminEmail, s.cfg.AdminPassword, s.cfg.AdminSessionEpoch)
}

func (s *Server) customerSessionVersion(customer domain.Customer) string {
	return security.AuthSessionVersion(s.cfg.SecretKey, "customer", customer.ID, customer.Email, customer.PasswordHash, customer.SessionEpoch)
}

type customerRequest struct {
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Password    string     `json:"password"`
}

func (s *Server) createCustomer(c *gin.Context) {
	var req customerRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Email == "" {
		writeError(c, http.StatusBadRequest, "email is required")
		return
	}
	passwordHash, ok := passwordHashFromRequest(c, req.Password, true)
	if !ok {
		return
	}
	customer, err := s.store.CreateCustomer(c.Request.Context(), domain.Customer{
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		Status:       domain.CustomerStatusOrDefault(req.Status),
		ExpiresAt:    req.ExpiresAt,
		PasswordHash: passwordHash,
	})
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "customer.created", customer.ID)
	c.JSON(http.StatusCreated, customer)
}

func (s *Server) listCustomers(c *gin.Context) {
	customers, err := s.store.ListCustomers(c.Request.Context())
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, customers)
}

func (s *Server) getCustomer(c *gin.Context) {
	customer, err := s.store.GetCustomer(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, customer)
}

func (s *Server) updateCustomer(c *gin.Context) {
	current, err := s.store.GetCustomer(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	fields, ok := bindJSONFields(c)
	if !ok {
		return
	}
	now := time.Now().UTC()
	oldLoginEnabled := customerCanLogin(current, now) && current.PasswordHash != ""
	_, emailChanged := fields["email"]
	_, passwordChanged := fields["password"]
	_, statusChanged := fields["status"]
	_, expiresAtChanged := fields["expires_at"]
	if !patchString(c, fields, "email", &current.Email, false, true) {
		return
	}
	if !patchString(c, fields, "display_name", &current.DisplayName, true, false) {
		return
	}
	if !patchString(c, fields, "status", &current.Status, false, true) {
		return
	}
	if !patchOptionalTime(c, fields, "expires_at", &current.ExpiresAt) {
		return
	}
	if !patchCustomerPassword(c, fields, &current) {
		return
	}
	resetSessions, ok := patchCustomerSessionReset(c, fields, &current)
	if !ok {
		return
	}
	updated, err := s.store.UpdateCustomer(c.Request.Context(), current)
	if handleStoreError(c, err) {
		return
	}
	newLoginEnabled := customerCanLogin(updated, now) && updated.PasswordHash != ""
	if emailChanged || passwordChanged || statusChanged || expiresAtChanged || resetSessions || (oldLoginEnabled && !newLoginEnabled) {
		if _, err := s.store.RevokeCustomerAuthRefreshTokens(c.Request.Context(), updated.ID, now, clientIP(c), c.Request.UserAgent()); handleStoreError(c, err) {
			return
		}
	}
	s.audit(c, "customer.updated", updated.ID)
	c.JSON(http.StatusOK, updated)
}

func (s *Server) deleteCustomer(c *gin.Context) {
	if handleStoreError(c, s.store.DeleteCustomer(c.Request.Context(), c.Param("id"))) {
		return
	}
	s.audit(c, "customer.deleted", c.Param("id"))
	c.Status(http.StatusNoContent)
}

func (s *Server) customerMe(c *gin.Context) {
	customerID, ok := customerIDFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "invalid bearer token")
		return
	}
	customer, err := s.store.GetCustomer(c.Request.Context(), customerID)
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, customer)
}

func (s *Server) customerSubscriptionTokens(c *gin.Context) {
	customerID, ok := customerIDFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "invalid bearer token")
		return
	}
	tokens, err := s.store.ListSubscriptionTokens(c.Request.Context(), customerID)
	if handleStoreError(c, err) {
		return
	}
	now := time.Now().UTC()
	active := make([]domain.SubscriptionToken, 0, len(tokens))
	for i := range tokens {
		if !activeSubscriptionToken(tokens[i], now) {
			continue
		}
		if tokens[i].EncryptedToken == "" {
			active = append(active, tokens[i])
			continue
		}
		plain, err := security.DecryptStringWithBase64Key(s.cfg.DatabaseEncryptionKey, tokens[i].EncryptedToken)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "could not decrypt subscription token")
			return
		}
		tokens[i].PlainToken = plain
		active = append(active, tokens[i])
	}
	c.JSON(http.StatusOK, active)
}

func activeSubscriptionToken(token domain.SubscriptionToken, now time.Time) bool {
	return token.Enabled && (token.ExpiresAt == nil || token.ExpiresAt.After(now))
}

type nodeRequest struct {
	Name              string `json:"name"`
	Hostname          string `json:"hostname"`
	PublicHost        string `json:"public_host"`
	Region            string `json:"region"`
	Runtime           string `json:"runtime"`
	Protocol          string `json:"protocol"`
	Port              int    `json:"port"`
	Transport         string `json:"transport"`
	Security          string `json:"security"`
	SNI               string `json:"sni"`
	Fingerprint       string `json:"fingerprint"`
	ALPN              string `json:"alpn"`
	Path              string `json:"path"`
	HostHeader        string `json:"host_header"`
	RealityPublicKey  string `json:"reality_public_key"`
	RealityShortID    string `json:"reality_short_id"`
	RuntimeAPIEnabled *bool  `json:"runtime_api_enabled"`
	RuntimeAPIHost    string `json:"runtime_api_host"`
	RuntimeAPIPort    int    `json:"runtime_api_port"`
	RuntimeInboundTag string `json:"runtime_inbound_tag"`
	Enabled           *bool  `json:"enabled"`
}

type nodesSyncRequest struct {
	Nodes []nodeRequest `json:"nodes"`
}

type nodeSyncResult struct {
	Name    string           `json:"name"`
	Action  string           `json:"action"`
	Node    domain.ProxyNode `json:"node"`
	Created bool             `json:"created"`
}

type nodesSyncResponse struct {
	Nodes []nodeSyncResult `json:"nodes"`
}

func (s *Server) createNode(c *gin.Context) {
	var req nodeRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == "" || req.Hostname == "" {
		writeError(c, http.StatusBadRequest, "name and hostname are required")
		return
	}
	if req.Port != 0 && !validPort(req.Port) {
		writeError(c, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if req.RuntimeAPIPort != 0 && !validPort(req.RuntimeAPIPort) {
		writeError(c, http.StatusBadRequest, "runtime_api_port must be between 1 and 65535")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	runtimeAPIEnabled := false
	if req.RuntimeAPIEnabled != nil {
		runtimeAPIEnabled = *req.RuntimeAPIEnabled
	}
	node, err := s.store.CreateProxyNode(c.Request.Context(), domain.ProxyNode{
		Name:              req.Name,
		Hostname:          req.Hostname,
		PublicHost:        req.PublicHost,
		Region:            req.Region,
		Runtime:           req.Runtime,
		Protocol:          req.Protocol,
		Port:              req.Port,
		Transport:         req.Transport,
		Security:          req.Security,
		SNI:               req.SNI,
		Fingerprint:       req.Fingerprint,
		ALPN:              req.ALPN,
		Path:              req.Path,
		HostHeader:        req.HostHeader,
		RealityPublicKey:  req.RealityPublicKey,
		RealityShortID:    req.RealityShortID,
		RuntimeAPIEnabled: runtimeAPIEnabled,
		RuntimeAPIHost:    req.RuntimeAPIHost,
		RuntimeAPIPort:    req.RuntimeAPIPort,
		RuntimeInboundTag: req.RuntimeInboundTag,
		Enabled:           enabled,
	})
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "proxy_node.created", node.ID)
	c.JSON(http.StatusCreated, node)
}

func (s *Server) syncNodes(c *gin.Context) {
	var req nodesSyncRequest
	if !bindJSON(c, &req) {
		return
	}
	if len(req.Nodes) == 0 {
		writeError(c, http.StatusBadRequest, "nodes is required")
		return
	}

	seen := map[string]struct{}{}
	inputs := make([]store.ProxyNodeUpsert, 0, len(req.Nodes))
	for _, item := range req.Nodes {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			writeError(c, http.StatusBadRequest, "nodes[].name is required")
			return
		}
		if _, exists := seen[name]; exists {
			writeError(c, http.StatusBadRequest, "nodes[].name must be unique")
			return
		}
		seen[name] = struct{}{}

		if item.Hostname == "" && item.PublicHost == "" {
			writeError(c, http.StatusBadRequest, "nodes[].hostname or public_host is required")
			return
		}
		runtime := strings.ToLower(strings.TrimSpace(item.Runtime))
		if runtime == "" {
			writeError(c, http.StatusBadRequest, "nodes[].runtime is required")
			return
		}
		if !validNodeRuntime(runtime) {
			writeError(c, http.StatusBadRequest, "nodes[].runtime must be custom or xray")
			return
		}
		if item.Hostname == "" {
			item.Hostname = item.PublicHost
		}
		if item.Port != 0 && !validPort(item.Port) {
			writeError(c, http.StatusBadRequest, "port must be between 1 and 65535")
			return
		}
		if item.RuntimeAPIPort != 0 && !validPort(item.RuntimeAPIPort) {
			writeError(c, http.StatusBadRequest, "runtime_api_port must be between 1 and 65535")
			return
		}

		enabled := false
		preserveEnabled := item.Enabled == nil
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		runtimeAPIEnabled := false
		if item.RuntimeAPIEnabled != nil {
			runtimeAPIEnabled = *item.RuntimeAPIEnabled
		}

		inputs = append(inputs, store.ProxyNodeUpsert{
			Node: domain.ProxyNode{
				Name:              name,
				Hostname:          item.Hostname,
				PublicHost:        item.PublicHost,
				Region:            item.Region,
				Runtime:           runtime,
				Protocol:          item.Protocol,
				Port:              item.Port,
				Transport:         item.Transport,
				Security:          item.Security,
				SNI:               item.SNI,
				Fingerprint:       item.Fingerprint,
				ALPN:              item.ALPN,
				Path:              item.Path,
				HostHeader:        item.HostHeader,
				RealityPublicKey:  item.RealityPublicKey,
				RealityShortID:    item.RealityShortID,
				RuntimeAPIEnabled: runtimeAPIEnabled,
				RuntimeAPIHost:    item.RuntimeAPIHost,
				RuntimeAPIPort:    item.RuntimeAPIPort,
				RuntimeInboundTag: item.RuntimeInboundTag,
				Enabled:           enabled,
			},
			PreserveEnabled: preserveEnabled,
		})
	}

	upsertResults, err := s.store.UpsertProxyNodesByName(c.Request.Context(), inputs)
	if handleStoreError(c, err) {
		return
	}

	results := make([]nodeSyncResult, 0, len(upsertResults))
	createdCount := 0
	updatedCount := 0
	for _, result := range upsertResults {
		action := "updated"
		if result.Created {
			action = "created"
			createdCount++
		} else {
			updatedCount++
		}
		results = append(results, nodeSyncResult{
			Name:    result.Node.Name,
			Action:  action,
			Node:    result.Node,
			Created: result.Created,
		})
	}

	s.audit(c, "proxy_node.synced", gin.H{
		"created": createdCount,
		"updated": updatedCount,
		"total":   len(results),
	})
	c.JSON(http.StatusOK, nodesSyncResponse{Nodes: results})
}

func (s *Server) listNodes(c *gin.Context) {
	nodes, err := s.store.ListProxyNodes(c.Request.Context())
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, nodes)
}

func (s *Server) getNode(c *gin.Context) {
	node, err := s.store.GetProxyNode(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, node)
}

func (s *Server) updateNode(c *gin.Context) {
	current, err := s.store.GetProxyNode(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	fields, ok := bindJSONFields(c)
	if !ok {
		return
	}
	if !applyNodePatch(c, &current, fields) {
		return
	}
	updated, err := s.store.UpdateProxyNode(c.Request.Context(), current)
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "proxy_node.updated", updated.ID)
	c.JSON(http.StatusOK, updated)
}

func (s *Server) deleteNode(c *gin.Context) {
	if handleStoreError(c, s.store.DeleteProxyNode(c.Request.Context(), c.Param("id"))) {
		return
	}
	s.audit(c, "proxy_node.deleted", c.Param("id"))
	c.Status(http.StatusNoContent)
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

func (s *Server) createProxyAccount(c *gin.Context) {
	var req proxyAccountRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.CustomerID == "" || req.EmailTag == "" {
		writeError(c, http.StatusBadRequest, "customer_id and email_tag are required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	account, err := s.store.CreateProxyAccount(c.Request.Context(), domain.ProxyAccount{
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
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "proxy_account.created", account.ID)
	c.JSON(http.StatusCreated, account)
}

func (s *Server) listProxyAccounts(c *gin.Context) {
	accounts, err := s.store.ListProxyAccounts(c.Request.Context())
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, accounts)
}

func (s *Server) getProxyAccount(c *gin.Context) {
	account, err := s.store.GetProxyAccount(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, account)
}

func (s *Server) updateProxyAccount(c *gin.Context) {
	current, err := s.store.GetProxyAccount(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	fields, ok := bindJSONFields(c)
	if !ok {
		return
	}
	if !patchString(c, fields, "customer_id", &current.CustomerID, false, true) {
		return
	}
	if !patchString(c, fields, "protocol", &current.Protocol, false, true) {
		return
	}
	if !patchString(c, fields, "uuid", &current.UUID, false, true) {
		return
	}
	if !patchString(c, fields, "email_tag", &current.EmailTag, false, true) {
		return
	}
	if !patchString(c, fields, "flow", &current.Flow, true, false) {
		return
	}
	if !patchBool(c, fields, "enabled", &current.Enabled) {
		return
	}
	if !patchOptionalTime(c, fields, "expires_at", &current.ExpiresAt) {
		return
	}
	if !patchOptionalInt64(c, fields, "traffic_limit_bytes", &current.TrafficLimitBytes) {
		return
	}
	if !patchStringSlice(c, fields, "node_ids", &current.NodeIDs) {
		return
	}
	updated, err := s.store.UpdateProxyAccount(c.Request.Context(), current)
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "proxy_account.updated", updated.ID)
	c.JSON(http.StatusOK, updated)
}

func (s *Server) deleteProxyAccount(c *gin.Context) {
	if handleStoreError(c, s.store.DeleteProxyAccount(c.Request.Context(), c.Param("id"))) {
		return
	}
	s.audit(c, "proxy_account.deleted", c.Param("id"))
	c.Status(http.StatusNoContent)
}

type subscriptionTokenRequest struct {
	CustomerID string     `json:"customer_id"`
	Name       string     `json:"name"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

func (s *Server) createSubscriptionToken(c *gin.Context) {
	var req subscriptionTokenRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.CustomerID == "" {
		writeError(c, http.StatusBadRequest, "customer_id is required")
		return
	}
	rawToken, err := security.NewRandomToken()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create subscription token")
		return
	}
	encryptedToken, err := security.EncryptStringWithBase64Key(s.cfg.DatabaseEncryptionKey, rawToken)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not encrypt subscription token")
		return
	}
	token, err := s.store.CreateSubscriptionToken(c.Request.Context(), domain.SubscriptionToken{
		CustomerID:     req.CustomerID,
		Name:           req.Name,
		TokenHash:      security.TokenDigest(rawToken),
		EncryptedToken: encryptedToken,
		Enabled:        true,
		ExpiresAt:      req.ExpiresAt,
	})
	if handleStoreError(c, err) {
		return
	}
	token.PlainToken = rawToken
	c.Header("X-Subscription-Token", rawToken)
	s.audit(c, "subscription_token.created", token.ID)
	c.JSON(http.StatusCreated, token)
}

func (s *Server) listSubscriptionTokens(c *gin.Context) {
	tokens, err := s.store.ListSubscriptionTokens(c.Request.Context(), c.Query("customer_id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, tokens)
}

func (s *Server) getSubscriptionToken(c *gin.Context) {
	token, err := s.store.GetSubscriptionToken(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, token)
}

func (s *Server) updateSubscriptionToken(c *gin.Context) {
	current, err := s.store.GetSubscriptionToken(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	fields, ok := bindJSONFields(c)
	if !ok {
		return
	}
	if !patchString(c, fields, "name", &current.Name, false, true) {
		return
	}
	if !patchBool(c, fields, "enabled", &current.Enabled) {
		return
	}
	if !patchOptionalTime(c, fields, "expires_at", &current.ExpiresAt) {
		return
	}
	updated, err := s.store.UpdateSubscriptionToken(c.Request.Context(), current)
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "subscription_token.updated", updated.ID)
	c.JSON(http.StatusOK, updated)
}

func (s *Server) rotateSubscriptionToken(c *gin.Context) {
	rawToken, err := security.NewRandomToken()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not rotate subscription token")
		return
	}
	encryptedToken, err := security.EncryptStringWithBase64Key(s.cfg.DatabaseEncryptionKey, rawToken)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not encrypt subscription token")
		return
	}
	token, err := s.store.RotateSubscriptionToken(c.Request.Context(), c.Param("id"), security.TokenDigest(rawToken), encryptedToken)
	if handleStoreError(c, err) {
		return
	}
	token.PlainToken = rawToken
	c.Header("X-Subscription-Token", rawToken)
	s.audit(c, "subscription_token.rotated", token.ID)
	c.JSON(http.StatusOK, token)
}

type trafficUsageRequest struct {
	ProxyAccountID string    `json:"proxy_account_id"`
	ProxyNodeID    string    `json:"proxy_node_id"`
	UploadBytes    *int64    `json:"upload_bytes"`
	DownloadBytes  *int64    `json:"download_bytes"`
	UploadGB       *float64  `json:"upload_gb"`
	DownloadGB     *float64  `json:"download_gb"`
	RecordedAt     time.Time `json:"recorded_at"`
}

func (s *Server) recordTrafficUsage(c *gin.Context) {
	var req trafficUsageRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.ProxyAccountID == "" || req.ProxyNodeID == "" {
		writeError(c, http.StatusBadRequest, "proxy_account_id and proxy_node_id are required")
		return
	}
	uploadBytes, err := bytesFromTrafficUnits(req.UploadBytes, req.UploadGB, "upload")
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	downloadBytes, err := bytesFromTrafficUnits(req.DownloadBytes, req.DownloadGB, "download")
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	usage, err := s.store.RecordTrafficUsage(c.Request.Context(), domain.TrafficUsage{
		ProxyAccountID: req.ProxyAccountID,
		ProxyNodeID:    req.ProxyNodeID,
		UploadBytes:    uploadBytes,
		DownloadBytes:  downloadBytes,
		RecordedAt:     req.RecordedAt,
	})
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "traffic_usage.recorded", usage.ID)
	c.JSON(http.StatusCreated, usage)
}

type domainAccessLogBatchRequest struct {
	Events []domainAccessLogEventRequest `json:"events"`

	ProxyAccountID string    `json:"proxy_account_id"`
	ProxyNodeID    string    `json:"proxy_node_id"`
	Domain         string    `json:"domain"`
	EventCount     int64     `json:"event_count"`
	UploadBytes    *int64    `json:"upload_bytes"`
	DownloadBytes  *int64    `json:"download_bytes"`
	UploadGB       *float64  `json:"upload_gb"`
	DownloadGB     *float64  `json:"download_gb"`
	AccessedAt     time.Time `json:"accessed_at"`
}

type domainAccessLogEventRequest struct {
	ProxyAccountID string    `json:"proxy_account_id"`
	ProxyNodeID    string    `json:"proxy_node_id"`
	Domain         string    `json:"domain"`
	EventCount     int64     `json:"event_count"`
	UploadBytes    *int64    `json:"upload_bytes"`
	DownloadBytes  *int64    `json:"download_bytes"`
	UploadGB       *float64  `json:"upload_gb"`
	DownloadGB     *float64  `json:"download_gb"`
	AccessedAt     time.Time `json:"accessed_at"`
}

func (s *Server) recordDomainAccessLogs(c *gin.Context) {
	var req domainAccessLogBatchRequest
	if !bindJSON(c, &req) {
		return
	}
	events := req.Events
	if len(events) == 0 {
		events = []domainAccessLogEventRequest{{
			ProxyAccountID: req.ProxyAccountID,
			ProxyNodeID:    req.ProxyNodeID,
			Domain:         req.Domain,
			EventCount:     req.EventCount,
			UploadBytes:    req.UploadBytes,
			DownloadBytes:  req.DownloadBytes,
			UploadGB:       req.UploadGB,
			DownloadGB:     req.DownloadGB,
			AccessedAt:     req.AccessedAt,
		}}
	}
	logs := make([]domain.DomainAccessLog, 0, len(events))
	for _, event := range events {
		log, ok := domainAccessLogFromRequest(c, event)
		if !ok {
			return
		}
		logs = append(logs, log)
	}
	inserted, err := s.store.RecordDomainAccessLogs(c.Request.Context(), logs)
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "domain_access_logs.recorded", gin.H{"rows": inserted})
	c.JSON(http.StatusCreated, gin.H{"inserted": inserted})
}

func domainAccessLogFromRequest(c *gin.Context, req domainAccessLogEventRequest) (domain.DomainAccessLog, bool) {
	if req.ProxyAccountID == "" || req.ProxyNodeID == "" {
		writeError(c, http.StatusBadRequest, "proxy_account_id and proxy_node_id are required")
		return domain.DomainAccessLog{}, false
	}
	domainName, ok := normalizeAccessDomain(req.Domain)
	if !ok {
		writeError(c, http.StatusBadRequest, "domain must be a hostname without path, query, or credentials")
		return domain.DomainAccessLog{}, false
	}
	eventCount := req.EventCount
	if eventCount == 0 {
		eventCount = 1
	}
	if eventCount < 0 {
		writeError(c, http.StatusBadRequest, "event_count cannot be negative")
		return domain.DomainAccessLog{}, false
	}
	uploadBytes, err := bytesFromTrafficUnits(req.UploadBytes, req.UploadGB, "upload")
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return domain.DomainAccessLog{}, false
	}
	downloadBytes, err := bytesFromTrafficUnits(req.DownloadBytes, req.DownloadGB, "download")
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return domain.DomainAccessLog{}, false
	}
	return domain.DomainAccessLog{
		ProxyAccountID: req.ProxyAccountID,
		ProxyNodeID:    req.ProxyNodeID,
		Domain:         domainName,
		EventCount:     eventCount,
		UploadBytes:    uploadBytes,
		DownloadBytes:  downloadBytes,
		AccessedAt:     req.AccessedAt,
	}, true
}

func (s *Server) listDomainAccessLogs(c *gin.Context) {
	filter, ok := domainAccessLogFilterFromQuery(c)
	if !ok {
		return
	}
	logs, err := s.store.ListDomainAccessLogs(c.Request.Context(), filter)
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, logs)
}

func (s *Server) summarizeDomainAccessLogs(c *gin.Context) {
	filter, ok := domainAccessLogFilterFromQuery(c)
	if !ok {
		return
	}
	rows, err := s.store.SummarizeDomainAccessLogs(c.Request.Context(), filter)
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, rows)
}

func domainAccessLogFilterFromQuery(c *gin.Context) (store.DomainAccessLogFilter, bool) {
	filter := store.DomainAccessLogFilter{
		ProxyAccountID: strings.TrimSpace(c.Query("proxy_account_id")),
		ProxyNodeID:    strings.TrimSpace(c.Query("proxy_node_id")),
	}
	if domainParam := strings.TrimSpace(c.Query("domain")); domainParam != "" {
		normalized, ok := normalizeAccessDomain(domainParam)
		if !ok {
			writeError(c, http.StatusBadRequest, "domain must be a hostname without path, query, or credentials")
			return store.DomainAccessLogFilter{}, false
		}
		filter.Domain = normalized
	}
	if sinceParam := strings.TrimSpace(c.Query("since")); sinceParam != "" {
		value, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			writeError(c, http.StatusBadRequest, "since must be RFC3339")
			return store.DomainAccessLogFilter{}, false
		}
		filter.Since = &value
	}
	if untilParam := strings.TrimSpace(c.Query("until")); untilParam != "" {
		value, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			writeError(c, http.StatusBadRequest, "until must be RFC3339")
			return store.DomainAccessLogFilter{}, false
		}
		filter.Until = &value
	}
	if limitParam := strings.TrimSpace(c.Query("limit")); limitParam != "" {
		value, err := strconv.Atoi(limitParam)
		if err != nil || value <= 0 {
			writeError(c, http.StatusBadRequest, "limit must be a positive integer")
			return store.DomainAccessLogFilter{}, false
		}
		filter.Limit = value
	}
	return filter, true
}

func (s *Server) getSubscription(c *gin.Context) {
	fmtParam, ok := subscriptionFormat(c)
	if !ok {
		return
	}

	token, err := s.store.GetSubscriptionTokenByHash(c.Request.Context(), security.TokenDigest(c.Param("token")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "subscription not found")
		return
	}
	if handleStoreError(c, err) {
		return
	}
	if !token.Enabled {
		writeError(c, http.StatusNotFound, "subscription not found")
		return
	}
	now := time.Now().UTC()
	if token.ExpiresAt != nil && token.ExpiresAt.Before(now) {
		writeError(c, http.StatusForbidden, "subscription expired")
		return
	}
	customer, accounts, err := s.store.SubscriptionData(c.Request.Context(), token.CustomerID)
	if handleStoreError(c, err) {
		return
	}
	body := subscription.Build(customer, accounts, fmtParam, now)
	_ = s.store.MarkSubscriptionUsed(c.Request.Context(), token.ID, clientIP(c), c.Request.UserAgent())
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func subscriptionFormat(c *gin.Context) (string, bool) {
	switch c.DefaultQuery("fmt", "base64") {
	case "base64", "v2ray":
		return "base64", true
	case "raw":
		return "raw", true
	default:
		writeError(c, http.StatusBadRequest, "fmt must be base64 or raw")
		return "", false
	}
}

func (s *Server) requireAdmin() gin.HandlerFunc {
	return s.requireRole(security.PrincipalTypeAdmin)
}

func (s *Server) requireCustomer() gin.HandlerFunc {
	return s.requireRole(security.PrincipalTypeCustomer)
}

func (s *Server) requireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(c, http.StatusUnauthorized, "missing bearer token")
			c.Abort()
			return
		}
		claims, ok := security.VerifyAccessToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), s.cfg.SecretKey)
		if !ok {
			writeError(c, http.StatusUnauthorized, "invalid bearer token")
			c.Abort()
			return
		}
		if claims.Role != role {
			writeError(c, http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		c.Set("auth_claims", claims)
		c.Set("actor", claims.Actor())
		c.Next()
	}
}

func (s *Server) audit(c *gin.Context, action string, metadata any) {
	_ = s.store.WriteAudit(c.Request.Context(), actorFromContext(c), action, metadata)
}

func actorFromContext(c *gin.Context) string {
	actor, _ := c.Get("actor")
	if value, ok := actor.(string); ok {
		return value
	}
	return ""
}

func claimsFromContext(c *gin.Context) (security.AccessClaims, bool) {
	value, exists := c.Get("auth_claims")
	if !exists {
		return security.AccessClaims{}, false
	}
	claims, ok := value.(security.AccessClaims)
	return claims, ok
}

func customerIDFromContext(c *gin.Context) (string, bool) {
	claims, ok := claimsFromContext(c)
	if !ok || claims.Role != security.PrincipalTypeCustomer || strings.TrimSpace(claims.Subject) == "" {
		return "", false
	}
	return claims.Subject, true
}

func applyNodePatch(c *gin.Context, node *domain.ProxyNode, fields map[string]json.RawMessage) bool {
	if !patchString(c, fields, "name", &node.Name, false, true) {
		return false
	}
	if !patchString(c, fields, "hostname", &node.Hostname, false, true) {
		return false
	}
	if !patchString(c, fields, "public_host", &node.PublicHost, true, false) {
		return false
	}
	if !patchString(c, fields, "region", &node.Region, true, false) {
		return false
	}
	if !patchString(c, fields, "runtime", &node.Runtime, false, true) {
		return false
	}
	if !patchString(c, fields, "protocol", &node.Protocol, false, true) {
		return false
	}
	if !patchInt(c, fields, "port", &node.Port, validPort, "port must be between 1 and 65535") {
		return false
	}
	if !patchString(c, fields, "transport", &node.Transport, false, true) {
		return false
	}
	if !patchString(c, fields, "security", &node.Security, false, true) {
		return false
	}
	if !patchString(c, fields, "sni", &node.SNI, true, false) {
		return false
	}
	if !patchString(c, fields, "fingerprint", &node.Fingerprint, true, false) {
		return false
	}
	if !patchString(c, fields, "alpn", &node.ALPN, true, false) {
		return false
	}
	if !patchString(c, fields, "path", &node.Path, true, false) {
		return false
	}
	if !patchString(c, fields, "host_header", &node.HostHeader, true, false) {
		return false
	}
	if !patchString(c, fields, "reality_public_key", &node.RealityPublicKey, true, false) {
		return false
	}
	if !patchString(c, fields, "reality_short_id", &node.RealityShortID, true, false) {
		return false
	}
	if !patchBool(c, fields, "runtime_api_enabled", &node.RuntimeAPIEnabled) {
		return false
	}
	if !patchString(c, fields, "runtime_api_host", &node.RuntimeAPIHost, true, false) {
		return false
	}
	if !patchInt(c, fields, "runtime_api_port", &node.RuntimeAPIPort, validRuntimeAPIPort, "runtime_api_port must be 0 or between 1 and 65535") {
		return false
	}
	if !patchString(c, fields, "runtime_inbound_tag", &node.RuntimeInboundTag, true, false) {
		return false
	}
	return patchBool(c, fields, "enabled", &node.Enabled)
}

func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		writeError(c, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func bindJSONFields(c *gin.Context) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		writeError(c, http.StatusBadRequest, "invalid json body")
		return nil, false
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	return fields, true
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func bytesFromTrafficUnits(byteValue *int64, gbValue *float64, direction string) (int64, error) {
	if byteValue != nil && gbValue != nil {
		return 0, fmt.Errorf("%s_bytes and %s_gb cannot both be set", direction, direction)
	}
	if byteValue != nil {
		if *byteValue < 0 {
			return 0, fmt.Errorf("%s_bytes cannot be negative", direction)
		}
		return *byteValue, nil
	}
	if gbValue == nil {
		return 0, nil
	}
	if math.IsNaN(*gbValue) || math.IsInf(*gbValue, 0) {
		return 0, fmt.Errorf("%s_gb must be finite", direction)
	}
	if *gbValue < 0 {
		return 0, fmt.Errorf("%s_gb cannot be negative", direction)
	}
	const bytesPerGB = float64(1000 * 1000 * 1000)
	const maxInt64 = float64(1<<63 - 1)
	if *gbValue > maxInt64/bytesPerGB {
		return 0, fmt.Errorf("%s_gb is too large", direction)
	}
	return int64(math.Round(*gbValue * bytesPerGB)), nil
}

func normalizeAccessDomain(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", false
	}
	if strings.Contains(value, "://") {
		return "", false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = strings.ToLower(strings.TrimSpace(host))
	}
	value = strings.TrimSuffix(value, ".")
	if value == "" || len(value) > 253 {
		return "", false
	}
	if strings.ContainsAny(value, " \t\r\n:/?#@*") {
		return "", false
	}
	return value, true
}

func handleStoreError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "not found")
		return true
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(c, http.StatusConflict, "already exists")
		return true
	}
	if errors.Is(err, store.ErrInvalid) {
		writeError(c, http.StatusBadRequest, "invalid request")
		return true
	}
	writeError(c, http.StatusInternalServerError, "internal server error")
	return true
}

func passwordHashFromRequest(c *gin.Context, password string, allowEmpty bool) (string, bool) {
	if password == "" {
		if allowEmpty {
			return "", true
		}
		writeError(c, http.StatusBadRequest, "password is required")
		return "", false
	}
	if len(password) < 8 {
		writeError(c, http.StatusBadRequest, "password must contain at least 8 characters")
		return "", false
	}
	hash, err := security.PasswordHash(password)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not hash password")
		return "", false
	}
	return hash, true
}

func patchCustomerPassword(c *gin.Context, fields map[string]json.RawMessage, customer *domain.Customer) bool {
	raw, exists := fields["password"]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		customer.PasswordHash = ""
		return true
	}
	var password string
	if !decodePatchValue(c, "password", raw, &password) {
		return false
	}
	hash, ok := passwordHashFromRequest(c, password, false)
	if !ok {
		return false
	}
	customer.PasswordHash = hash
	return true
}

func patchCustomerSessionReset(c *gin.Context, fields map[string]json.RawMessage, customer *domain.Customer) (bool, bool) {
	raw, exists := fields["reset_sessions"]
	if !exists {
		return false, true
	}
	if jsonNull(raw) {
		writeError(c, http.StatusBadRequest, "reset_sessions cannot be null")
		return false, false
	}
	var reset bool
	if !decodePatchValue(c, "reset_sessions", raw, &reset) {
		return false, false
	}
	if !reset {
		return false, true
	}
	epoch, err := security.NewRandomToken()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not reset sessions")
		return false, false
	}
	customer.SessionEpoch = epoch
	return true, true
}

func patchString(c *gin.Context, fields map[string]json.RawMessage, name string, target *string, nullable bool, nonEmpty bool) bool {
	raw, exists := fields[name]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		if !nullable {
			writeError(c, http.StatusBadRequest, name+" cannot be null")
			return false
		}
		*target = ""
		return true
	}
	var value string
	if !decodePatchValue(c, name, raw, &value) {
		return false
	}
	if nonEmpty && strings.TrimSpace(value) == "" {
		writeError(c, http.StatusBadRequest, name+" is required")
		return false
	}
	*target = value
	return true
}

func patchBool(c *gin.Context, fields map[string]json.RawMessage, name string, target *bool) bool {
	raw, exists := fields[name]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		writeError(c, http.StatusBadRequest, name+" cannot be null")
		return false
	}
	var value bool
	if !decodePatchValue(c, name, raw, &value) {
		return false
	}
	*target = value
	return true
}

func patchInt(c *gin.Context, fields map[string]json.RawMessage, name string, target *int, valid func(int) bool, message string) bool {
	raw, exists := fields[name]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		writeError(c, http.StatusBadRequest, name+" cannot be null")
		return false
	}
	var value int
	if !decodePatchValue(c, name, raw, &value) {
		return false
	}
	if !valid(value) {
		writeError(c, http.StatusBadRequest, message)
		return false
	}
	*target = value
	return true
}

func patchOptionalTime(c *gin.Context, fields map[string]json.RawMessage, name string, target **time.Time) bool {
	raw, exists := fields[name]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		*target = nil
		return true
	}
	var value time.Time
	if !decodePatchValue(c, name, raw, &value) {
		return false
	}
	*target = &value
	return true
}

func patchOptionalInt64(c *gin.Context, fields map[string]json.RawMessage, name string, target **int64) bool {
	raw, exists := fields[name]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		*target = nil
		return true
	}
	var value int64
	if !decodePatchValue(c, name, raw, &value) {
		return false
	}
	if value < 0 {
		writeError(c, http.StatusBadRequest, name+" cannot be negative")
		return false
	}
	*target = &value
	return true
}

func patchStringSlice(c *gin.Context, fields map[string]json.RawMessage, name string, target *[]string) bool {
	raw, exists := fields[name]
	if !exists {
		return true
	}
	if jsonNull(raw) {
		*target = []string{}
		return true
	}
	var value []string
	if !decodePatchValue(c, name, raw, &value) {
		return false
	}
	*target = value
	return true
}

func decodePatchValue(c *gin.Context, name string, raw json.RawMessage, target any) bool {
	if err := json.Unmarshal(raw, target); err != nil {
		writeError(c, http.StatusBadRequest, name+" has invalid value")
		return false
	}
	return true
}

func jsonNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validRuntimeAPIPort(port int) bool {
	return port == 0 || validPort(port)
}

func validNodeRuntime(runtime string) bool {
	switch runtime {
	case "custom", "xray":
		return true
	default:
		return false
	}
}

func clientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}
