# Pipeline Orchestrator —  Handoff Document

## Project Summary
A research-grade distributed ML pipeline orchestrator simulating AWS/GCP/Azure using Docker.
- **Control plane:** Go 1.25 + HashiCorp Raft (consensus) + gRPC
- **Workers:** Python 3.11 + FastAPI + conda env named `pipeline-worker`
- **Storage:** MinIO (S3-compatible)
- **Repo:** `~/Projects/pipeline_orchestrator`
- **GitHub:** `github.com/joelcrouch/pipeline-orchestrator`

## Key Decisions Made
- Using **HashiCorp Raft** (`github.com/hashicorp/raft` + `raft-boltdb`) NOT a from-scratch implementation
- **3-node Raft quorum — one node per cloud** (cp-aws-1, cp-gcp-1, cp-azure-1)
- Docker bridge networks simulate clouds: `net-aws` (10.10.0.0/24), `net-gcp` (10.20.0.0/24), `net-azure` (10.30.0.0/24)
- tc-netem on gateway: AWS↔GCP ~50ms, AWS↔Azure ~75ms, GCP↔Azure ~125ms
- Control plane final Docker stage: `alpine:3.19` (not distroless) so curl/wget work for healthchecks
- `on_event` deprecation warnings in FastAPI are known and harmless — will fix to `lifespan` pattern later

## Project Structure
```
pipeline-orchestrator/
├── control-plane/          # Go — HashiCorp Raft + gRPC server
│   ├── cmd/orchestrator/main.go
│   ├── internal/raft/      # S1.1-S1.3 work goes here
│   ├── internal/agent/     # S1.4 worker registry
│   ├── internal/scheduler/ # Sprint 2
│   ├── internal/storage/   # Sprint 2
│   ├── internal/metrics/   # Prometheus
│   ├── Dockerfile          # alpine:3.19 final stage, 28.4MB
│   ├── go.mod              # module: github.com/joelcrouch/pipeline-orchestrator/control-plane
│   └── .golangci.yml       # golangci-lint v2.10.1 config
├── worker/                 # Python — FastAPI workers
│   ├── worker/main.py      # FastAPI app, /health, /status endpoints
│   ├── worker/heartbeat.py # HeartbeatClient (stub, real gRPC in S1.4)
│   ├── tests/              # 7 tests, all passing
│   ├── environment.yml     # conda env: pipeline-worker
│   ├── requirements-pip.txt # prometheus-client (not on conda-forge)
│   └── Dockerfile          # continuumio/miniconda3 base
├── docker/
│   ├── docker-compose.yml  # 9-container cluster (see below)
│   ├── gateway/
│   │   ├── Dockerfile      # alpine:3.19 + iproute2 + iputils + bash
│   │   └── entrypoint.sh   # tc-netem latency rules, subnet-based iface detection
│   └── init/init-buckets.sh
├── proto/                  # .proto files (empty stubs, filled in S1.x)
│   ├── raft.proto
│   ├── worker.proto
│   └── task.proto
├── scripts/
│   ├── sim-latency.sh      # test/status/set cross-cloud latency
│   └── proto-gen.sh
├── data/                   # gitignored, .gitkeep files present
├── Makefile
├── .env / .env.example
└── .gitignore
```

## Cluster Topology (docker/docker-compose.yml)
| Container | Cloud | Role | Networks | Host Ports |
|---|---|---|---|---|
| gateway | - | WAN sim + tc-netem | all 3 | - |
| cp-aws-1 | AWS | Raft peer 1 | all 3 | HTTP:8080, gRPC:50051 |
| cp-gcp-1 | GCP | Raft peer 2 | aws+gcp+azure | HTTP:8083, gRPC:50052 |
| cp-azure-1 | Azure | Raft peer 3 | aws+gcp+azure | HTTP:8085, gRPC:50054 |
| worker-aws-1 | AWS | worker | net-aws | - |
| worker-aws-2 | AWS | worker | net-aws | - |
| worker-gcp-1 | GCP | worker | net-gcp | - |
| worker-azure-1 | Azure | worker | net-azure | - |
| minio | - | S3 storage | all 3 | 9000, 9001 |

