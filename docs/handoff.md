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
- Worker base image: `mambaorg/micromamba:1.5.8` — base conda was polluted, switched to micromamba


## micromamba Notes
- Base conda env was polluted/inconsistent — switched to micromamba permanently
- Binary lives at: `~/Projects/pipeline_orchestrator/bin/micromamba`
- Env lives at: `~/.local/share/mamba/envs/pipeline-worker`
- Activate with: `micromamba activate pipeline-worker`
- Worker Dockerfile uses `mambaorg/micromamba:1.5.8` base image
- curl installed inside the micromamba env via: `micromamba install -n pipeline-worker -c conda-forge curl`


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
|   |__ verifysprint0.sh   #test sprint0 is complete
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

## IP Address Map (important — duplicates caused bugs)
| Container | net-aws | net-gcp | net-azure |
|---|---|---|---|
| gateway | 10.10.0.254 | 10.20.0.254 | 10.30.0.254 |
| cp-aws-1 | 10.10.0.10 | 10.20.0.10 | 10.30.0.10 |
| cp-gcp-1 | 10.10.0.11 | 10.20.0.11 | 10.30.0.11 |
| cp-azure-1 | 10.10.0.13 | 10.20.0.13 | 10.30.0.13 |
| worker-aws-1 | 10.10.0.20 | - | - |
| worker-aws-2 | 10.10.0.21 | - | - |
| worker-gcp-1 | - | 10.20.0.20 | - |
| worker-azure-1 | - | - | 10.30.0.20 |
| minio | 10.10.0.100 | 10.20.0.100 | 10.30.0.100 |

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
- S0.4 ✅ complete — apply Makefile fix, run make sim-up, verify docker compose ps shows all 9 healthy
- S0.5 ✅ complete — MinIO is running in compose, finished bucket init script + smoke test
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
- [x] **S1.1** — HashiCorp Raft setup + BoltDB log store + PipelineFSM skeleton ✅
- [x] **S1.2** — Cluster bootstrap + leader election ✅
- [x] **S1.3** — Full PipelineFSM + replication verification ✅
- [x] **S1.4** — Worker registration + heartbeat via gRPC ✅

**Dependency order:** S1.1 → S1.2 → S1.3 → S1.4 (S1.4 can start once S1.2 is stable)

---

### S1.1 — HashiCorp Raft Setup & Persistent Log Store ✅

**What was built:**
- Added `github.com/hashicorp/raft v1.7.3` and `github.com/hashicorp/raft-boltdb v1` to `control-plane/go.mod`
- `internal/raft/node.go` — `RaftNode` struct wrapping `*hashiraft.Raft`; `NewRaftNode` creates TCP transport, BoltDB log+stable store, file snapshot store, and starts the node; internal `newRaftNodeWithTransport` constructor accepts any `hashiraft.Transport` so unit tests use `InmemTransport`
- `internal/raft/fsm.go` — `PipelineFSM` skeleton implementing `hashiraft.FSM` with empty `Apply`, `Snapshot`, `Restore`
- `internal/metrics/metrics.go` — Prometheus gauges: `raft_elections_total`, `raft_term`, `raft_state`
- `TestNodeInit` — single bootstrapped node elects itself leader within 10s

**Key config values** (`node.go`):
```
HeartbeatTimeout  = 500ms   (cross-cloud tuned)
ElectionTimeout   = 1000ms
CommitTimeout     = 50ms
SnapshotInterval  = 30s
SnapshotThreshold = 100
```

**BoltDB persistence:** files written to `cfg.DataDir` (env `RAFT_DATA_DIR`, default `/data/raft`); Docker volume `raft-data-*` mounted there so files survive container restarts.

---

### S1.2 — Cluster Bootstrap & Leader Election ✅

