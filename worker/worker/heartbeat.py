import logging
import threading
import time

logger= logging.getLogger(__name__)

class HeartbeatClient:
    """
    Send periodic heartbeats to raft leader placehold for s0.3, real gRPC call added in s1.4
    """
    def __init__(self, worker_id: str, cloud_tag:str, orchestrator_addr:str):
        self.worker_id=worker_id
        self.cloud_tag=cloud_tag
        self.orchestrator_addr=orchestrator_addr
        self._stop_event=threading.Event()

    def run(self):
        logger.info(
            f"Heartbeat loop started "
            f"worker_id={self.worker_id} "
            f"addr={self.orchestrator_addr}"
        )
        while not self._stop_event.is_set():
            self._send_heartbeat()
            self._stop_event.wait(timeout=5)

    def _send_heartbeat(self):
        #also placeholder pedning real impl in s1.4
        logger.debug(
            f"heartbeat worker_id={self.worker_id} cloud ={self.cloud_tag}"
        )

    def stop(self):
        self._stop_event.set()
        logger.info("Heartbeat client stopped")