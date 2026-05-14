# Sprint 2 — High-Throughput MapReduce Core

 **Goal:** Implement the specialized MapReduce core and validate high-throughput execution by successfully passing the Distributed Matrix Multiplication test.

 **Branch convention:** `feature/s2.1-chunking`, `feature/s2.2-map-core`, `feature/s2.3-reduce-core`

 ---

 ## Stories

 | Story | Description | Effort |
 |---|---|---|
 | **S2.1** | Matrix Chunking & Manifest | 8 pts |
 | **S2.2** | Map Task Core (Computation) | 8 pts |
 | **S2.3** | Shuffle/Reduce Core (Aggregation) | 13 pts |

 **Dependency order:** S2.1 → S2.2 → S2.3

 ---

 ## Story 2.1 — Matrix Chunking & Manifest

 **Description:** Implement logic to slice matrices A and B into blocks and generate a `MapTaskManifest` — the ordered list of map tasks that the orchestrator will dispatch to
 workers.

 ### Acceptance Criteria

 - **AC1** `chunker.py`: given matrix dims `(m, k, n)` and `block_size`, generates the correct number of map tasks: `ceil(m/B) × ceil(k/B) × ceil(n/B)` tasks
 - **AC2** Each `MapTask` carries: `job_id`, `task_id`, block indices `(i, j, k)`, MinIO URIs for `A[i,j]` and `B[j,k]`, and the output URI where the partial result should be
 written
 - **AC3** Edge case: matrix dimension not divisible by `block_size` — final blocks are padded to `block_size` with zeros before upload, results are trimmed on read
 - **AC4** Manifest is submitted to the control plane via `SubmitJob` gRPC RPC, stored in the Raft FSM under `CmdSubmitJob`; `/jobs/{job_id}` HTTP endpoint returns manifest +
 status
 - **AC5** Unit test: `A(4096×4096)`, `B(4096×4096)`, `block_size=1024` → exactly 64 map tasks with correct URIs

 ### Files

 | File | Action |
 |---|---|
 | `proto/task.proto` | New — MapTask, ReduceTask, TaskResult, TaskService |
 | `scripts/proto-gen.sh` | Update to also generate task stubs |
 | `control-plane/internal/gen/task/` | Generated Go stubs |
 | `worker/worker/gen/task_pb2*.py` | Generated Python stubs |
 | `control-plane/internal/scheduler/manifest.go` | Job struct, CmdSubmitJob, CmdUpdateTaskStatus FSM commands |
 | `control-plane/internal/raft/fsm.go` | Add job/task state to PipelineFSM |
 | `control-plane/cmd/orchestrator/main.go` | Register TaskService, add `/jobs/{id}` HTTP handler |
 | `worker/worker/tasks/__init__.py` | New empty package |
 | `worker/worker/tasks/chunker.py` | matrix_dims → MapTaskManifest |
 | `worker/tests/test_chunker.py` | Unit tests for chunker |
 | `scripts/sprint2_verification.sh` | New verification script |

 ### Proto definition (`proto/task.proto`)

 ```protobuf
 syntax = "proto3";
 package task;
 option go_package = "github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/gen/task;taskpb";

 message MapTask {
   string job_id     = 1;
   string task_id    = 2;  // "map_{i}_{j}_{k}"
   int32  i          = 3;  // A row-block index
   int32  j          = 4;  // A col-block / B row-block index
   int32  k          = 5;  // B col-block index
   string a_uri      = 6;  // "s3://pipeline-data/jobs/{job_id}/blocks/A_{i}_{j}.npy"
   string b_uri      = 7;  // "s3://pipeline-data/jobs/{job_id}/blocks/B_{j}_{k}.npy"
   string output_uri = 8;  // "s3://pipeline-data/jobs/{job_id}/partial/C_{i}_{k}_{j}.npy"
   int32  block_size = 9;
 }

 message ReduceTask {
   string   job_id      = 1;
   string   task_id     = 2;  // "reduce_{i}_{k}"
   int32    i           = 3;
   int32    k           = 4;
   repeated string input_uris  = 5;  // all partial C_{i}_{k}_{j} URIs
   string   output_uri  = 6;  // "s3://pipeline-data/jobs/{job_id}/result/C_{i}_{k}.npy"
 }

 message JobRequest {
   string job_id     = 1;
   int32  m          = 2;  // rows of A
   int32  k_dim      = 3;  // cols of A / rows of B
   int32  n          = 4;  // cols of B
   int32  block_size = 5;
 }
 message JobResponse  { bool ok = 1; string error = 2; }

 message PollTaskRequest  { string worker_id = 1; string task_type = 2; }  // task_type: "map" | "reduce"
 message PollTaskResponse {
   bool     has_task   = 1;
   MapTask  map_task   = 2;
   ReduceTask reduce_task = 3;
 }

 message ReportResultRequest {
   string job_id      = 1;
   string task_id     = 2;
   bool   success     = 3;
   string error       = 4;
   int64  duration_ms = 5;
 }
 message ReportResultResponse { bool ok = 1; }

 message GetJobStatusRequest  { string job_id = 1; }
 message GetJobStatusResponse {
   string job_id        = 1;
   string status        = 2;  // "pending" | "mapping" | "reducing" | "done" | "failed"
   int32  map_total     = 3;
   int32  map_done      = 4;
   int32  reduce_total  = 5;
   int32  reduce_done   = 6;
 }

 service TaskService {
   rpc SubmitJob        (JobRequest)           returns (JobResponse);
   rpc PollTask         (PollTaskRequest)      returns (PollTaskResponse);
   rpc ReportTaskResult (ReportResultRequest)  returns (ReportResultResponse);
   rpc GetJobStatus     (GetJobStatusRequest)  returns (GetJobStatusResponse);
 }
 ```

 ### `worker/worker/tasks/chunker.py` sketch

 ```python
 import math
 from dataclasses import dataclass
 from typing import List

 @dataclass
 class MapTask:
     job_id: str
     task_id: str
     i: int; j: int; k: int
     a_uri: str; b_uri: str; output_uri: str
     block_size: int

 def generate_manifest(job_id: str, m: int, k_dim: int, n: int,
                       block_size: int) -> List[MapTask]:
     """Return all (i,j,k) map tasks for A(m×k_dim) @ B(k_dim×n)."""
     bi = math.ceil(m     / block_size)
     bj = math.ceil(k_dim / block_size)
     bk = math.ceil(n     / block_size)
     tasks = []
     base = f"s3://pipeline-data/jobs/{job_id}"
     for i in range(bi):
         for j in range(bj):
             for k in range(bk):
                 tasks.append(MapTask(
                     job_id=job_id,
                     task_id=f"map_{i}_{j}_{k}",
                     i=i, j=j, k=k,
                     a_uri=f"{base}/blocks/A_{i}_{j}.npy",
                     b_uri=f"{base}/blocks/B_{j}_{k}.npy",
                     output_uri=f"{base}/partial/C_{i}_{k}_{j}.npy",
                     block_size=block_size,
                 ))
     return tasks
 ```

 ---

 ## Story 2.2 — Map Task Core (Computation)

 **Description:** Implement worker logic to execute a map task: pull A[i,j] and B[j,k] from MinIO, compute the partial product using PyTorch (GPU if available, CPU fallback), and
 push the result to MinIO by URI.

 ### Acceptance Criteria

 - **AC1** Worker calls `PollTask(worker_id, "map")` on a 1s loop; on `has_task=true` it executes the map task and calls `ReportTaskResult`
 - **AC2** Map execution: downloads A[i,j] and B[j,k] from MinIO as `.npy` files, computes `torch.matmul(A_block, B_block)`, uploads result to `output_uri`
 - **AC3** GPU-aware: uses `torch.device("cuda" if torch.cuda.is_available() else "cpu")`; result is moved to CPU before upload
 - **AC4** Partial result at `output_uri` is verifiable: `numpy.load(uri)` equals `numpy.matmul(A_block, B_block)` within `atol=1e-4`
 - **AC5** End-to-end smoke test with 4 workers: submit a job for `A(256×256)`, `B(256×256)`, `block_size=128` (8 map tasks); all 8 tasks complete within 60s; all partial result
 URIs are readable
 - **AC6** `ReportTaskResult` on success updates task status in FSM to `"done"` via `CmdUpdateTaskStatus`

 ### Files

 | File | Action |
 |---|---|
 | `control-plane/internal/scheduler/scheduler.go` | Dispatches pending map tasks via `PollTask`; tracks task state |
 | `control-plane/internal/scheduler/scheduler_test.go` | Unit tests with mock FSM |
 | `worker/worker/tasks/matrix_multiply.py` | PyTorch matmul execution |
 | `worker/worker/tasks/storage.py` | MinIO get/put helpers wrapping `boto3` |
 | `worker/worker/main.py` | Start `TaskWorker` thread alongside heartbeat |
 | `worker/tests/test_matrix_multiply.py` | Unit + integration tests |

 ### `worker/worker/tasks/matrix_multiply.py` sketch

 ```python
 import io
 import numpy as np
 import torch

 def run_map_task(task, minio_client) -> dict:
     """Execute one MapTask. Returns result metadata."""
     A = _load_npy(minio_client, task.a_uri)
     B = _load_npy(minio_client, task.b_uri)

     device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
     tA = torch.from_numpy(A).to(device)
     tB = torch.from_numpy(B).to(device)

     import time
     t0 = time.perf_counter()
     C = torch.matmul(tA, tB).cpu().numpy()
     duration_ms = int((time.perf_counter() - t0) * 1000)

     _save_npy(minio_client, task.output_uri, C)
     return {"device": str(device), "shape": list(C.shape), "duration_ms": duration_ms}

 def _load_npy(client, uri: str) -> np.ndarray:
     bucket, key = _parse_uri(uri)
     obj = client.get_object(Bucket=bucket, Key=key)
     return np.load(io.BytesIO(obj["Body"].read()))

 def _save_npy(client, uri: str, arr: np.ndarray):
     bucket, key = _parse_uri(uri)
     buf = io.BytesIO(); np.save(buf, arr); buf.seek(0)
     client.put_object(Bucket=bucket, Key=key, Body=buf)

 def _parse_uri(uri: str):
     # "s3://bucket/key" → ("bucket", "key")
     path = uri.removeprefix("s3://")
     bucket, _, key = path.partition("/")
     return bucket, key
 ```

 ### Scheduler design (`internal/scheduler/scheduler.go`)

 ```
 JobScheduler
   ├── queue: []PendingTask   (in-memory, rebuilt from FSM on startup)
   ├── PollTask(worker_id, task_type) → next pending task or empty
   └── ReportTaskResult(task_id, success) → apply CmdUpdateTaskStatus via Raft
        if all map tasks done → enqueue reduce tasks (triggers S2.3)
 ```

 - Leader-only (followers redirect same as AgentRegistry)
 - `PollTask` is idempotent: re-assigns a task if it has been in "assigned" state for >30s without a result (simple re-queue)

 ---

 ## Story 2.3 — Shuffle/Reduce Core (Aggregation)

 **Description:** Implement the orchestration logic to chain Map → Reduce. After all map tasks for a given output block `C[i,k]` complete, schedule a reduce task. The reduce
 worker pulls the partial products cross-cloud and performs the final summation.

 ### Acceptance Criteria

 - **AC1** `JobScheduler.checkMapProgress()` (called on every `ReportTaskResult`): when all `j` partial products for a `(i,k)` pair are `"done"`, enqueue one `ReduceTask` for that
  pair
 - **AC2** Reduce worker calls `PollTask(worker_id, "reduce")`; downloads all `input_uris`, sums them with `numpy.sum(partials, axis=0)`, writes `output_uri`
 - **AC3** When all reduce tasks are `"done"`, scheduler applies `CmdUpdateJobStatus("done")` via Raft; `/jobs/{job_id}` HTTP endpoint returns `"status":"done"`
 - **AC4** **Correctness check:** end-to-end test — generate random `A(512×512)`, `B(512×512)`, `block_size=128`; distribute across all 4 workers; assemble final `C` from result
 blocks; verify `||C_distributed - A@B||_F / ||A@B||_F < 1e-5`
 - **AC5** Cross-cloud: at least one map task executes on `worker-gcp-1` or `worker-azure-1` and its partial result is consumed by a reduce task on a different worker
 - **AC6** For the `>100GB` claim: document that total data volume = `2 × m × k_dim × n × 4 bytes` (float32); demonstrate the block-wise algorithm is size-independent by running
 at `block_size=64` and `block_size=256` and showing identical results

 ### Files

 | File | Action |
 |---|---|
 | `control-plane/internal/scheduler/scheduler.go` | Add `checkMapProgress`, `enqueueReduceTasks`, `checkReduceProgress` |
 | `control-plane/internal/scheduler/scheduler_test.go` | Add reduce-phase tests |
 | `worker/worker/tasks/matrix_multiply.py` | Add `run_reduce_task()` |
 | `worker/tests/test_end_to_end.py` | Full correctness test against numpy reference |
 | `scripts/sprint2_verification.sh` | Fill in `s2_3()` with correctness check |
 | `docs/handoff.md` | Record RQ1 throughput baseline numbers |

 ### End-to-end test sketch (`worker/tests/test_end_to_end.py`)

 ```python
 import numpy as np
 import pytest

 def test_distributed_matmul_correctness(cluster):
     """Full MapReduce pipeline produces numerically correct result."""
     rng = np.random.default_rng(42)
     A = rng.standard_normal((512, 512)).astype(np.float32)
     B = rng.standard_normal((512, 512)).astype(np.float32)
     C_ref = A @ B

     job_id = cluster.submit_job(A, B, block_size=128)
     cluster.wait_for_job(job_id, timeout=120)

     C_dist = cluster.assemble_result(job_id, shape=(512, 512))
     rel_err = np.linalg.norm(C_dist - C_ref, "fro") / np.linalg.norm(C_ref, "fro")
     assert rel_err < 1e-5, f"Relative Frobenius error {rel_err:.2e} exceeds threshold"
 ```

 ---

 ## New FSM Commands

 Add to `control-plane/internal/raft/fsm.go`:

 | Command | Payload | Effect |
 |---|---|---|
 | `CmdSubmitJob` | `{job_id, map_tasks[], status}` | Creates job entry in FSM |
 | `CmdUpdateTaskStatus` | `{job_id, task_id, status, duration_ms}` | Mutates task status |
 | `CmdUpdateJobStatus` | `{job_id, status}` | Mutates job status |

 ---

 ## New HTTP Endpoints (control plane)

 | Endpoint | Method | Description |
 |---|---|---|
 | `/jobs` | GET | List all jobs (id, status, progress) |
 | `/jobs/{id}` | GET | Full job details + task list |

 ---

 ## Test Plan

 ### Unit (no Docker)
 - `test_chunker.py` — task count, URI format, edge cases
 - `test_matrix_multiply.py` — partial product correctness, GPU fallback
 - `scheduler_test.go` — PollTask FIFO, re-queue on timeout, reduce trigger
 - `registry_test.go` — existing tests still green

 ### Integration (Docker cluster)
 ```bash
 bash scripts/sprint2_verification.sh s2.1   # chunker + manifest + FSM
 bash scripts/sprint2_verification.sh s2.2   # 8 map tasks complete, URIs readable
 bash scripts/sprint2_verification.sh s2.3   # correctness check, >100GB doc
 bash scripts/sprint2_verification.sh all    # full sprint 2 suite
 ```

 ---

 ## RQ1 Baseline (record in handoff.md after S2.3)

 After a successful `A(2048×2048) @ B(2048×2048)` run with `block_size=512`, record:
 - Total wall-clock time
 - Effective FLOP/s (2 × m × k × n ops / wall-clock)
 - Per-worker time breakdown (map phase vs reduce phase)
 - MinIO throughput (GB/s read + write)

 These are the **normal-operations baseline** measurements referenced in RQ1 and used as the control group for Sprint 4 chaos experiments.

 ---

 ## Implementation Order

 1. Write `proto/task.proto` → run `bash scripts/proto-gen.sh` → commit stubs
 2. Add `CmdSubmitJob / CmdUpdateTaskStatus / CmdUpdateJobStatus` to FSM → unit tests green
 3. Implement `chunker.py` + `test_chunker.py` → `s2_1()` green
 4. Implement `TaskService` gRPC server (SubmitJob + PollTask + ReportTaskResult) in `internal/scheduler/`
 5. Implement `matrix_multiply.py` + `storage.py` + `TaskWorker` thread in Python worker
 6. End-to-end smoke: submit 8-task job, all map tasks complete → `s2_2()` green
 7. Add `checkMapProgress` → reduce task enqueue → `run_reduce_task()` → correctness test
 8. `s2_3()` green → record RQ1 baseline numbers
