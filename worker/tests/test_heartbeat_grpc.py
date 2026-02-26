"""
Tests for the real gRPC HeartbeatClient implementation (S1.4).
Uses unittest.mock to avoid needing a live gRPC server.
"""
from unittest.mock import MagicMock, patch

import grpc
import pytest

from worker.heartbeat import HeartbeatClient


def make_client(addr="localhost:50051", worker_addr="test-worker:8081"):
    return HeartbeatClient(
        worker_id="test-worker",
        cloud_tag="aws",
        orchestrator_addr=addr,
        worker_addr=worker_addr,
    )


# ── _register tests ───────────────────────────────────────────────────────────

def test_register_success():
    """_register returns True when server replies ok=True."""
    client = make_client()
    mock_resp = MagicMock()
    mock_resp.ok = True
    mock_resp.leader_addr = ""

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.RegisterWorker.return_value = mock_resp
            result = client._register()

    assert result is True
    assert client._orchestrator_addr == "localhost:50051"


def test_register_redirect():
    """_register returns False and updates _orchestrator_addr on follower redirect."""
    client = make_client()
    mock_resp = MagicMock()
    mock_resp.ok = False
    mock_resp.leader_addr = "cp-aws-1:50051"
    mock_resp.error = ""

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.RegisterWorker.return_value = mock_resp
            result = client._register()

    assert result is False
    assert client._orchestrator_addr == "cp-aws-1:50051"


def test_register_grpc_error_raises():
    """_register raises RuntimeError on gRPC transport failure."""
    client = make_client()
    rpc_err = grpc.RpcError()
    rpc_err.code = lambda: grpc.StatusCode.UNAVAILABLE
    rpc_err.details = lambda: "connection refused"

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.RegisterWorker.side_effect = rpc_err
            with pytest.raises(RuntimeError):
                client._register()


# ── _send_heartbeat tests ─────────────────────────────────────────────────────

def test_send_heartbeat_ok():
    """_send_heartbeat does not raise when server replies ok=True."""
    client = make_client()
    mock_resp = MagicMock()
    mock_resp.ok = True
    mock_resp.leader_addr = ""

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.Heartbeat.return_value = mock_resp
            client._send_heartbeat()  # must not raise


def test_send_heartbeat_redirect_updates_addr():
    """_send_heartbeat silently updates address on redirect."""
    client = make_client()
    mock_resp = MagicMock()
    mock_resp.ok = False
    mock_resp.leader_addr = "cp-gcp-1:50051"

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.Heartbeat.return_value = mock_resp
            client._send_heartbeat()

    assert client._orchestrator_addr == "cp-gcp-1:50051"


def test_send_heartbeat_grpc_error_does_not_raise():
    """_send_heartbeat swallows gRPC errors — workers must not crash on transient failures."""
    client = make_client()
    rpc_err = grpc.RpcError()
    rpc_err.code = lambda: grpc.StatusCode.UNAVAILABLE
    rpc_err.details = lambda: "connection refused"

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.Heartbeat.side_effect = rpc_err
            client._send_heartbeat()  # must not raise


def test_send_heartbeat_unexpected_error_does_not_raise():
    """_send_heartbeat swallows any non-gRPC exception too."""
    client = make_client()

    with patch("worker.heartbeat.grpc.insecure_channel"):
        with patch("worker.heartbeat.worker_pb2_grpc.WorkerServiceStub") as MockStub:
            MockStub.return_value.Heartbeat.side_effect = RuntimeError("unexpected")
            client._send_heartbeat()  # must not raise


# ── constructor tests ─────────────────────────────────────────────────────────

def test_worker_addr_fallback():
    """worker_addr defaults to worker_id when not provided."""
    client = HeartbeatClient(
        worker_id="my-worker",
        cloud_tag="gcp",
        orchestrator_addr="localhost:50051",
    )
    assert client.worker_addr == "my-worker"


def test_worker_addr_explicit():
    """Explicit worker_addr is preserved."""
    client = make_client(worker_addr="10.10.0.20:8081")
    assert client.worker_addr == "10.10.0.20:8081"
