import math
from typing import List, Dict

class MapTask:
    def __init__(self, job_id: str, task_id: str, i: int, j: int, k: int, 
                 a_uri: str, b_uri: str, output_uri: str, block_size: int, 
                 metadata: Dict[str, str] = None):
        self.job_id = job_id
        self.task_id = task_id
        self.i = i
        self.j = j
        self.k = k
        self.a_uri = a_uri
        self.b_uri = b_uri
        self.output_uri = output_uri
        self.block_size = block_size
        self.metadata = metadata or {}

def generate_map_tasks(job_id: str, m: int, k_dim: int, n: int, 
                       block_size: int, model_type: str = "matmul") -> List[MapTask]:
    """
    Generates a list of MapTask objects for a distributed matrix multiplication.
    
    A is (m x k_dim)
    B is (k_dim x n)
    Block size B defines the chunks.
    
    Number of blocks:
    bi = ceil(m / B)
    bj = ceil(k_dim / B)
    bk = ceil(n / B)
    
    Total map tasks = bi * bj * bk
    """
    bi = math.ceil(m / block_size)
    bj = math.ceil(k_dim / block_size)
    bk = math.ceil(n / block_size)
    
    tasks = []
    base_uri = f"s3://pipeline-data/jobs/{job_id}"
    
    for i in range(bi):
        for j in range(bj):
            for k in range(bk):
                task_id = f"map_{i}_{j}_{k}"
                tasks.append(MapTask(
                    job_id=job_id,
                    task_id=task_id,
                    i=i,
                    j=j,
                    k=k,
                    a_uri=f"{base_uri}/blocks/A_{i}_{j}.npy",
                    b_uri=f"{base_uri}/blocks/B_{j}_{k}.npy",
                    output_uri=f"{base_uri}/partial/C_{i}_{k}_{j}.npy",
                    block_size=block_size,
                    metadata={
                        "model_type": model_type,
                        "i": str(i),
                        "j": str(j),
                        "k": str(k),
                        "block_size": str(block_size)
                    }
                ))
    return tasks
