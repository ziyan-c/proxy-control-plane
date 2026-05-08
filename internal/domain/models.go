package domain

import (
	"encoding/json"
	"time"
)

const bytesPerGB = 1000 * 1000 * 1000

func BytesToGB(bytes int64) float64 {
	return float64(bytes) / bytesPerGB
}

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
	ID                   string     `json:"id" gorm:"primaryKey;type:text"`
	Name                 string     `json:"name" gorm:"uniqueIndex;not null"`
	Hostname             string     `json:"hostname" gorm:"not null"`
	PublicHost           string     `json:"public_host,omitempty"`
	Region               string     `json:"region,omitempty"`
	Runtime              string     `json:"runtime" gorm:"index;not null"`
	Protocol             string     `json:"protocol" gorm:"not null"`
	Port                 int        `json:"port" gorm:"not null"`
	Transport            string     `json:"transport" gorm:"not null"`
	Security             string     `json:"security" gorm:"not null"`
	SNI                  string     `json:"sni,omitempty"`
	Fingerprint          string     `json:"fingerprint,omitempty"`
	ALPN                 string     `json:"alpn,omitempty"`
	Path                 string     `json:"path,omitempty"`
	HostHeader           string     `json:"host_header,omitempty"`
	RealityPublicKey     string     `json:"reality_public_key,omitempty"`
	RealityShortID       string     `json:"reality_short_id,omitempty"`
	RuntimeAPIEnabled    bool       `json:"runtime_api_enabled" gorm:"index;not null;default:false"`
	RuntimeAPIHost       string     `json:"runtime_api_host,omitempty"`
	RuntimeAPIPort       int        `json:"runtime_api_port,omitempty" gorm:"not null;default:0"`
	RuntimeInboundTag    string     `json:"runtime_inbound_tag,omitempty"`
	LastRuntimeSyncAt    *time.Time `json:"last_runtime_sync_at,omitempty"`
	LastRuntimeSyncError string     `json:"last_runtime_sync_error,omitempty"`
	Enabled              bool       `json:"enabled" gorm:"index;not null"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

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
	EncryptedToken    string     `json:"-" gorm:"column:encrypted_token"`
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

func (t TrafficUsage) MarshalJSON() ([]byte, error) {
	type Alias TrafficUsage
	return json.Marshal(struct {
		Alias
		TotalBytes int64   `json:"total_bytes"`
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
	}{
		Alias:      Alias(t),
		TotalBytes: t.UploadBytes + t.DownloadBytes,
		UploadGB:   BytesToGB(t.UploadBytes),
		DownloadGB: BytesToGB(t.DownloadBytes),
		TotalGB:    BytesToGB(t.UploadBytes + t.DownloadBytes),
	})
}

type TrafficUsageDaily struct {
	ProxyAccountID string    `json:"proxy_account_id" gorm:"primaryKey;type:text;not null"`
	ProxyNodeID    string    `json:"proxy_node_id" gorm:"primaryKey;type:text;not null"`
	Day            time.Time `json:"day" gorm:"primaryKey;type:date;not null"`
	UploadBytes    int64     `json:"upload_bytes" gorm:"not null"`
	DownloadBytes  int64     `json:"download_bytes" gorm:"not null"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	ProxyAccount ProxyAccount `json:"-" gorm:"foreignKey:ProxyAccountID;constraint:OnDelete:CASCADE;"`
	ProxyNode    ProxyNode    `json:"-" gorm:"foreignKey:ProxyNodeID;constraint:OnDelete:CASCADE;"`
}

func (t TrafficUsageDaily) MarshalJSON() ([]byte, error) {
	type Alias TrafficUsageDaily
	return json.Marshal(struct {
		Alias
		TotalBytes int64   `json:"total_bytes"`
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
	}{
		Alias:      Alias(t),
		TotalBytes: t.UploadBytes + t.DownloadBytes,
		UploadGB:   BytesToGB(t.UploadBytes),
		DownloadGB: BytesToGB(t.DownloadBytes),
		TotalGB:    BytesToGB(t.UploadBytes + t.DownloadBytes),
	})
}

type RuntimeUser struct {
	ProxyAccountID string `json:"proxy_account_id,omitempty"`
	Email          string `json:"email"`
	UUID           string `json:"uuid"`
	Flow           string `json:"flow,omitempty"`
}

type TrafficDelta struct {
	ProxyAccountID string `json:"proxy_account_id"`
	UploadBytes    int64  `json:"upload_bytes"`
	DownloadBytes  int64  `json:"download_bytes"`
}