Raft peer list: `cp-aws-1:7000,cp-gcp-1:7000,cp-azure-1:7000`

## Sprint 0 Status
- [x] **S0.1** — Docker networks + gateway + tc-netem latency ✅
  - AWS intra: <1ms, AWS↔GCP ~50ms, AWS↔Azure ~75ms verified
- [x] **S0.2** — Go scaffold: build/test/lint all green, 28.4MB Docker image ✅
  - `make build`, `make test`, `make lint` (golangci-lint v2.10.1, 0 issues)
- [x] **S0.3** — Python worker: FastAPI /health, 7/7 pytest passing ✅
  - conda env `pipeline-worker` created at `~/.local/share/mamba/envs/pipeline-worker`
- [x] **S0.4** — 9-container compose file, all 9 healthy ✅ **COMPLETE**
  - Two bugs fixed: duplicate IP on net-azure + curl missing from worker image (see details below)
- [x] **S0.5** — MinIO smoke test passing from all 3 clouds ✅ **COMPLETE**
  - `mc-init` one-shot container creates `pipeline-data` bucket on startup
  - `bash scripts/test-storage.sh` — PUT/GET 1MB verified from aws, gcp, azure workers

### S0.4 bugs fixed
**1. Duplicate IP on net-azure (`docker/docker-compose.yml`)**
- `cp-gcp-1` and `cp-azure-1` both had `ipv4_address: 10.30.0.11` on `net-azure`
- Race on startup: whichever claimed it second got "Address already in use"
- Stale bridge deletions (`sudo ip link delete br-*`) were red herrings — IP conflict was the real cause
- Fix: changed `cp-azure-1` net-azure to `10.30.0.13` (consistent with its `.13` on net-aws/net-gcp)

**2. `curl` missing from micromamba base image (`worker/Dockerfile`)**
- Worker healthcheck used `curl` but `mambaorg/micromamba:1.5.8` ships without it
- Fix: `RUN micromamba install -n pipeline-worker -c conda-forge curl -y && micromamba clean -afy`

- [ ] **S0.4** — 9-container compose file **HISTORY/NOTES BELOW**
  - compose file written, currently running `make sim-up` to verify
  - worker Dockerfile build (conda install) takes several minutes
  - need to verify all 9 containers reach healthy status
  - Using micromamba (NOT conda) — base conda env was polluted/inconsistent
    - micromamba installed at ~/Projects/pipeline_orchestrator/bin/micromamba
    - env lives at ~/.local/share/mamba/envs/pipeline-worker
    - activate with: micromamba activate pipeline-worker

S0.4 IN PROGRESS: stale kernel bridge interfaces cause "Address already in use" 
  on every sim-up. Fix: manually delete with `sudo ip link delete br-<id>` before 
  each sim-up until Makefile fix is applied.