**What was built:**
- `raft.BootstrapCluster()` called only when `RAFT_BOOTSTRAP=true` AND `HasExistingState()` returns false — prevents re-bootstrap on restart
- `peersToServers()` converts the `RAFT_PEERS` CSV env var (`cp-aws-1:7000,cp-gcp-1:7000,cp-azure-1:7000`) into a `raft.Configuration` server list; falls back to single-node if peers is empty (used in unit tests)
- `TestBootstrapMultiNode` — 3-node cluster with `InmemTransport`, verifies exactly one leader elected within 15s
- `/raft-state` HTTP endpoint on all 3 nodes returns `{"node_id":"...","state":"...","leader":"...","term":N}`
- Prometheus stats polled every 5s in a background goroutine: `raft_state`, `raft_term`, `raft_elections_total` (incremented on term increase)

**Bugs fixed during S1.2:**

1. **`RAFT_BOOTSTRAPT=true` typo in `docker/docker-compose.yml`** — extra `T` on the env var name meant all nodes read an empty string from `RAFT_BOOTSTRAP`, never bootstrapped, stayed as Followers with term 0 forever. Fix: corrected to `RAFT_BOOTSTRAP=true`.

2. **TCP transport binding to a single interface** — `NewTCPTransportWithLogger(cfg.RaftAddr, ...)` where `cfg.RaftAddr` was e.g. `cp-aws-1:7000`. Inside a multi-network Docker container, `cp-aws-1` resolved to one specific interface IP (e.g. `10.30.0.13` on net-azure). Peers on the other two networks got "connection refused". Fix: bind to `":"+port` (all interfaces, `0.0.0.0`), advertise the full `hostname:port` separately as the `TCPAddr` argument.

**Verification:** `bash scripts/sprint1_verification.sh s1.2` — all checks green including leader election within 20s, exactly 1 leader, all 3 `/raft-state` endpoints, Prometheus metrics, and leader failover within 20s.

---

### S1.3 — Full PipelineFSM & Log Replication ✅

**What was built:**

**`internal/raft/fsm.go`** — full FSM replacing the skeleton:
- `WorkerInfo` struct: `ID`, `Address`, `CloudTag`, `Status`, `LastSeen`
- Typed command envelope: `Command{Type CommandType, Payload json.RawMessage}` serialized to `{"type":"register_worker","payload":{...}}`
- `CommandType` constants: `CmdRegisterWorker`, `CmdUpdateWorkerStatus`
- `PipelineFSM.Apply()` dispatches on command type via switch; `applyRegisterWorker` upserts a worker with `Status: "online"`; `applyUpdateWorkerStatus` mutates `Status` and `LastSeen`
- `sync.RWMutex` on the workers map — Raft calls `Apply()` serially so no lock is needed there, but external HTTP readers (goroutines) hold `RLock`
- `Snapshot()` deep-copies the map under `RLock`, JSON-marshals it, returns a `pipelineFSMSnapshot`
- `Restore()` JSON-decodes into a fresh map under `WLock`, replacing all state
- `Workers()` and `GetWorker(id)` — safe copy helpers for external readers
- `MarshalCommand()` convenience helper used by both production code and tests
- `var _ hashiraft.FSM = (*PipelineFSM)(nil)` compile-time interface check

**`internal/raft/node.go`** — added `Apply(cmd []byte, timeout time.Duration) error` method that calls `n.raft.Apply()` and records elapsed time in `metrics.RaftReplicationLatencyMs`

**`internal/metrics/metrics.go`** — added `raft_replication_latency_ms` Histogram with exponential buckets `1ms → ~4096ms`

**`cmd/orchestrator/main.go`** — `fsm` created before `raftNode` so the `/cluster-state` handler can read `fsm.Workers()` and return the full worker registry as JSON

**`/cluster-state` HTTP endpoint** returns:
```json
{"node_id":"cp-aws-1","state":"Leader","workers":[...]}
```

**Test suite** (`internal/raft/raft_test.go`):
- `makeCluster(t, n)` — creates n-node `InmemTransport` cluster, fully meshed and bootstrapped, with `t.Cleanup` shutdown
- `waitForLeader(t, nodes, timeout)` — polls until one node reaches `hashiraft.Leader`
- `TestFSMApply` — directly calls `fsm.Apply()`, verifies register + status-update mutations
- `TestFSMSnapshotRestore` — 3 workers added, snapshot taken, fresh FSM restored, all 3 workers verified consistent
- `TestReplicationToFollowers` — leader `Apply()` call, all 3 FSMs must reflect the entry within 500ms
- `TestMajorityQuorum` — one follower isolated with `InmemTransport.DisconnectAll()`, `Apply()` still succeeds (2/3 majority), isolated node catches up after reconnect

