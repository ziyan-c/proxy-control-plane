import base64


def test_vless_subscription_flow(client, admin_headers):
    customer = client.post(
        "/admin/customers",
        headers=admin_headers,
        json={"email": "bob@example.com", "display_name": "Bob"},
    ).json()
    node = client.post(
        "/admin/nodes",
        headers=admin_headers,
        json={
            "name": "fr",
            "hostname": "fr.example.com",
            "public_host": "proxy.example.com",
            "region": "eu",
            "port": 443,
        },
    ).json()
    account = client.post(
        "/admin/proxy-accounts",
        headers=admin_headers,
        json={
            "customer_id": customer["id"],
            "email_tag": "bob-phone",
            "node_ids": [node["id"]],
        },
    ).json()
    token_response = client.post(
        "/admin/subscription-tokens",
        headers=admin_headers,
        json={"customer_id": customer["id"], "name": "bob-default"},
    )
    assert token_response.status_code == 201
    token = token_response.json()["plain_token"]

    raw_response = client.get(f"/sub/{token}?fmt=raw")
    assert raw_response.status_code == 200
    assert f"vless://{account['uuid']}@proxy.example.com:443" in raw_response.text
    assert "fr-bob-phone" in raw_response.text

    encoded_response = client.get(f"/sub/{token}")
    decoded = base64.b64decode(encoded_response.text).decode()
    assert decoded == raw_response.text


def test_unknown_subscription_token_is_404(client):
    response = client.get("/sub/not-a-real-token")

    assert response.status_code == 404

