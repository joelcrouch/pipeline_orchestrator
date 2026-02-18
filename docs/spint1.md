# Data Pipeline Orchestrator — Sprint 1
## Fault-Tolerant Raft Control Plane
**Stack:** Go 1.22+ · gRPC + protobuf · Prometheus

**Goal:** Implement a working Raft consensus layer in Go across the 3 control-plane nodes (2 in GCP, 1 in AWS). The cluster must self-heal after a node failure and maintain log consistency across the simulated WAN boundary.

**Total effort:** 29 points

> ⚠️ **Latency heads-up:** The simulated AWS↔GCP link has ~50ms RTT. Raft's paper defaults (election timeout 150–300ms, heartbeat ~50ms) are tuned for single-datacenter deployments and will cause constant spurious elections across this link. All timing-related acceptance criteria and tech notes below account for this — pay attention to the recommended values.

---

## S1.1 — Raft Core Data Structures & Persistent Log
*8 points · Tags: Go, Raft*

**As a** control plane developer, **I want to** implement the foundational Raft data structures — Log, Term counter, and node State machine — with log entries persisted to disk, **so that** the cluster has a durable, well-defined state that survives restarts and forms the basis for all consensus operations.

### Acceptance Criteria
- [ ] `RaftNode` struct has fields: `currentTerm` (uint64), `votedFor` (string), `log` ([]LogEntry), `state` (Follower|Candidate|Leader)
- [ ] Log entries persist to a write-ahead log file and survive process restart (verified by a test that kills and restarts a node)
- [ ] Term is monotonically increasing — a unit test proves a node never decreases its term
- [ ] State transitions are logged with `slog` at INFO level: `<node> transitioned from Follower to Candidate (term 3)`
- [ ] `go test ./internal/raft/... -run TestLog` passes with coverage of append, read, and recovery

### Technical Notes
- Keep it simple: encode `LogEntry` as JSON-newline to a flat file. BoltDB or badger are fine but add complexity — skip for now
- Use Go's `sync.RWMutex` to protect state: reads are frequent (heartbeats), writes are rarer (elections)
- `LogEntry` should carry: `Index uint64`, `Term uint64`, `Command []byte` — keep Command opaque so the state machine layer is decoupled
- Write a `TestLogRecovery` test that appends 100 entries, simulates a crash, restarts, and asserts all entries are readable

---

## S1.2 — Leader Election (RequestVote RPC)
*8 points · Tags: Go, Raft, Networking*

**As a** control plane developer, **I want to** implement the Raft leader election protocol so that exactly one node becomes leader after startup or after the current leader fails, **so that** the cluster is self-healing and always has a leader to coordinate work, even across the simulated AWS↔GCP boundary.

### Acceptance Criteria
- [ ] On startup, the cluster elects a leader within 15 seconds (verified by watching logs)
- [ ] If the leader container is killed (`docker stop`), a new leader is elected within 15 seconds
- [ ] Only one leader exists at any term — enforced by a test that checks for split-brain after a simulated partition
- [ ] Election timeout is randomized between [300ms, 600ms] — increased from paper defaults to tolerate 50ms cross-cloud RTT
- [ ] Raft election events are emitted as Prometheus counters: `raft_elections_total`, `raft_term`

### Technical Notes
- Implement `RequestVote` as a gRPC unary call. Define it in `proto/raft.proto` and generate with `protoc`
- A node grants a vote only if: it hasn't voted this term AND the candidate's log is at least as up-to-date
- Use a context with a 2–3s timeout on each `RequestVote` RPC — the 50ms cross-cloud link means the default gRPC deadline is too short
- Do not start this story until S1.1 unit tests are green — election logic depends on correct term/log comparison

---

## S1.3 — Log Replication (AppendEntries RPC)
*8 points · Tags: Go, Raft, Networking*

**As a** control plane developer, **I want to** implement AppendEntries so the leader replicates log entries to all followers and advances the commit index once a quorum acknowledges, **so that** the cluster maintains a consistent, replicated log across all three control-plane nodes regardless of which cloud they're on.

### Acceptance Criteria
- [ ] Leader replicates a test command to all followers within 500ms under normal conditions
- [ ] An entry is only committed once a majority (2 of 3 nodes) acknowledges it
- [ ] If one follower is paused (`docker pause`), the remaining two still commit new entries
- [ ] Log consistency is verified after replication: all node logs match byte-for-byte on committed entries
- [ ] `AppendEntries` also serves as heartbeat (empty entries) — followers reset their election timer on receipt

### Technical Notes
- `AppendEntries` must carry: `term`, `leaderId`, `prevLogIndex`, `prevLogTerm`, `entries[]`, `leaderCommit`
- Followers reject `AppendEntries` if `prevLogTerm`/`prevLogIndex` don't match — implement the log-rollback (`nextIndex` decrement) loop
- Heartbeat interval should be ~150ms — well below the 300ms minimum election timeout, but with headroom for the 50ms WAN hop
- Track `raft_replication_latency_ms` as a Prometheus histogram — you'll use this data in Sprint 4's RQ3 cross-cloud analysis

---

## S1.4 — Node Agent Heartbeat & Cluster State View
*5 points · Tags: Go, Python, Raft*

**As a** cluster operator, **I want to** have Python worker nodes send periodic heartbeats to the Raft leader so the leader maintains a live view of worker health, **so that** the control plane always knows which workers are available to accept Map/Reduce tasks before scheduling any work.

### Acceptance Criteria
- [ ] Each Python worker sends a `RegisterWorker` gRPC call to the leader on startup, including its address and cloud tag (`aws`|`gcp`)
- [ ] Workers send a `Heartbeat` RPC every 5 seconds; the leader marks a worker offline if 3 consecutive heartbeats are missed
- [ ] `GET /cluster-state` on the leader's HTTP debug endpoint returns a JSON list of all workers with status and last-seen timestamp
- [ ] If a worker container is stopped, the leader's cluster state reflects it as offline within 20 seconds
- [ ] A worker that restarts automatically re-registers without manual intervention

### Technical Notes
- Add `WorkerService` to the proto alongside `RaftService` — keep them as separate gRPC services in the same server
- Store worker state in a `map[string]*WorkerInfo` protected by a mutex — this does not need Raft replication yet (that's Sprint 2)
- The Python worker should use `grpcio` and the generated stub; use a background thread for heartbeats so the main worker loop isn't blocked
- Leader-only check: if a follower receives a `RegisterWorker` call, return the current leader address so the worker can redirect and retry

---

## Sprint 1 — Deliverables Checklist

- [ ] Raft leader election: new leader elected within 15 seconds of any node failure
- [ ] Log replication: consistent replicated log verified across all 3 control-plane nodes
- [ ] Cross-cloud Raft: election and replication work across the simulated AWS↔GCP 50ms link
- [ ] Worker registration: Python workers join and leave the cluster dynamically; leader tracks health
- [ ] Prometheus metrics: `raft_elections_total`, `raft_term`, `raft_replication_latency_ms` emitted
- [ ] Baseline Raft latency numbers recorded for comparison in Sprint 4 RQ3 analysis

---

**Story dependency order:** S1.1 → S1.2 → S1.3 → S1.4. Do not start S1.2 until S1.1 unit tests are green. S1.4 (Python agent) can be worked in parallel once S1.2 is stable enough to accept connections.

**Recommended pairing:** Put your strongest Go developer on S1.1–S1.3 as a focused stream. A second developer can own the remaining Sprint 0 infra cleanup, then pick up S1.4 once the control plane is accepting gRPC connections.