- Makefile sim-down target needs bridge cleanup loop (see below)
makefilesim-down:
    docker compose -f docker/docker-compose.yml down -v --remove-orphans
    @for br in $$(ip link show type bridge | awk -F': ' '{print $$2}' | grep '^br-'); do \
        if ! docker network ls --format '{{.ID}}' | grep -q $${br#br-}; then \
            sudo ip link delete $$br 2>/dev/null || true; \
        fi \
    done
```

there was a red herring in the errors, but we got there:
docker compose -f docker/docker-compose.yml ps
NAME             IMAGE                   COMMAND                  SERVICE          CREATED          STATUS                    PORTS
cp-aws-1         docker-cp-aws-1         "/orchestrator"          cp-aws-1         12 minutes ago   Up 12 minutes (healthy)   0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp, 0.0.0.0:50051->50051/tcp, [::]:50051->50051/tcp
cp-azure-1       docker-cp-azure-1       "/orchestrator"          cp-azure-1       12 minutes ago   Up 12 minutes (healthy)   0.0.0.0:8085->8080/tcp, [::]:8085->8080/tcp, 0.0.0.0:50054->50051/tcp, [::]:50054->50051/tcp
cp-gcp-1         docker-cp-gcp-1         "/orchestrator"          cp-gcp-1         12 minutes ago   Up 12 minutes (healthy)   0.0.0.0:8083->8080/tcp, [::]:8083->8080/tcp, 0.0.0.0:50052->50051/tcp, [::]:50052->50051/tcp
gateway          docker-gateway          "/entrypoint.sh"         gateway          12 minutes ago   Up 12 minutes (healthy)   
minio            minio/minio:latest      "/usr/bin/docker-ent…"   minio            12 minutes ago   Up 12 minutes (healthy)   0.0.0.0:9000-9001->9000-9001/tcp, [::]:9000-9001->9000-9001/tcp
worker-aws-1     docker-worker-aws-1     "/usr/local/bin/_ent…"   worker-aws-1     20 seconds ago   Up 18 seconds (healthy)   8081/tcp
worker-aws-2     docker-worker-aws-2     "/usr/local/bin/_ent…"   worker-aws-2     20 seconds ago   Up 18 seconds (healthy)   8081/tcp
worker-azure-1   docker-worker-azure-1   "/usr/local/bin/_ent…"   worker-azure-1   20 seconds ago   Up 18 seconds (healthy)   8081/tcp
worker-gcp-1     docker-worker-gcp-1     "/usr/local/bin/_ent…"   worker-gcp-1     20 seconds ago   Up 18 seconds (healthy)   8081/tcp
bash scripts/sim-latency.sh test 
── Latency test ─────────────────────────────────

▶ Intra-cloud (AWS→AWS): expect <1ms
PING 10.10.0.1 (10.10.0.1) 56(84) bytes of data.
64 bytes from 10.10.0.1: icmp_seq=1 ttl=64 time=0.062 ms
64 bytes from 10.10.0.1: icmp_seq=2 ttl=64 time=0.070 ms
64 bytes from 10.10.0.1: icmp_seq=3 ttl=64 time=0.061 ms
64 bytes from 10.10.0.1: icmp_seq=4 ttl=64 time=0.064 ms

--- 10.10.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3081ms
rtt min/avg/max/mdev = 0.061/0.064/0.070/0.003 ms

▶ Cross-cloud (AWS→GCP): expect ~50ms
PING 10.20.0.1 (10.20.0.1) 56(84) bytes of data.
64 bytes from 10.20.0.1: icmp_seq=1 ttl=64 time=93.4 ms
64 bytes from 10.20.0.1: icmp_seq=2 ttl=64 time=47.3 ms
64 bytes from 10.20.0.1: icmp_seq=3 ttl=64 time=52.2 ms
64 bytes from 10.20.0.1: icmp_seq=4 ttl=64 time=49.6 ms

--- 10.20.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3004ms
rtt min/avg/max/mdev = 47.308/60.625/93.449/19.028 ms

▶ Cross-cloud (AWS→Azure): expect ~75ms
PING 10.30.0.1 (10.30.0.1) 56(84) bytes of data.
64 bytes from 10.30.0.1: icmp_seq=1 ttl=64 time=154 ms
64 bytes from 10.30.0.1: icmp_seq=2 ttl=64 time=82.7 ms
64 bytes from 10.30.0.1: icmp_seq=3 ttl=64 time=66.5 ms
64 bytes from 10.30.0.1: icmp_seq=4 ttl=64 time=68.4 ms

--- 10.30.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3002ms
rtt min/avg/max/mdev = 66.465/92.820/153.784/35.750 ms
(pipeline-worker) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ 

wo changes fixed everything:                                                                                                                                  
                  
  1. docker/docker-compose.yml — duplicate IP on net-azure                                                                                                       
  - cp-gcp-1 and cp-azure-1 both had ipv4_address: 10.30.0.11 on net-azure                                                                                       
  - This caused a race: whichever container claimed it second got "Address already in use"                                                                       
  - Fix: changed cp-azure-1's net-azure address to 10.30.0.13 (consistent with its .13 on net-aws and net-gcp)                                                   
  - The stale bridge deletions (sudo ip link delete br-*) were red herrings — the IP conflict was the real cause all along

  2. worker/Dockerfile — curl not in micromamba base image
  - Healthcheck was CMD curl -f http://localhost:8081/health but mambaorg/micromamba:1.5.8 ships without curl
  - Fix: added RUN micromamba install -n pipeline-worker -c conda-forge curl -y && micromamba clean -afy before the COPY worker/ step

**3. micromamba notes:**
```
- Base conda env was polluted — switched to micromamba
- micromamba binary: ~/Projects/pipeline_orchestrator/bin/micromamba
- env lives at: ~/.local/share/mamba/envs/pipeline-worker
- activate with: micromamba activate pipeline-worker
- worker Dockerfile now uses mambaorg/micromamba:1.5.8 base image
```

**4. Update sprint status:**
```
- S0.1 ✅ complete
- S0.2 ✅ complete  
- S0.3 ✅ complete
- S0.4 🔄 in progress — apply Makefile fix, run make sim-up, verify docker compose ps shows all 9 healthy
- S0.5 ⏳ not started — MinIO is running in compose, just needs bucket init script + smoke test
```

**5. First thing to do in new conversation:**
```
1. Apply the Makefile sim-down fix
2. make sim-down && make sim-up
3. docker compose -f docker/docker-compose.yml ps  (verify all 9 healthy)
4. Tick off S0.4, move to S0.5 (MinIO bucket init)
5. Then Sprint 1: go get github.com/hashicorp/raft
- [ ] **S0.5** — MinIO smoke test (included in compose, needs bucket init + test script)

## Sprint 1 Plan (HashiCorp Raft)
- [ ] **S1.1** — HashiCorp Raft setup + BoltDB log store + PipelineFSM skeleton
  - `go get github.com/hashicorp/raft` + `go get github.com/hashicorp/raft-boltdb`
  - Key config: HeartbeatTimeout=500ms, ElectionTimeout=1000ms (cross-cloud tuned)
  - BoltDB files at `/data/raft/` inside container (volume mounted)
- [ ] **S1.2** — Cluster bootstrap + leader election
  - `raft.BootstrapCluster()` with 3 peers, check `raft.HasExistingState()` first
  - TCP transport on port 7000 (separate from gRPC 50051)
  - Prometheus: `raft_elections_total`, `raft_term`, `raft_state`
- [ ] **S1.3** — PipelineFSM state machine + replication verification
  - FSM holds `map[string]*WorkerInfo`, mutated by typed JSON commands
  - `RegisterWorkerCommand`, `UpdateWorkerStatusCommand`
  - Prometheus: `raft_replication_latency_ms` histogram
- [ ] **S1.4** — Worker registration + heartbeat via gRPC
  - `proto/worker.proto`: RegisterWorker + Heartbeat RPCs
  - Python `heartbeat.py` stub → real gRPC call
  - Leader redirects followers via `raft.Leader()`

## Key Commands
```bash
# Environment
conda activate pipeline-worker

# Build & test
make build        # go build ./... in control-plane/
make test         # go test + python -m pytest
make lint         # golangci-lint + ruff (ruff commented out until pip deps installed)

# Cluster
make sim-up       # docker compose up --build -d
make sim-down     # docker compose down
bash scripts/sim-latency.sh test    # verify latency
bash scripts/sim-latency.sh status  # show tc rules

# Logs
docker logs cp-aws-1 --follow
docker logs gateway

# Health checks
curl http://localhost:8080/health        # cp-aws-1
curl http://localhost:8083/health        # cp-gcp-1
curl http://localhost:8085/health        # cp-azure-1
```

## Immediate Next Steps
1. Sprint 0 complete — start Sprint 1 with S1.1
2. First thing: `cd control-plane && go get github.com/hashicorp/raft && go get github.com/hashicorp/raft-boltdb && go mod tidy`
3. Work goes in `control-plane/internal/raft/`

## Ports Quick Reference
| Service | Port | Purpose |
|---|---|---|
| cp-aws-1 HTTP | 8080 | /health, /cluster-state, /raft-state |
| cp-gcp-1 HTTP | 8083 | same |
| cp-azure-1 HTTP | 8085 | same |
| cp-aws-1 gRPC | 50051 | WorkerService, RaftService |
| cp-gcp-1 gRPC | 50052 | same |
| cp-azure-1 gRPC | 50054 | same |
| MinIO S3 | 9000 | pipeline-data bucket |
| MinIO console | 9001 | http://localhost:9001 |
| Workers HTTP | 8081 | /health (internal only, no host mapping) |