# Project Structure

```
pipeline-orchestrator/
│
├── .gitignore
├── .env.example               # template — copy to .env, never commit .env
├── Makefile                   # top-level: sim-up, sim-down, build, test, proto-gen
├── README.md
│
│
├── proto/                     # source of truth for all inter-service contracts
│   ├── raft.proto             # RaftService: RequestVote, AppendEntries
│   ├── worker.proto           # WorkerService: RegisterWorker, Heartbeat
│   ├── task.proto             # TaskService: SubmitTask, TaskStatus (Sprint 2)
│   └── gen/                   # ← gitignored, populated by `make proto-gen`
│       ├── go/                #   generated Go stubs
│       └── python/            #   generated Python stubs
│
│
├── control-plane/             # Go service — Raft consensus + task scheduling
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── orchestrator/
│   │       └── main.go        # entrypoint: wires everything together
│   └── internal/
│       ├── raft/              # S1.1–S1.3: core Raft implementation
│       │   ├── node.go        #   RaftNode struct, state machine
│       │   ├── log.go         #   persistent write-ahead log
│       │   ├── election.go    #   RequestVote logic
│       │   ├── replication.go #   AppendEntries logic
│       │   └── raft_test.go
│       ├── agent/             # S1.4: worker registry + heartbeat tracking
│       │   ├── registry.go
│       │   └── registry_test.go
│       ├── scheduler/         # Sprint 2: task assignment + load balancing
│       │   └── scheduler.go
│       ├── storage/           # Sprint 2: MinIO/S3 client wrapper
│       │   └── storage.go
│       └── metrics/           # Prometheus instrumentation
│           └── metrics.go
│
│
├── worker/                    # Python service — Map/Reduce execution
│   ├── Dockerfile
│   ├── environment.yml        # conda env definition (see below)
│   ├── requirements-pip.txt   # pip-only deps installed AFTER conda
│   ├── pyproject.toml         # project metadata + tool config (ruff, pytest)
│   ├── worker/
│   │   ├── __init__.py
│   │   ├── main.py            # FastAPI app + startup/shutdown hooks
│   │   ├── heartbeat.py       # S1.4: background heartbeat thread
│   │   ├── tasks/             # Sprint 2: Map/Reduce execution
│   │   │   ├── __init__.py
│   │   │   ├── map_task.py
│   │   │   └── reduce_task.py
│   │   └── storage/           # Sprint 2: boto3 wrapper for MinIO/S3
│   │       ├── __init__.py
│   │       └── client.py
│   └── tests/
│       ├── test_health.py
│       └── test_heartbeat.py
│
│
├── docker/
│   ├── docker-compose.yml     # main cluster definition (6 nodes + MinIO)
│   ├── docker-compose.override.yml  # local dev: source mounts, hot reload
│   ├── gateway/               # S0.1: router container for cross-cloud hop
│   │   └── Dockerfile
│   └── init/                  # S0.5: MinIO bucket init container
│       └── init-buckets.sh
│
│
├── scripts/
│   ├── proto-gen.sh           # runs protoc for Go + Python targets
│   ├── test-storage.sh        # S0.5: smoke test MinIO PUT/GET + checksum
│   └── sim-latency.sh         # S0.1: applies tc-netem rules inside containers
│
│
└── data/                      # gitignored except structure markers
    ├── .gitkeep
    ├── raw/                   # source datasets (gitignored)
    ├── matrices/              # generated test matrices (gitignored)
    └── processed/             # pipeline output (gitignored)
```

---

## Conda Environment (`worker/environment.yml`)

This is your base conda env definition. Conda handles CUDA, cuDNN, PyTorch, and numpy — the things that go sideways with pip. Anything not on conda-forge goes in `requirements-pip.txt` and is installed after.

