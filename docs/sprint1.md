# Data Pipeline Orchestrator — Sprint 1
## Fault-Tolerant Raft Control Plane (HashiCorp Raft)
**Stack:** Go 1.25+ · hashicorp/raft · hashicorp/raft-boltdb · gRPC + protobuf · Prometheus

**Goal:** Stand up a working, instrumented Raft consensus layer across 3 control-plane nodes (2 in GCP, 1 in AWS) using the HashiCorp Raft library. The cluster must self-heal after node failures, replicate state correctly across the simulated WAN boundary, and emit the metrics needed for Sprint 4's cross-cloud analysis.

**Total effort:** 29 points

> ⚠️ **Latency heads-up:** The simulated AWS↔GCP link has ~50ms RTT and AWS↔Azure ~75ms RTT. HashiCorp Raft's default timeouts are tuned for single-datacenter deployments and will cause constant spurious elections across these links. All timing-related acceptance criteria and tech notes below account for this — pay close attention to the recommended config values.

---

## S1.1 — HashiCorp Raft Setup & Persistent Log Store
*8 points · Tags: Go, Raft*

**As a** control plane developer, **I want to** configure the HashiCorp Raft library with a BoltDB-backed persistent log store and a custom FSM skeleton, **so that** the cluster has a durable, production-grade consensus foundation that survives restarts without writing a Raft implementation from scratch.

### Acceptance Criteria
- [✅ ] `go get github.com/hashicorp/raft` and `go get github.com/hashicorp/raft-boltdb` added to `go.mod`
- [✅ ] A `RaftNode` wrapper struct in `internal/raft/node.go` initializes `hashicorp/raft` with BoltDB log store and stable store
- [✅ ] A `PipelineFSM` struct implements `raft.FSM` interface (`Apply`, `Snapshot`, `Restore`) — can be a skeleton for now
- [✅ ] BoltDB files persist to a mounted volume — verified by restarting a node and confirming it rejoins without data loss
- [✅  ] State transitions are logged via `slog` at INFO level by hooking into `raft.Config.Logger`
- [✅  ] `go test ./internal/raft/... -run TestNodeInit` passes — verifies the node starts and reaches a valid initial state

### Technical Notes
- Key config values to set in `raft.Config`: `HeartbeatTimeout: 500ms`, `ElectionTimeout: 1000ms`, `CommitTimeout: 50ms` — conservative for cross-cloud use
- Use `raft-boltdb.NewBoltStore(path)` for both `LogStore` and `StableStore` — one BoltDB file handles both
- The `PipelineFSM.Apply()` method receives committed log entries — for now just unmarshal the command and log it; real state mutations come in Sprint 2
- `raft.Config.Logger` accepts an `hclog.Logger` — wrap `slog` or just use `hclog.New()` directly for now
- Store BoltDB files in `/data/raft/` inside the container and mount that path as a Docker volume

---

## S1.2 — Leader Election & Cluster Bootstrap
*8 points · Tags: Go, Raft, Networking*

**As a** control plane developer, **I want to** bootstrap a 3-node Raft cluster so that exactly one node becomes leader after startup or after the current leader fails, **so that** the cluster is self-healing and always has a coordinator, even across the simulated multi-cloud boundary.

### Acceptance Criteria
- [ ✅] On startup, the cluster elects a leader within 20 seconds (verified by watching logs)
- [✅ ] If the leader container is killed (`docker stop`), a new leader is elected within 20 seconds
- [✅ ] Only one leader exists at any term — verified by querying `/raft-state` on all 3 nodes simultaneously
- [ ✅] Raft election events emitted as Prometheus metrics: `raft_elections_total`, `raft_term`, `raft_state` (0=Follower, 1=Candidate, 2=Leader)
- [✅ ] A `/raft-state` HTTP debug endpoint returns the node's current Raft state, leader address, and current term

### Technical Notes
- Use `raft.BootstrapCluster()` with a `raft.Configuration` listing all 3 peers — only call this once on first boot, check with `raft.HasExistingState()`
- Peer addresses come from env vars (`RAFT_PEERS=cp-aws-1:7000,cp-gcp-1:7000,cp-gcp-2:7000`) so compose can configure them
- Use `raft.NewTCPTransport` on port 7000 (separate from gRPC port 50051) — Raft has its own internal RPC mechanism
- `hashicorp/raft` exposes `raft.Stats()` map — poll this every 5s in a goroutine and update Prometheus gauges
- Election timeouts of 1000ms give ~950ms headroom above the 50ms cross-cloud RTT — avoids spurious elections while still recovering quickly

---

## S1.3 — FSM State Machine & Log Replication Verification
*8 points · Tags: Go, Raft, Networking*

**As a** control plane developer, **I want to** implement the `PipelineFSM` state machine and verify that log entries replicate correctly across all nodes, **so that** the cluster maintains consistent shared state regardless of which cloud a node is on and I have a foundation to build task scheduling on in Sprint 2.

