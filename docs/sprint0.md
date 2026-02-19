# Data Pipeline Orchestrator — Sprint 0
## Environment & Scaffolding
**Stack:** Go 1.25+ · Python 3.11+ · Docker Compose · gRPC + protobuf · MinIO · Prometheus

**Goal:** Get every developer to a state where they can run a 6-node simulated multi-cloud cluster locally, with Go and Python services building and passing tests. No Raft logic yet — just solid infrastructure.

**Total effort:** 19 points

---

## S0.1 — Simulated Multi-Cloud Docker Network
*5 points · Tags: Docker, Networking, Infra*

**As a** developer, **I want to** spin up isolated Docker networks that simulate AWS and GCP VPCs with realistic cross-cloud latency, **so that** I can develop and test distributed behavior locally without spending cloud credits.

### Acceptance Criteria  **DONE**
- [ ] `docker-compose up` creates three named bridge networks: `net-aws` and `net-gcp` and `net-azure`
- [ ] A `tc-netem` sidecar or iptables rule imposes configurable latency (default 50ms RTT) on cross-network traffic
- [ ] Containers on `net-aws` cannot reach containers on `net-gcp` except through the gateway node  and `net-azure`
- [ ] Running `ping` from an AWS node to a GCP node shows ~50ms RTT; same-network ping is <1ms
- [ ] A single `make sim-up` command starts the full environment; `make sim-down` tears it down cleanly

### Technical Notes
- Use Docker bridge networks with `--subnet` for distinct CIDR ranges (e.g. `10.10.0.0/24` for AWS, `10.20.0.0/24` for GCP)
- `tc qdisc add dev eth0 root netem delay 50ms 5ms` can be run in a privileged init container
- Consider a gateway/router container attached to both networks as the cross-cloud hop
- Document the simulated topology in a README diagram so the team always knows which container = which cloud role

---

## S0.2 — Go Control Plane Skeleton & Build Pipeline  **DONE**
*3 points · Tags: Go, Infra*

**As a** developer, **I want to** scaffold the Go module for the control plane with a working build, lint, and test pipeline, **so that** every contributor can run `go build`, `go test ./...` and get a green baseline from day one.

### Acceptance Criteria
- [ ] `go build ./...` compiles without errors from a fresh clone
- [ ] `go test ./...` passes with at least one placeholder test per package
- [ ] `golangci-lint run` returns zero issues on the scaffold
- [ ] A Dockerfile builds the control plane binary into a minimal `scratch` or `distroless` image under 50MB
- [ ] `make build`, `make test`, and `make lint` targets all work from the repo root

### Technical Notes
- Use Go 1.22+ with modules. Suggested package layout: `cmd/orchestrator`, `internal/raft`, `internal/agent`, `internal/storage`
- Use `slog` (stdlib) for structured logging from the start — retrofitting logging is painful
- Wire up a minimal gRPC server in `cmd/orchestrator/main.go` so the container has a real entrypoint
- Pin `golangci-lint` version in a `.golangci.yml` to avoid CI drift

---

## S0.3 — Python Worker Skeleton & Packaging  **DONE**
*3 points · Tags: Python, Infra*

**As a** developer, **I want to** scaffold the Python worker service with a working Dockerfile, dependency management, and test harness, **so that** the data-plane worker can be iterated on independently and deployed as a container image.

### Acceptance Criteria
- [ ] `uv` or `pip install -r requirements.txt` installs all deps without conflicts
- [ ] `python -m pytest` passes with at least one smoke test
- [ ] `docker build` produces an image that starts the worker and logs `Worker ready` to stdout
- [ ] The worker exposes a `/health` HTTP endpoint returning `200 OK` (use FastAPI or Flask)
- [ ] Worker accepts `ORCHESTRATOR_ADDR` env var to know where the control plane lives

### Technical Notes
- Use `pyproject.toml` + `uv` for dependency management — faster installs in Docker than pip
- FastAPI + uvicorn is recommended: gives async support and auto-docs which help during debugging
- Structure: `worker/main.py`, `worker/tasks/`, `worker/storage/` to mirror control-plane packages
- The `/health` endpoint will be used by Docker healthcheck and later by the Raft leader for node status

