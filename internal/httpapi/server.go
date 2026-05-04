package httpapi

import (
	"errors"
	"net"
	"net/http"
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
	router.Use(gin.Logger(), gin.Recovery())

	server := &Server{
		cfg:   cfg,
		store: st,
	}
	server.routes(router)
	return router
}

func (s *Server) routes(router *gin.Engine) {
	router.GET("/health", s.health)
	router.POST("/admin/login", s.login)
	router.GET("/sub/:token", s.getSubscription)

	admin := router.Group("/admin", s.requireAdmin())
	admin.GET("/customers", s.listCustomers)
	admin.POST("/customers", s.createCustomer)
	admin.GET("/customers/:id", s.getCustomer)
	admin.PATCH("/customers/:id", s.updateCustomer)
	admin.DELETE("/customers/:id", s.deleteCustomer)

	admin.GET("/nodes", s.listNodes)
	admin.POST("/nodes", s.createNode)
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

func (s *Server) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Email != s.cfg.AdminEmail || !security.VerifyPassword(req.Password, s.cfg.AdminPassword) {
		writeError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := security.CreateAccessToken(req.Email, s.cfg.SecretKey, s.cfg.AccessTokenTTL())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create access token")
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": token, "token_type": "bearer"})
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

func (s *Server) createCustomer(c *gin.Context) {
	var req customerRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Email == "" {
		writeError(c, http.StatusBadRequest, "email is required")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	customer, err := s.store.CreateCustomer(c.Request.Context(), domain.Customer{
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Status:      req.Status,
		ExpiresAt:   req.ExpiresAt,
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
	var req customerPatch
	if !bindJSON(c, &req) {
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
	updated, err := s.store.UpdateCustomer(c.Request.Context(), current)
	if handleStoreError(c, err) {
		return
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

func (s *Server) createNode(c *gin.Context) {
	var req nodeRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == "" || req.Hostname == "" {
		writeError(c, http.StatusBadRequest, "name and hostname are required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	node, err := s.store.CreateProxyNode(c.Request.Context(), domain.ProxyNode{
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
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "proxy_node.created", node.ID)
	c.JSON(http.StatusCreated, node)
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
	var req nodePatch
	if !bindJSON(c, &req) {
		return
	}
	applyNodePatch(&current, req)
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
	var req proxyAccountPatch
	if !bindJSON(c, &req) {
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

type subscriptionTokenPatch struct {
	Name      *string    `json:"name"`
	Enabled   *bool      `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
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
	token, err := s.store.CreateSubscriptionToken(c.Request.Context(), domain.SubscriptionToken{
		CustomerID: req.CustomerID,
		Name:       req.Name,
		TokenHash:  security.TokenDigest(rawToken),
		Enabled:    true,
		ExpiresAt:  req.ExpiresAt,
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
	var req subscriptionTokenPatch
	if !bindJSON(c, &req) {
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
	token, err := s.store.RotateSubscriptionToken(c.Request.Context(), c.Param("id"), security.TokenDigest(rawToken))
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
	UploadBytes    int64     `json:"upload_bytes"`
	DownloadBytes  int64     `json:"download_bytes"`
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
	if req.UploadBytes < 0 || req.DownloadBytes < 0 {
		writeError(c, http.StatusBadRequest, "traffic bytes cannot be negative")
		return
	}
	usage, err := s.store.RecordTrafficUsage(c.Request.Context(), domain.TrafficUsage{
		ProxyAccountID: req.ProxyAccountID,
		ProxyNodeID:    req.ProxyNodeID,
		UploadBytes:    req.UploadBytes,
		DownloadBytes:  req.DownloadBytes,
		RecordedAt:     req.RecordedAt,
	})
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "traffic_usage.recorded", usage.ID)
	c.JSON(http.StatusCreated, usage)
}

func (s *Server) getSubscription(c *gin.Context) {
	fmtParam := c.DefaultQuery("fmt", "v2ray")
	if fmtParam != "v2ray" && fmtParam != "raw" {
		writeError(c, http.StatusBadRequest, "fmt must be v2ray or raw")
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

func (s *Server) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(c, http.StatusUnauthorized, "missing bearer token")
			c.Abort()
			return
		}
		subject, ok := security.VerifyAccessToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), s.cfg.SecretKey)
		if !ok {
			writeError(c, http.StatusUnauthorized, "invalid bearer token")
			c.Abort()
			return
		}
		c.Set("actor", subject)
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

func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		writeError(c, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
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
	writeError(c, http.StatusInternalServerError, "internal server error")
	return true
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
