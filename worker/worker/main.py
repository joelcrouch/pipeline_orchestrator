import logging
import os
import threading

import uvicorn
from fastapi import FastAPI
from fastapi.responses import JSONResponse

from worker.heartbeat import HeartbeatClient

logging.basicConfig(
    level=logging.INFO,
    format='{"time":"%(asctime)s","level":"%(levelname)s","msg":"%(message)s"}',
)
logger = logging.getLogger(__name__)

# ── Config from env ───────────────────────────────────────────────
WORKER_ID = os.environ.get("WORKER_ID", "worker-unknown")
CLOUD_TAG = os.environ.get("WORKER_CLOUD_TAG", "unknown")
ORCHESTRATOR_ADDR = os.environ.get("ORCHESTRATOR_ADDR", "")
HTTP_PORT = int(os.environ.get("HTTP_PORT", "8081"))

# ── App ───────────────────────────────────────────────────────────
app = FastAPI(title="Pipeline Worker", version="0.1.0")

heartbeat_client: HeartbeatClient | None = None
heartbeat_thread: threading.Thread | None = None


@app.on_event("startup")
async def startup():
    global heartbeat_client, heartbeat_thread
    logger.info(f"Worker ready — id={WORKER_ID} cloud={CLOUD_TAG}")

    if ORCHESTRATOR_ADDR:
        heartbeat_client = HeartbeatClient(
            worker_id=WORKER_ID,
            cloud_tag=CLOUD_TAG,
            orchestrator_addr=ORCHESTRATOR_ADDR,
        )
        heartbeat_thread = threading.Thread(
            target=heartbeat_client.run, daemon=True
        )
        heartbeat_thread.start()
        logger.info(f"Heartbeat started -> {ORCHESTRATOR_ADDR}")
    else:
        logger.warning("ORCHESTRATOR_ADDR not set — heartbeat disabled")


@app.on_event("shutdown")
async def shutdown():
    if heartbeat_client:
        heartbeat_client.stop()
    logger.info("Worker shutdown complete")


# ── Routes ────────────────────────────────────────────────────────

@app.get("/health")
async def health():
    return JSONResponse({"status": "ok", "worker_id": WORKER_ID, "cloud": CLOUD_TAG})


@app.get("/status")
async def status():
    return JSONResponse({
        "worker_id": WORKER_ID,
        "cloud_tag": CLOUD_TAG,
        "orchestrator_addr": ORCHESTRATOR_ADDR or "not configured",
        "heartbeat_active": heartbeat_client is not None,
    })


# ── Entrypoint ────────────────────────────────────────────────────

if __name__ == "__main__":
    uvicorn.run("worker.main:app", host="0.0.0.0", port=HTTP_PORT, reload=False)