**Bugs fixed during S1.3:**

1. **`cmd/orchestrator/main_test.go` wrong package** — file had `package raft` (copy-paste from `internal/raft/raft_test.go`) placed in `cmd/orchestrator/`, causing `found packages main and raft` build error. Fix: replaced with a proper `package main` test covering `envOr`, `boolEnv`, and `splitCSV`.

2. **`cmd/orchestrator/main.go` garbled `NewRaftNode` call** — `fsm :=` assignment was embedded inside the constructor argument list, and had a duplicate/corrupted block below it. Also contained `internraft` typo (missing `al`). Fix: rewrote the file cleanly.

3. **`TestMajorityQuorum` race condition** — `raft.Apply()` returning success means the log entry is *committed* (majority acknowledged), but follower FSMs dispatch the entry asynchronously after the commit. Checking follower FSMs immediately after `Apply()` returned a false negative. Fix: added a 500ms poll loop after `Apply()` before asserting connected-follower FSMs, mirroring the pattern in `TestReplicationToFollowers`. Also fixed a `deadline :=` redeclaration compile error caused by the same variable name being used twice in the same function scope.

**Verification:** `bash scripts/sprint1_verification.sh s1.3` — all acceptance criteria green, including two checks added at the end of S1.4 work:
- **AC2 re-election test** (Docker): stops the current Raft leader, polls the remaining two nodes for a new leader, asserts it emerges within 20s, then restarts the stopped node and confirms it rejoins
- **AC5+ metrics spot-check** (Docker): re-fetches `/metrics` after the election, verifies `raft_state`, `raft_term`, `raft_elections_total`, and `raft_replication_latency_ms_count` all have live numeric values

---

### S1.4 — Worker Registration & Heartbeat via gRPC ✅

**What was built:**

**`proto/worker.proto`** — defines `WorkerService` with two RPCs:
- `RegisterWorker(RegisterWorkerRequest) → RegisterWorkerResponse` — initial worker registration; `leader_addr` in the response is the follower-redirect address
- `Heartbeat(HeartbeatRequest) → HeartbeatResponse` — periodic liveness ping with same redirect semantics

**`scripts/proto-gen.sh`** — generates Go stubs into `control-plane/internal/gen/worker/` and Python stubs into `worker/worker/gen/`; fixes the bare `import worker_pb2` → relative import in `worker_pb2_grpc.py`

**`control-plane/internal/agent/registry.go`** — `AgentRegistry` implementing `WorkerServiceServer`:
- `RaftApplier` interface narrows `*RaftNode` to just `Apply / Leader / LeaderID / State` — keeps the registry unit-testable without a real cluster
- `HeartbeatTracker` (in-memory, not Raft-replicated) tracks `LastSeen` and `MarkedOffline` per worker
- `RegisterWorker` RPC: leader-only; applies `CmdRegisterWorker` via Raft; followers redirect
- `Heartbeat` RPC: leader-only; resets `LastSeen` and `MarkedOffline`; creates tracker on first heartbeat after failover
- `checkHeartbeats()`: called every 5s by `monitorLoop`; marks workers with `>15s` silence offline via `CmdUpdateWorkerStatus`; `MarkedOffline=true` set under lock before releasing to prevent duplicate Apply calls
- `raftAddrToGRPC()`: converts Raft peer address to gRPC address for redirects; if the host is an IP (see bug below), falls back to `LeaderID()` (the hostname)

**`control-plane/internal/raft/node.go`** — added `LeaderID() string` using `raft.LeaderWithID()`; returns the leader's server ID (e.g. `"cp-gcp-1"`), which Docker DNS resolves correctly on every subnet

**`cmd/orchestrator/main.go`** — wires `AgentRegistry` into the gRPC server (`workerpb.RegisterWorkerServiceServer`) and manages its context lifecycle

