package xrayconfig

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Config struct {
	Inbounds []Inbound `json:"inbounds"`
}

type Inbound struct {
	Tag      string          `json:"tag"`
	Protocol string          `json:"protocol"`
	Settings InboundSettings `json:"settings"`
}

type InboundSettings struct {
	Clients []Client `json:"clients"`
}

type Client struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Flow  string `json:"flow"`
}

func VLESSClients(data []byte, inboundTag string) ([]Client, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse Xray config JSON: %w", err)
	}

	inboundTag = strings.TrimSpace(inboundTag)
	clients := make([]Client, 0)
	for _, inbound := range cfg.Inbounds {
		if !strings.EqualFold(strings.TrimSpace(inbound.Protocol), "vless") {
			continue
		}
		if inboundTag != "" && strings.TrimSpace(inbound.Tag) != inboundTag {
			continue
		}
		for _, client := range inbound.Settings.Clients {
			client.ID = strings.TrimSpace(client.ID)
			client.Email = strings.TrimSpace(client.Email)
			client.Flow = strings.TrimSpace(client.Flow)
			if client.ID == "" {
				continue
			}
			clients = append(clients, client)
		}
	}
	if inboundTag != "" && len(clients) == 0 {
		return nil, fmt.Errorf("no VLESS clients found for inbound tag %q", inboundTag)
	}
	return clients, nil
}
