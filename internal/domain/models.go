package domain

import "time"

type Customer struct {
	ID          string     `json:"id" gorm:"primaryKey;type:text"`
	Email       string     `json:"email" gorm:"uniqueIndex;not null"`
	DisplayName string     `json:"display_name,omitempty"`
	Status      string     `json:"status" gorm:"index;not null"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	ProxyAccounts      []ProxyAccount      `json:"-" gorm:"foreignKey:CustomerID;constraint:OnDelete:CASCADE;"`
	SubscriptionTokens []SubscriptionToken `json:"-" gorm:"foreignKey:CustomerID;constraint:OnDelete:CASCADE;"`
}

type ProxyNode struct {
	ID               string    `json:"id" gorm:"primaryKey;type:text"`
	Name             string    `json:"name" gorm:"uniqueIndex;not null"`
	Hostname         string    `json:"hostname" gorm:"not null"`
	PublicHost       string    `json:"public_host,omitempty"`
	Region           string    `json:"region,omitempty"`
	Protocol         string    `json:"protocol" gorm:"not null"`
	Port             int       `json:"port" gorm:"not null"`
	Transport        string    `json:"transport" gorm:"not null"`
	Security         string    `json:"security" gorm:"not null"`
	SNI              string    `json:"sni,omitempty"`
	Fingerprint      string    `json:"fingerprint,omitempty"`
	ALPN             string    `json:"alpn,omitempty"`
	Path             string    `json:"path,omitempty"`
	HostHeader       string    `json:"host_header,omitempty"`
	RealityPublicKey string    `json:"reality_public_key,omitempty"`
	RealityShortID   string    `json:"reality_short_id,omitempty"`
	Enabled          bool      `json:"enabled" gorm:"index;not null"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	ProxyAccounts []ProxyAccount `json:"-" gorm:"many2many:proxy_account_nodes;constraint:OnDelete:CASCADE;"`
}

type ProxyAccount struct {
	ID                string      `json:"id" gorm:"primaryKey;type:text"`
	CustomerID        string      `json:"customer_id" gorm:"index;not null"`
	Protocol          string      `json:"protocol" gorm:"not null"`
	UUID              string      `json:"uuid" gorm:"uniqueIndex;not null"`
	EmailTag          string      `json:"email_tag" gorm:"not null"`
	Flow              string      `json:"flow,omitempty"`
	Enabled           bool        `json:"enabled" gorm:"index;not null"`
	ExpiresAt         *time.Time  `json:"expires_at,omitempty"`
	TrafficLimitBytes *int64      `json:"traffic_limit_bytes,omitempty"`
	NodeIDs           []string    `json:"node_ids,omitempty" gorm:"-"`
	Nodes             []ProxyNode `json:"nodes,omitempty" gorm:"many2many:proxy_account_nodes;constraint:OnDelete:CASCADE;"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`

	Customer Customer `json:"-" gorm:"foreignKey:CustomerID;constraint:OnDelete:CASCADE;"`
}

type SubscriptionToken struct {
	ID                string     `json:"id" gorm:"primaryKey;type:text"`
	CustomerID        string     `json:"customer_id" gorm:"index;not null"`
	Name              string     `json:"name" gorm:"not null"`
	TokenHash         string     `json:"-" gorm:"uniqueIndex;not null"`
	Enabled           bool       `json:"enabled" gorm:"index;not null"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP        string     `json:"last_used_ip,omitempty"`
	LastUsedUserAgent string     `json:"last_used_user_agent,omitempty"`
	PlainToken        string     `json:"plain_token,omitempty" gorm:"-"`

	Customer Customer `json:"-" gorm:"foreignKey:CustomerID;constraint:OnDelete:CASCADE;"`
}

type TrafficUsage struct {
	ID             string    `json:"id" gorm:"primaryKey;type:text"`
	ProxyAccountID string    `json:"proxy_account_id" gorm:"index:ix_traffic_usage_account_recorded,priority:1;not null"`
	ProxyNodeID    string    `json:"proxy_node_id" gorm:"index;not null"`
	UploadBytes    int64     `json:"upload_bytes" gorm:"not null"`
	DownloadBytes  int64     `json:"download_bytes" gorm:"not null"`
	RecordedAt     time.Time `json:"recorded_at" gorm:"index:ix_traffic_usage_account_recorded,priority:2;not null"`

	ProxyAccount ProxyAccount `json:"-" gorm:"foreignKey:ProxyAccountID;constraint:OnDelete:CASCADE;"`
	ProxyNode    ProxyNode    `json:"-" gorm:"foreignKey:ProxyNodeID;constraint:OnDelete:CASCADE;"`
}

type AuditLog struct {
	ID           string    `json:"id" gorm:"primaryKey;type:text"`
	Actor        string    `json:"actor,omitempty"`
	Action       string    `json:"action" gorm:"not null"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	CreatedAt    time.Time `json:"created_at" gorm:"index;not null"`
}

func (TrafficUsage) TableName() string {
	return "traffic_usage"
}

func (AuditLog) TableName() string {
	return "audit_logs"
}
