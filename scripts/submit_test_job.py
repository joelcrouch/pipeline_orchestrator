import grpc
import numpy as np
import io
import boto3
import uuid
import sys
import os

# Add generated stubs to path
sys.path.append(os.path.join(os.path.dirname(__file__), "../worker"))
from worker.gen import task_pb2, task_pb2_grpc

def upload_matrix_blocks(minio_client, bucket, job_id, matrix_name, matrix, block_size):
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
            minio_client.put_object(Bucket=bucket, Key=key, Body=buf)
            print(f"  Uploaded {matrix_name}_{i}_{j}")

def submit_job():
    job_id = str(uuid.uuid4())[:8]
    M, K, N = 256, 256, 256
    BLOCK_SIZE = 128
    
    print(f"🚀 Starting Job {job_id} (MatMul {M}x{K} @ {K}x{N}, Block: {BLOCK_SIZE})")
    
    # Setup MinIO
    s3 = boto3.client('s3',
                      endpoint_url='http://localhost:9000',
                      aws_access_key_id='minioadmin',
                      aws_secret_access_key='minioadmin')
    
    # Generate random data
    rng = np.random.default_rng()
    A = rng.standard_normal((M, K)).astype(np.float32)
    B = rng.standard_normal((K, N)).astype(np.float32)
    
    upload_matrix_blocks(s3, "pipeline-data", job_id, "A", A, BLOCK_SIZE)
    upload_matrix_blocks(s3, "pipeline-data", job_id, "B", B, BLOCK_SIZE)
    
    # Connect to Orchestrator (Leader usually on 50051)
    # Note: In a real scenario we might need to try other ports if 50051 isn't the leader
    channel = grpc.insecure_channel('localhost:50051')
    stub = task_pb2_grpc.TaskServiceStub(channel)
    
    req = task_pb2.JobRequest(
        job_id=job_id,
        m=M,
        k_dim=K,
        n=N,
        block_size=BLOCK_SIZE,
        model_type="matmul"
    )
    
    try:
        resp = stub.SubmitJob(req)
        if not resp.ok:
            if resp.leader_addr:
                print(f"❌ Not leader. Redirecting to {resp.leader_addr}...")
                # Extract port from leader_addr (e.g. "cp-gcp-1:50051")
                # Since we are local, we map cp-gcp-1:50051 to localhost:50052
                port_map = {"cp-aws-1": "50051", "cp-gcp-1": "50052", "cp-azure-1": "50054"}
                host = resp.leader_addr.split(":")[0]
                new_port = port_map.get(host, "50051")
                channel = grpc.insecure_channel(f'localhost:{new_port}')
                stub = task_pb2_grpc.TaskServiceStub(channel)
                resp = stub.SubmitJob(req)
            else:
                print(f"❌ Error: {resp.error}")
                return

        if resp.ok:
            print(f"✅ Job {job_id} submitted successfully!")
            print(f"   View status: curl -s http://localhost:8080/jobs/{job_id} | jq")
    except Exception as e:
        print(f"💥 Failed to submit job: {e}")

if __name__ == "__main__":
    submit_job()