---

## S0.4 — 9-Container Cluster Compose File with Health Checks
*6 points · Tags: Docker, Infra*

**As a** developer, **I want to** define a `docker-compose.yml` that launches 3 Go control-plane nodes (one per cloud) and 4 Python worker nodes across three simulated cloud networks, **so that** I have a reproducible multi-cloud cluster to run Raft and MapReduce experiments against without touching real cloud infrastructure.

### Acceptance Criteria
- [ ] `docker compose up` starts exactly 9 named containers: `cp-aws-1`, `cp-gcp-1`, `cp-azure-1` (control plane) + `worker-aws-1`, `worker-aws-2`, `worker-gcp-1`, `worker-azure-1` (workers) + `gateway` + `minio`
- [ ] Each container has a Docker `HEALTHCHECK` that orchestration waits on before marking it healthy
- [ ] `docker compose ps` shows all 9 containers as healthy within 90 seconds of startup
- [ ] Control-plane nodes are attached to all three networks so Raft traffic crosses all simulated cloud boundaries
- [ ] Worker nodes are on their respective single network only
- [ ] Environment variables for inter-service addresses use Docker service-name DNS (e.g. `cp-aws-1:50051`)
- [ ] Startup order enforced: `gateway` → control plane nodes → workers

### Technical Notes
- Use `depends_on` with `condition: service_healthy` so workers only start after their local control plane node is ready
- Set resource limits (`cpus`, `memory`) per container — prevents runaway containers on a laptop
- Use `.env` file for tunable params: `RAFT_HEARTBEAT_MS`, `RAFT_ELECTION_TIMEOUT_MS`, `CROSS_CLOUD_LATENCY_MS`, `AZURE_LATENCY_MS`
- Control plane healthcheck uses `curl` via alpine base image (distroless has no shell tools)
- The 3-node Raft quorum (one per cloud) means any single cloud failure still allows commits — 2/3 nodes remain
---

## S0.5 — Shared Durable Storage Simulation (MinIO)
*3 points · Tags: Infra, Python, Go*

**As a** developer, **I want to** run a local MinIO instance accessible to all nodes so workers can write intermediate results to S3-compatible storage, **so that** the MapReduce pipeline can write and read data exactly as it would against real S3/GCS without network egress costs.

### Acceptance Criteria
- [ ] A `minio` container starts as part of docker-compose and is reachable from all 6 nodes
- [ ] Both Go (`aws-sdk-go-v2`) and Python (`boto3`) clients can PUT and GET objects using `MINIO_ENDPOINT` env var
- [ ] A smoke test script (`scripts/test-storage.sh`) writes a 1MB file and reads it back, printing the checksum
- [ ] Bucket `pipeline-data` is created automatically on first startup via MinIO init container or `mc alias`
- [ ] MinIO data directory is volume-mounted so data survives `docker compose restart`

### Technical Notes
- Use `minio/minio:latest` with `MINIO_ROOT_USER` and `MINIO_ROOT_PASSWORD` env vars
- Set `AWS_ENDPOINT_URL` in all containers pointing to `http://minio:9000` — boto3 and aws-sdk respect this
- For GCS simulation, the same MinIO works fine — just treat it as the neutral cross-cloud store
- MinIO console (port 9001) is useful for debugging — expose it on localhost only

---

## Sprint 0 — Deliverables Checklist

- [ ] Reproducible 6-node cluster launches with a single `make sim-up` command
- [ ] Cross-cloud latency simulation verified (AWS↔GCP RTT ~50ms, intra-cloud <1ms)
- [ ] Go control plane scaffold: builds, lints, and tests green
- [ ] Python worker scaffold: installs, tests pass, `/health` endpoint returns 200
- [ ] MinIO storage accessible from all nodes, verified by smoke test
- [ ] Team README documents how to run the environment and what each container represents

---

**Dependency note:** S0.1 (Docker network) must complete before S0.4 (cluster compose). S0.2 and S0.3 can run in parallel with S0.1. S0.5 can be added to the compose file once S0.4 is stable.