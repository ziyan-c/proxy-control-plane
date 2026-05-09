package domain

import (
	"encoding/json"
	"testing"
)

func TestCustomerStatusNormalization(t *testing.T) {
	if got := CustomerStatusOrDefault(" Active "); got != CustomerStatusActive {
		t.Fatalf("CustomerStatusOrDefault() = %q, want %q", got, CustomerStatusActive)
	}
	if !CustomerStatusIsActive("ACTIVE") {
		t.Fatal("CustomerStatusIsActive rejected uppercase active status")
	}
}

func TestTrafficUsageMarshalIncludesGBFields(t *testing.T) {
	usage := TrafficUsage{
		UploadBytes:   1500 * 1000 * 1000,
		DownloadBytes: 500 * 1000 * 1000,
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
		TotalBytes int64   `json:"total_bytes"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.UploadGB != 1.5 || got.DownloadGB != 0.5 || got.TotalGB != 2 {
		t.Fatalf("GB fields = upload %v download %v total %v", got.UploadGB, got.DownloadGB, got.TotalGB)
	}
	if got.TotalBytes != 2000*1000*1000 {
		t.Fatalf("total_bytes = %v", got.TotalBytes)
	}
}

func TestDomainAccessLogMarshalIncludesGBFields(t *testing.T) {
	log := DomainAccessLog{
		UploadBytes:   250 * 1000 * 1000,
		DownloadBytes: 750 * 1000 * 1000,
	}
	data, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		UploadGB   float64 `json:"upload_gb"`
		DownloadGB float64 `json:"download_gb"`
		TotalGB    float64 `json:"total_gb"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.UploadGB != 0.25 || got.DownloadGB != 0.75 || got.TotalGB != 1 {
		t.Fatalf("GB fields = upload %v download %v total %v", got.UploadGB, got.DownloadGB, got.TotalGB)
	}
}
