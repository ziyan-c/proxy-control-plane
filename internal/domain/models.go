package domain

import "time"

type Customer struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name,omitempty"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ProxyNode struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Hostname         string    `json:"hostname"`
	PublicHost       string    `json:"public_host,omitempty"`
	Region           string    `json:"region,omitempty"`
	Protocol         string    `json:"protocol"`
	Port             int       `json:"port"`
	Transport        string    `json:"transport"`
	Security         string    `json:"security"`
	SNI              string    `json:"sni,omitempty"`
	Fingerprint      string    `json:"fingerprint,omitempty"`
	ALPN             string    `json:"alpn,omitempty"`
	Path             string    `json:"path,omitempty"`
	HostHeader       string    `json:"host_header,omitempty"`
	RealityPublicKey string    `json:"reality_public_key,omitempty"`
	RealityShortID   string    `json:"reality_short_id,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProxyAccount struct {
	ID                string      `json:"id"`
	CustomerID        string      `json:"customer_id"`
	Protocol          string      `json:"protocol"`
	UUID              string      `json:"uuid"`
	EmailTag          string      `json:"email_tag"`
	Flow              string      `json:"flow,omitempty"`
	Enabled           bool        `json:"enabled"`
	ExpiresAt         *time.Time  `json:"expires_at,omitempty"`
	TrafficLimitBytes *int64      `json:"traffic_limit_bytes,omitempty"`
	NodeIDs           []string    `json:"node_ids,omitempty"`
	Nodes             []ProxyNode `json:"nodes,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

type SubscriptionToken struct {
	ID                string     `json:"id"`
	CustomerID        string     `json:"customer_id"`
	Name              string     `json:"name"`
	TokenHash         string     `json:"-"`
	Enabled           bool       `json:"enabled"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP        string     `json:"last_used_ip,omitempty"`
	LastUsedUserAgent string     `json:"last_used_user_agent,omitempty"`
	PlainToken        string     `json:"plain_token,omitempty"`
}

type TrafficUsage struct {
	ID             string    `json:"id"`
	ProxyAccountID string    `json:"proxy_account_id"`
	ProxyNodeID    string    `json:"proxy_node_id"`
	UploadBytes    int64     `json:"upload_bytes"`
	DownloadBytes  int64     `json:"download_bytes"`
	RecordedAt     time.Time `json:"recorded_at"`
}

type AuditLog struct {
	ID           string    `json:"id"`
	Actor        string    `json:"actor,omitempty"`
	Action       string    `json:"action"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
