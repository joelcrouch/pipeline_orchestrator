from fastapi.testclient import TestClient
from worker.main import app

client = TestClient(app)

def test_health_returns_200():
    response = client.get("/health")
    assert response.status_code == 200

def test_health_returns_json():
    response = client.get("/health")
    data = response.json()
    assert "status" in data
    assert data["status"] == "ok"

def test_health_contains_worker_id():
    response = client.get("/health")
    data = response.json()
    assert "worker_id" in data

def test_status_returns_200():
    response = client.get("/status")
    assert response.status_code == 200

def test_status_contains_expected_fields():
    response = client.get("/status")
    data = response.json()
    assert "worker_id" in data
    assert "cloud_tag" in data
    assert "heartbeat_active" in data