func (t TrafficDelta) MarshalJSON() ([]byte, error) {
	type Alias TrafficDelta
	return json.Marshal(struct {
		Alias
		TotalBytes int64   `json:"total_bytes"`
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
	}{
		Alias:      Alias(t),
		TotalBytes: t.UploadBytes + t.DownloadBytes,
		UploadGB:   BytesToGB(t.UploadBytes),
		DownloadGB: BytesToGB(t.DownloadBytes),
		TotalGB:    BytesToGB(t.UploadBytes + t.DownloadBytes),
	})
}

type DomainAccessLog struct {
	ID             string    `json:"id" gorm:"primaryKey;type:text"`
	ProxyAccountID string    `json:"proxy_account_id" gorm:"index:idx_domain_access_logs_account_accessed,priority:1;not null"`
	ProxyNodeID    string    `json:"proxy_node_id" gorm:"index:idx_domain_access_logs_node_accessed,priority:1;not null"`
	Domain         string    `json:"domain" gorm:"index:idx_domain_access_logs_domain_accessed,priority:1;not null"`
	EventCount     int64     `json:"event_count" gorm:"not null;default:1"`
	UploadBytes    int64     `json:"upload_bytes" gorm:"not null;default:0"`
	DownloadBytes  int64     `json:"download_bytes" gorm:"not null;default:0"`
	AccessedAt     time.Time `json:"accessed_at" gorm:"index;index:idx_domain_access_logs_account_accessed,priority:2;index:idx_domain_access_logs_node_accessed,priority:2;index:idx_domain_access_logs_domain_accessed,priority:2;not null"`
	CreatedAt      time.Time `json:"created_at"`

	ProxyAccount ProxyAccount `json:"-" gorm:"foreignKey:ProxyAccountID;constraint:OnDelete:CASCADE;"`
	ProxyNode    ProxyNode    `json:"-" gorm:"foreignKey:ProxyNodeID;constraint:OnDelete:CASCADE;"`
}

func (d DomainAccessLog) MarshalJSON() ([]byte, error) {
	type Alias DomainAccessLog
	return json.Marshal(struct {
		Alias
		TotalBytes int64   `json:"total_bytes"`
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
	}{
		Alias:      Alias(d),
		TotalBytes: d.UploadBytes + d.DownloadBytes,
		UploadGB:   BytesToGB(d.UploadBytes),
		DownloadGB: BytesToGB(d.DownloadBytes),
		TotalGB:    BytesToGB(d.UploadBytes + d.DownloadBytes),
	})
}

type DomainAccessSummary struct {
	ProxyAccountID  string    `json:"proxy_account_id"`
	ProxyNodeID     string    `json:"proxy_node_id"`
	Domain          string    `json:"domain"`
	EventCount      int64     `json:"event_count"`
	UploadBytes     int64     `json:"upload_bytes"`
	DownloadBytes   int64     `json:"download_bytes"`
	FirstAccessedAt time.Time `json:"first_accessed_at"`
	LastAccessedAt  time.Time `json:"last_accessed_at"`
}

func (d DomainAccessSummary) MarshalJSON() ([]byte, error) {
	type Alias DomainAccessSummary
	return json.Marshal(struct {
		Alias
		TotalBytes int64   `json:"total_bytes"`
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
	}{
		Alias:      Alias(d),
		TotalBytes: d.UploadBytes + d.DownloadBytes,
		UploadGB:   BytesToGB(d.UploadBytes),
		DownloadGB: BytesToGB(d.DownloadBytes),
		TotalGB:    BytesToGB(d.UploadBytes + d.DownloadBytes),
	})
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

func (TrafficUsageDaily) TableName() string {
	return "traffic_usage_daily"
}

func (DomainAccessLog) TableName() string {
	return "domain_access_logs"
}

func (AuditLog) TableName() string {
	return "audit_logs"
}

const runtimeUserEmailSuffix = "@proxy-control-plane"

func RuntimeProxyAccountEmail(proxyAccountID string) string {
	if proxyAccountID == "" {
		return ""
	}
	return "pcp-" + proxyAccountID + runtimeUserEmailSuffix
}

func ProxyAccountIDFromRuntimeEmail(email string) (string, bool) {
	const prefix = "pcp-"
	if len(email) <= len(prefix)+len(runtimeUserEmailSuffix) {
		return "", false
	}
	if email[:len(prefix)] != prefix {
		return "", false
	}
	if email[len(email)-len(runtimeUserEmailSuffix):] != runtimeUserEmailSuffix {
		return "", false
	}
	id := email[len(prefix) : len(email)-len(runtimeUserEmailSuffix)]
	return id, id != ""
}