```yaml
name: pipeline-worker
channels:
  - pytorch          # PyTorch official channel — always use this over conda-forge for torch
  - nvidia           # CUDA toolkit + cuDNN
  - conda-forge      # everything else
  - defaults

dependencies:
  # Python
  - python=3.11

  # CUDA stack — conda manages driver compatibility here so you don't have to
  - cuda-toolkit=12.1
  - cudnn=8.9

  # Core ML
  - pytorch::pytorch=2.3
  - pytorch::torchvision
  - pytorch::pytorch-cuda=12.1

  # Data
  - numpy=1.26
  - pandas
  - scipy

  # Comms & API
  - grpcio
  - grpcio-tools         # protoc Python plugin
  - fastapi
  - uvicorn

  # Storage
  - boto3                # S3/MinIO client

  # Observability
  - prometheus-client

  # Dev & test
  - pytest
  - ruff                 # linter + formatter (replaces flake8/black/isort)
  - pip                  # always include so pip install below works

  - pip:
    - protobuf           # sometimes newer than conda-forge, keep in pip section
    - grpcio-status
```

> **Note:** After `conda env create -f environment.yml`, run:
> ```bash
> conda activate pipeline-worker
> pip install -r requirements-pip.txt   # for anything not on any conda channel
> ```

---

## `.env.example`

Copy this to `.env` before running — `.env` is gitignored, `.env.example` is committed.

```bash
# ── Raft tuning ──────────────────────────────
RAFT_ELECTION_TIMEOUT_MIN_MS=300
RAFT_ELECTION_TIMEOUT_MAX_MS=600
RAFT_HEARTBEAT_MS=150

# ── Network simulation ───────────────────────
CROSS_CLOUD_LATENCY_MS=50
CROSS_CLOUD_JITTER_MS=5

# ── MinIO (local simulation of S3/GCS) ───────
MINIO_ENDPOINT=http://minio:9000
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin   # fine for local sim, never use in prod
MINIO_BUCKET=pipeline-data

# ── Service addresses ────────────────────────
ORCHESTRATOR_ADDR=cp-aws-1:50051
METRICS_PORT=9090
DEBUG_HTTP_PORT=8080

# ── Worker config ─────────────────────────────
WORKER_CLOUD_TAG=aws            # or gcp — set per-container in compose
WORKER_HEARTBEAT_INTERVAL_S=5
WORKER_HEARTBEAT_MISS_LIMIT=3
```

---

## `Makefile` (top-level)

```makefile
.PHONY: sim-up sim-down build test lint proto-gen

sim-up:
	docker compose -f docker/docker-compose.yml up --build -d

sim-down:
	docker compose -f docker/docker-compose.yml down -v

build:
	cd control-plane && go build ./...

test:
	cd control-plane && go test ./...
	cd worker && python -m pytest

lint:
	cd control-plane && golangci-lint run
	cd worker && ruff check .

proto-gen:
	bash scripts/proto-gen.sh
```

---

## Key Decisions Explained

**Why a monorepo?**
Proto files are the contract between Go and Python. Keeping them in one repo means a single PR can update the `.proto`, regenerate both stubs, and update both services atomically — no version skew between repos.

**Why `proto/gen/` is gitignored?**
Generated code is an artifact, not source. `make proto-gen` rebuilds it deterministically from the `.proto` files. Committing generated stubs creates noisy diffs and merge conflicts with no upside.

**Why conda over pip for the worker?**
PyTorch + CUDA + cuDNN have native library dependencies (`libcudnn.so`, etc.) that pip installs into an ad-hoc location and frequently conflicts with system CUDA. Conda manages these as first-class packages in an isolated prefix — it knows the exact CUDA driver → toolkit → cuDNN → PyTorch compatibility matrix and resolves it correctly. Use pip only for pure-Python packages with no native deps.

**Why not one environment for everything?**
The Go control plane has no Python dependency and the Python worker has no Go dependency. Keeping them as separate services with separate Dockerfiles means you can update one without touching the other, and your Docker image layers are smaller and more cacheable.