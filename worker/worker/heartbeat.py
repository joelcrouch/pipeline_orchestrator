import logging
import threading

import grpc

from worker.gen import worker_pb2, worker_pb2_grpc

logger = logging.getLogger(__name__)

_RETRY_DELAY_S = 5
_HEARTBEAT_INTERVAL_S = 5


class HeartbeatClient:
    """
    Registers with the Raft leader on startup, then sends periodic heartbeats.
    Handles leader redirects transparently — S1.4 real gRPC implementation.
    """

    def __init__(
        self,
        worker_id: str,
        cloud_tag: str,
        orchestrator_addr: str,
        worker_addr: str = "",
    ):
        self.worker_id = worker_id
        self.cloud_tag = cloud_tag
        self.worker_addr = worker_addr or worker_id  # fallback: use worker_id as addr
        self._orchestrator_addr = orchestrator_addr   # mutable — updated on redirect
        self._stop_event = threading.Event()

    def run(self):
        logger.info(
            f"Heartbeat loop starting "
            f"worker_id={self.worker_id} "
            f"addr={self._orchestrator_addr}"
        )
        self._register_with_retry()

        while not self._stop_event.is_set():
            self._send_heartbeat()
            self._stop_event.wait(timeout=_HEARTBEAT_INTERVAL_S)

    # ── Registration ──────────────────────────────────────────────────────────

    def _register_with_retry(self):
        """Loops until successfully registered or stop() is called."""
        while not self._stop_event.is_set():
            try:
                if self._register():
                    return  # success
                # _register returned False → redirect received, addr updated, retry immediately
            except Exception as e:
                logger.warning(
                    f"Registration failed, retrying in {_RETRY_DELAY_S}s: "
                    f"worker_id={self.worker_id} error={e}"
                )
                self._stop_event.wait(timeout=_RETRY_DELAY_S)

    def _register(self) -> bool:
        """
        Attempts one RegisterWorker RPC.
        Returns True on success.
        Returns False if a redirect was received (self._orchestrator_addr updated).
        Raises on hard errors.
        """
        with grpc.insecure_channel(self._orchestrator_addr) as channel:
            stub = worker_pb2_grpc.WorkerServiceStub(channel)
            req = worker_pb2.RegisterWorkerRequest(
                worker_id=self.worker_id,
                address=self.worker_addr,
                cloud_tag=self.cloud_tag,
            )
            try:
                resp = stub.RegisterWorker(req, timeout=5.0)
            except grpc.RpcError as e:
                raise RuntimeError(
                    f"gRPC RegisterWorker: {e.code()} {e.details()}"
                )

        if resp.ok:
            logger.info(
                f"Registered: worker_id={self.worker_id} "
                f"orchestrator={self._orchestrator_addr}"
            )
            return True

        if resp.leader_addr:
            logger.info(
                f"Redirected to leader: {resp.leader_addr} "
                f"(was {self._orchestrator_addr})"
            )
            self._orchestrator_addr = resp.leader_addr
            return False

        raise RuntimeError(f"RegisterWorker rejected: {resp.error}")

    # ── Heartbeat ─────────────────────────────────────────────────────────────

    def _send_heartbeat(self):
        """Sends one Heartbeat RPC. Logs errors but never raises."""
        try:
            with grpc.insecure_channel(self._orchestrator_addr) as channel:
                stub = worker_pb2_grpc.WorkerServiceStub(channel)
                req = worker_pb2.HeartbeatRequest(worker_id=self.worker_id)
                resp = stub.Heartbeat(req, timeout=3.0)

            if resp.ok:
                logger.debug(f"Heartbeat ok: worker_id={self.worker_id}")
                return

            if resp.leader_addr:
                logger.info(
                    f"Heartbeat redirected to leader: {resp.leader_addr} "
                    f"(was {self._orchestrator_addr})"
                )
                self._orchestrator_addr = resp.leader_addr
                return

            logger.warning(
                f"Heartbeat rejected: worker_id={self.worker_id} "
                f"error={resp.error}"
            )

        except grpc.RpcError as e:
            logger.warning(
                f"Heartbeat gRPC error: worker_id={self.worker_id} "
                f"code={e.code()} details={e.details()}"
            )
        except Exception as e:
            logger.warning(
                f"Heartbeat error: worker_id={self.worker_id} error={e}"
            )

    def stop(self):
        self._stop_event.set()
        logger.info(f"Heartbeat client stopped: worker_id={self.worker_id}")
