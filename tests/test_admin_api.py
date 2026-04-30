def test_health(client):
    response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_customer_node_and_account_flow(client, admin_headers):
    customer_response = client.post(
        "/admin/customers",
        headers=admin_headers,
        json={"email": "alice@example.com", "display_name": "Alice"},
    )
    assert customer_response.status_code == 201
    customer_id = customer_response.json()["id"]

    node_response = client.post(
        "/admin/nodes",
        headers=admin_headers,
        json={
            "name": "la-us",
            "hostname": "la-us.example.com",
            "region": "us-west",
            "port": 443,
        },
    )
    assert node_response.status_code == 201
    node_id = node_response.json()["id"]

    account_response = client.post(
        "/admin/proxy-accounts",
        headers=admin_headers,
        json={
            "customer_id": customer_id,
            "email_tag": "alice-main",
            "node_ids": [node_id],
        },
    )
    assert account_response.status_code == 201
    assert account_response.json()["customer_id"] == customer_id


def test_admin_endpoints_require_bearer_token(client):
    response = client.get("/admin/customers")

    assert response.status_code == 401