### Acceptance Criteria
- [ ] `PipelineFSM` maintains an in-memory map of `WorkerInfo` entries, mutated by `Apply()` commands
- [ ] A `RegisterWorkerCommand` submitted via `raft.Apply()` on the leader replicates to all followers within 500ms under normal conditions
- [ ] An entry is only committed once a majority (2 of 3 nodes) acknowledges it — verified by pausing one node and confirming the other two still commit
- [ ] After replication, querying FSM state on all 3 nodes returns identical results
- [ ] `raft_replication_latency_ms` Prometheus histogram tracks time from `raft.Apply()` call to commit confirmation
- [ ] `FSMSnapshot` and `FSMRestore` work correctly — verified by a test that snapshots, wipes memory state, restores, and checks consistency

### Technical Notes
- Define commands as typed structs serialized to JSON: `{"type":"register_worker","payload":{...}}`
- `raft.Apply(cmd, timeout)` returns a future — call `.Error()` on it to confirm commit before responding to the caller
- The in-memory FSM state does NOT need a mutex if you only mutate it inside `Apply()` — Raft guarantees single-threaded delivery
- Snapshot frequency: set `raft.Config.SnapshotInterval = 30s` and `SnapshotThreshold = 100` for now
- Design `Apply()` to handle multiple command types via a switch statement from the start — Sprint 2 adds task manifest commands

---

## S1.4 — Worker Registration & Cluster State View
*5 points · Tags: Go, Python, Raft*

**As a** cluster operator, **I want to** have Python worker nodes register with the Raft leader and send periodic heartbeats, **so that** the control plane always has an accurate view of available workers before scheduling any Map/Reduce tasks.

### Acceptance Criteria
- [ ] Each Python worker sends a `RegisterWorker` gRPC call to the leader on startup, including its `worker_id`, address, and `cloud_tag` (`aws`|`gcp`|`azure`)
- [ ] `RegisterWorker` submits a `RegisterWorkerCommand` through `raft.Apply()` so worker state is replicated to all nodes
- [ ] Workers send a `Heartbeat` gRPC call every 5 seconds; the leader marks a worker offline if 3 consecutive heartbeats are missed
- [ ] `GET /cluster-state` returns a JSON list of all workers with `status`, `cloud_tag`, and `last_seen` timestamp
- [ ] If a worker container is stopped, cluster state reflects it as offline within 20 seconds
- [ ] A worker that restarts automatically re-registers without manual intervention

### Technical Notes
- Add `WorkerService` to `proto/worker.proto` — `RegisterWorker` and `Heartbeat` as separate RPCs
- Leader-only enforcement: if a follower receives `RegisterWorker`, return the current leader address via `raft.Leader()` so the worker retries against the right node
- Heartbeat tracking lives in a separate `map[string]*HeartbeatTracker` in the agent registry — this does NOT go through Raft (ephemeral liveness data, not durable state)
- The Python worker's `heartbeat.py` already has the threading skeleton from S0.3 — replace `_send_heartbeat()` stub with the real gRPC call
- Wire the `HeartbeatTracker` cleanup to mark workers offline and submit an `UpdateWorkerStatusCommand` through Raft so offline status is also replicated

---

## Sprint 1 — Deliverables Checklist

- [ ] HashiCorp Raft running across all 3 control-plane nodes with BoltDB persistence
- [ ] Leader election: new leader elected within 20 seconds of any node failure
- [ ] FSM replication: consistent state verified across all 3 nodes after commits
- [ ] Cross-cloud Raft: election and replication verified across AWS↔GCP (50ms) and AWS↔Azure (75ms) links
- [ ] Worker registration: Python workers register, heartbeat, and are marked offline correctly
- [ ] Prometheus metrics emitted: `raft_elections_total`, `raft_term`, `raft_state`, `raft_replication_latency_ms`
- [ ] Baseline latency numbers recorded — these are your RQ3 control measurements for Sprint 4

---

## What HashiCorp Handles vs What You Write

| Concern | HashiCorp handles | You write |
|---|---|---|
| Leader election | ✅ Built in | Timeout tuning + bootstrap config |
| Log replication | ✅ Built in | FSM `Apply()` logic |
| Persistent storage | ✅ Via BoltDB | Volume mount + path config |
| Snapshotting | ✅ Built in | `Snapshot()` + `Restore()` methods |
| Network transport | ✅ TCP transport built in | Peer address configuration |
| Metrics | ⚠️ `Stats()` map only | Prometheus wrapper |
| State machine | ❌ Not included | `PipelineFSM` — fully yours |

---

**Story dependency order:** S1.1 → S1.2 → S1.3 → S1.4. Do not start S1.2 until S1.1 node init test is green. S1.4 can begin once S1.2 has a stable leader — the Python gRPC work is independent of S1.3.