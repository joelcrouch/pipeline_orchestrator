import grpc
import numpy as np
import io
import boto3
import uuid
import sys
import os
import time

# Add generated stubs to path
sys.path.append(os.path.join(os.path.dirname(__file__), "../worker"))
from worker.gen import task_pb2, task_pb2_grpc

def get_s3_client():
    return boto3.client('s3',
                        endpoint_url='http://localhost:9000',
                        aws_access_key_id='minioadmin',
                        aws_secret_access_key='minioadmin')

def upload_matrix_blocks(s3, bucket, job_id, matrix_name, matrix, block_size):
    m, n = matrix.shape
    bi = (m + block_size - 1) // block_size
    bj = (n + block_size - 1) // block_size
    
    for i in range(bi):
        for j in range(bj):
            row_start = i * block_size
            row_end = min(row_start + block_size, m)
            col_start = j * block_size
            col_end = min(col_start + block_size, n)
            
            block = matrix[row_start:row_end, col_start:col_end]
            
            # Pad if necessary
            if block.shape != (block_size, block_size):
                padded = np.zeros((block_size, block_size), dtype=matrix.dtype)
                padded[:block.shape[0], :block.shape[1]] = block
                block = padded
                
            buf = io.BytesIO()
            np.save(buf, block)
            buf.seek(0)
            
            key = f"jobs/{job_id}/blocks/{matrix_name}_{i}_{j}.npy"
            s3.put_object(Bucket=bucket, Key=key, Body=buf)

def download_and_assemble(s3, bucket, job_id, m, n, block_size):
    bi = (m + block_size - 1) // block_size
    bk = (n + block_size - 1) // block_size
    
    full_matrix = np.zeros((bi * block_size, bk * block_size), dtype=np.float32)
    
    for i in range(bi):
        for k in range(bk):
            key = f"jobs/{job_id}/result/C_{i}_{k}.npy"
            try:
                obj = s3.get_object(Bucket=bucket, Key=key)
                block = np.load(io.BytesIO(obj["Body"].read()))
                
                row_start = i * block_size
                col_start = k * block_size
                full_matrix[row_start:row_start+block_size, col_start:col_start+block_size] = block
            except Exception as e:
                print(f"  ⚠️ Failed to download block {i}_{k}: {e}")
                return None
                
    # Trim to original size
    return full_matrix[:m, :n]

def run_test(m, k_dim, n, block_size, timeout_secs=60):
    job_id = str(uuid.uuid4())[:8]
    print(f"🚀 Testing Job {job_id} ({m}x{k_dim} @ {k_dim}x{n}, Block: {block_size})")
    
    s3 = get_s3_client()
    rng = np.random.default_rng()
    A = rng.standard_normal((m, k_dim)).astype(np.float32)
    B = rng.standard_normal((k_dim, n)).astype(np.float32)
    
    print("  📤 Uploading blocks...")
    upload_matrix_blocks(s3, "pipeline-data", job_id, "A", A, block_size)
    upload_matrix_blocks(s3, "pipeline-data", job_id, "B", B, block_size)
    
    # Connect and submit
    channel = grpc.insecure_channel('localhost:50051')
    stub = task_pb2_grpc.TaskServiceStub(channel)
    
    req = task_pb2.JobRequest(job_id=job_id, m=m, k_dim=k_dim, n=n, block_size=block_size)
    
    try:
        resp = stub.SubmitJob(req)
        if not resp.ok and resp.leader_addr:
            port_map = {"cp-aws-1": "50051", "cp-gcp-1": "50052", "cp-azure-1": "50054"}
            host = resp.leader_addr.split(":")[0]
            channel = grpc.insecure_channel(f'localhost:{port_map[host]}')
            stub = task_pb2_grpc.TaskServiceStub(channel)
            resp = stub.SubmitJob(req)
        
        if not resp.ok:
            print(f"  ❌ Submission failed: {resp.error}")
            return False
    except Exception as e:
        print(f"  💥 gRPC error: {e}")
        return False

    print(f"  ⏳ Waiting up to {timeout_secs}s for job completion...")
    start_time = time.time()
    while time.time() - start_time < timeout_secs:
        status_resp = stub.GetJobStatus(task_pb2.GetJobStatusRequest(job_id=job_id))
        if status_resp.status == "done":
            break
        if status_resp.status == "failed":
            print("  ❌ Job failed according to orchestrator")
            return False
        time.sleep(2)
    else:
        print("  ⏰ Timeout waiting for job")
        return False

    print("  📥 Downloading and verifying result...")
    C_dist = download_and_assemble(s3, "pipeline-data", job_id, m, n, block_size)
    if C_dist is None: return False
    
    C_ref = A @ B
    
    diff = np.linalg.norm(C_dist - C_ref) / np.linalg.norm(C_ref)
    print(f"  📊 Relative Error: {diff:.2e}")
    
    if diff < 1e-5:
        print(f"  ✅ Numerical Correctness Verified!")
        return True
    else:
        print(f"  ❌ Numerical Correctness Failed!")
        return False

if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description="Verify MapReduce Matrix Multiplication")
    parser.add_argument("--m", type=int, default=256)
    parser.add_argument("--k", type=int, default=256)
    parser.add_argument("--n", type=int, default=256)
    parser.add_argument("--block-size", type=int, default=128)
    parser.add_argument("--timeout", type=int, default=120)
    args = parser.parse_args()

    success = run_test(args.m, args.k, args.n, args.block_size, args.timeout)
    if not success: sys.exit(1)
