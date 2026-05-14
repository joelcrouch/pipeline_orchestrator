import time
import torch
import numpy as np
import logging
from worker.storage.client import StorageClient

logger = logging.getLogger(__name__)

def execute_map_task(task, storage: StorageClient) -> dict:
    """Computes torch.matmul(A_block, B_block) and saves to output_uri."""
    logger.info(f"Executing MapTask: {task.task_id} (block {task.i}, {task.j}, {task.k})")
    
    # Load blocks
    A = storage.load_npy(task.a_uri)
    B = storage.load_npy(task.b_uri)
    
    # matmul
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    tA = torch.from_numpy(A).to(device)
    tB = torch.from_numpy(B).to(device)
    
    t0 = time.perf_counter()
    tC = torch.matmul(tA, tB)
    duration_ms = int((time.perf_counter() - t0) * 1000)
    
    C = tC.cpu().numpy()
    
    # Save partial result
    storage.save_npy(task.output_uri, C)
    
    return {
        "duration_ms": duration_ms,
        "device": str(device),
        "shape": list(C.shape),
    }
