package httpapi

import (
	"encoding/json"
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
	router.GET("/legacy-sub/*path", s.getLegacySubscription)

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

	admin.GET("/subscription-aliases", s.listSubscriptionAliases)
	admin.POST("/subscription-aliases", s.createSubscriptionAlias)
	admin.GET("/subscription-aliases/:id", s.getSubscriptionAlias)
	admin.PATCH("/subscription-aliases/:id", s.updateSubscriptionAlias)
	admin.DELETE("/subscription-aliases/:id", s.deleteSubscriptionAlias)

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
	fields, ok := bindJSONFields(c)
	if !ok {
		return
	}
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
	token, err := s.store.RotateSubscriptionToken(c.Request.Context(), c.Param("id"), security.TokenDigest(rawToken))
	if handleStoreError(c, err) {
		return
	}
	token.PlainToken = rawToken
	c.Header("X-Subscription-Token", rawToken)
	s.audit(c, "subscription_token.rotated", token.ID)
	c.JSON(http.StatusOK, token)
}

type subscriptionAliasRequest struct {
	CustomerID   string     `json:"customer_id"`
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	Enabled      *bool      `json:"enabled"`
	ExpiresAt    *time.Time `json:"expires_at"`
	SourcePath   string     `json:"source_path"`
	SourceSHA256 string     `json:"source_sha256"`
}

func (s *Server) createSubscriptionAlias(c *gin.Context) {
	var req subscriptionAliasRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.CustomerID == "" {
		writeError(c, http.StatusBadRequest, "customer_id is required")
		return
	}
	if req.Path == "" {
		writeError(c, http.StatusBadRequest, "path is required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	alias, err := s.store.CreateSubscriptionAlias(c.Request.Context(), domain.SubscriptionAlias{
		CustomerID:   req.CustomerID,
		Name:         req.Name,
		Path:         req.Path,
		Enabled:      enabled,
		ExpiresAt:    req.ExpiresAt,
		SourcePath:   req.SourcePath,
		SourceSHA256: req.SourceSHA256,
	})
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "subscription_alias.created", alias.ID)
	c.JSON(http.StatusCreated, alias)
}

func (s *Server) listSubscriptionAliases(c *gin.Context) {
	aliases, err := s.store.ListSubscriptionAliases(c.Request.Context(), c.Query("customer_id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, aliases)
}

func (s *Server) getSubscriptionAlias(c *gin.Context) {
	alias, err := s.store.GetSubscriptionAlias(c.Request.Context(), c.Param("id"))
	if handleStoreError(c, err) {
		return
	}
	c.JSON(http.StatusOK, alias)
}

func (s *Server) updateSubscriptionAlias(c *gin.Context) {
	current, err := s.store.GetSubscriptionAlias(c.Request.Context(), c.Param("id"))
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
	if !patchString(c, fields, "path", &current.Path, false, true) {
		return
	}
	if !patchBool(c, fields, "enabled", &current.Enabled) {
		return
	}
	if !patchOptionalTime(c, fields, "expires_at", &current.ExpiresAt) {
		return
	}
	if !patchString(c, fields, "source_path", &current.SourcePath, true, false) {
		return
	}
	if !patchString(c, fields, "source_sha256", &current.SourceSHA256, true, false) {
		return
	}
	updated, err := s.store.UpdateSubscriptionAlias(c.Request.Context(), current)
	if handleStoreError(c, err) {
		return
	}
	s.audit(c, "subscription_alias.updated", updated.ID)
	c.JSON(http.StatusOK, updated)
}

func (s *Server) deleteSubscriptionAlias(c *gin.Context) {
	if handleStoreError(c, s.store.DeleteSubscriptionAlias(c.Request.Context(), c.Param("id"))) {
		return
	}
	s.audit(c, "subscription_alias.deleted", c.Param("id"))
	c.Status(http.StatusNoContent)
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

func (s *Server) getLegacySubscription(c *gin.Context) {
	fmtParam, ok := subscriptionFormat(c)
	if !ok {
		return
	}

	pathHash, err := subscription.AliasDigest(c.Param("path"))
	if err != nil {
		writeError(c, http.StatusNotFound, "subscription not found")
		return
	}
	alias, err := s.store.GetSubscriptionAliasByPathHash(c.Request.Context(), pathHash)
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "subscription not found")
		return
	}
	if handleStoreError(c, err) {
		return
	}
	if !alias.Enabled {
		writeError(c, http.StatusNotFound, "subscription not found")
		return
	}
	now := time.Now().UTC()
	if alias.ExpiresAt != nil && alias.ExpiresAt.Before(now) {
		writeError(c, http.StatusForbidden, "subscription expired")
		return
	}
	customer, accounts, err := s.store.SubscriptionData(c.Request.Context(), alias.CustomerID)
	if handleStoreError(c, err) {
		return
	}
	body := subscription.Build(customer, accounts, fmtParam, now)
	_ = s.store.MarkSubscriptionAliasUsed(c.Request.Context(), alias.ID, clientIP(c), c.Request.UserAgent())
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
