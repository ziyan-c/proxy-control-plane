package xrayconfig

import "testing"

func TestVLESSClientsParsesTaggedInbound(t *testing.T) {
	data := []byte(`{
		"inbounds": [
			{
				"tag": "proxy-control-plane-vless-in",
				"protocol": "vless",
				"settings": {
					"clients": [
						{"id": " 00000000-0000-4000-8000-000000000001 ", "email": "legacy@example", "flow": "xtls-rprx-vision"},
						{"id": ""}
					]
				}
			},
			{
				"tag": "api",
				"protocol": "dokodemo-door",
				"settings": {"clients": [{"id": "ignored"}]}
			}
		]
	}`)

	clients, err := VLESSClients(data, "proxy-control-plane-vless-in")
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 {
		t.Fatalf("len(clients) = %d, want 1", len(clients))
	}
	if clients[0].ID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("client ID = %q", clients[0].ID)
	}
	if clients[0].Email != "legacy@example" || clients[0].Flow != "xtls-rprx-vision" {
		t.Fatalf("client = %#v", clients[0])
	}
}

func TestVLESSClientsReturnsErrorForMissingTag(t *testing.T) {
	_, err := VLESSClients([]byte(`{"inbounds":[]}`), "missing")
	if err == nil {
		t.Fatal("expected error for missing inbound tag")
	}
}