**`worker/worker/heartbeat.py`** — full rewrite with real gRPC:
- `_register_with_retry()` loops until stop event or success; retries on gRPC error after 5s sleep
- `_register()` returns `True` on success, `False` on follower redirect (updates `_orchestrator_addr`), raises on hard error
- `_send_heartbeat()` wraps the entire call in `try/except` — never raises, silently follows redirects
- `worker_addr` kwarg defaults to `worker_id` for backward compatibility with existing tests

**`worker/worker/main.py`** — reads `WORKER_ADDR` env var; falls back to `{WORKER_ID}:{HTTP_PORT}` if unset; passes it to `HeartbeatClient`

**`docker/docker-compose.yml`** — added `WORKER_ADDR=<service>:8081` to all 4 worker services

**`worker/pyproject.toml`** — added `pythonpath = ["."]` to `[tool.pytest.ini_options]` to fix `No module named 'worker'` import error in pytest

**Test suite:**
- 13 Go unit tests in `internal/agent/registry_test.go` using `mockRaft`: RegisterWorker/Heartbeat on leader and follower, redirect with and without known leader, heartbeat-reset of MarkedOffline, checkHeartbeats marks stale/no-spam/skips-on-follower, IP-based redirect fallback to LeaderID
- 9 Python mock-based tests in `worker/tests/test_heartbeat_grpc.py`: register success/redirect/gRPC error, heartbeat success/redirect/gRPC error/unexpected error, worker_addr fallback, explicit worker_addr

**Bugs fixed during S1.4:**

1. **IP-based Raft leader redirect unreachable from isolated worker networks** — `net.ResolveTCPAddr("tcp", cfg.RaftAddr)` in `node.go` resolves the hostname (e.g. `cp-gcp-1:7000`) to an IP at transport-creation time. Because hashicorp/raft includes the transport's `LocalAddr()` in heartbeat messages, followers learn the leader's address as an IP (e.g. `10.10.0.11:7000`) rather than the configured hostname. Workers on isolated Docker networks (e.g. `worker-azure-1` on `net-azure` only) cannot route to IPs on other subnets, so the gRPC redirect timed out with `DEADLINE_EXCEEDED`. Fix: `raftAddrToGRPC()` now detects when the host is an IP via `net.ParseIP()` and falls back to `r.raft.LeaderID()` (the Raft server ID = hostname) which Docker DNS resolves correctly on all networks.

2. **Stale Raft state from previous test runs** — Docker named volumes (`raft-data-aws-1` etc.) persist between `docker compose down` / `up` cycles. A `test-worker` entry left from a prior run was being counted as one of the 4 expected workers, masking `worker-azure-1`'s absence. Fix: `s1_4()` in the verification script now runs `docker compose down --volumes` before bringing the cluster up, ensuring a clean Raft log each time.

**Verification:** `bash scripts/sprint1_verification.sh s1.4` — all 6 acceptance criteria green: 4/4 workers online, all 4 named workers present in cluster-state, worker-aws-1 marked offline within 25s of container stop, worker-aws-1 auto-reregisters after restart.

## Key Commands
```bash
# Environment
micromamba activate pipeline-worker

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
docker logs gateway  --follow

# Health checks
curl http://localhost:8080/health        # cp-aws-1
curl http://localhost:8083/health        # cp-gcp-1
curl http://localhost:8085/health        # cp-azure-1
curl http://localhost:9001          # MinIO console
```

## Immediate Next Steps
**Sprint 1 is complete.** All S1.1–S1.4 deliverables verified green. Merge the `feature/s1.4-worker-registration` PR to `main`, then start **Sprint 2**.

Sprint 2 will likely need:
1. `git checkout main && git pull && git checkout -b feature/s2.x-<name>`
2. Task scheduling — define `proto/task.proto`; implement a scheduler in `internal/scheduler/` that assigns tasks to registered workers via the `PipelineFSM`
3. Worker task execution — Python workers receive tasks via gRPC, run them, report results
4. Storage integration — workers read/write task artifacts to MinIO (`pipeline-data` bucket)
5. End-to-end smoke test: submit a task through the control plane, verify it executes on a worker and the result lands in MinIO


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