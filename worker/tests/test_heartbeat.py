import time

from worker.heartbeat import HeartbeatClient


def test_heartbeat_client_starts_and_stops():
    client = HeartbeatClient(
        worker_id="test-worker",
        cloud_tag="aws",
        orchestrator_addr="localhost:50051",
    )
    import threading
    t = threading.Thread(target=client.run, daemon=True)
    t.start()
    time.sleep(0.1)
    client.stop()
    t.join(timeout=2)
    assert not t.is_alive()


def test_heartbeat_client_stop_without_start():
    client = HeartbeatClient(
        worker_id="test-worker",
        cloud_tag="gcp",
        orchestrator_addr="localhost:50051",
    )
    # Should not raise even if stop called before run
    client.stop()
