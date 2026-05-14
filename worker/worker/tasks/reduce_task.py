import time
import numpy as np
import logging
from worker.storage.client import StorageClient

logger = logging.getLogger(__name__)

def execute_reduce_task(task, storage: StorageClient) -> dict:
    """Sums partial products for a single output block and saves to output_uri."""
    logger.info(f"Executing ReduceTask: {task.task_id} (block {task.i}, {task.k})")
    
    t0 = time.perf_counter()
    
    # Load all partial products
    partials = []
    for uri in task.input_uris:
        partials.append(storage.load_npy(uri))
    
    # Sum them up
    # In a real system we might use torch here too, but for simple sum numpy is fine.
    result = np.sum(partials, axis=0)
    
    duration_ms = int((time.perf_counter() - t0) * 1000)
    
    # Save final block result
    storage.save_npy(task.output_uri, result)
    
    return {
        "duration_ms": duration_ms,
        "shape": list(result.shape),
        "num_partials": len(partials),
    }
