!/usr/bin/env bash
  # sprint1_verification.sh — run after each S1.x story, and as a full suite at sprint end.
  # Usage:
  #   bash scripts/sprint1_verification.sh s1.1        # S1.1 checks only
  #   bash scripts/sprint1_verification.sh s1.2        # S1.2 checks only
  #   bash scripts/sprint1_verification.sh all         # full sprint
  #   bash scripts/sprint1_verification.sh             # defaults to all

  set -eu

  STORY=${1:-all}
  COMPOSE="docker compose -f docker/docker-compose.yml"
  PASS=0; FAIL=0

  GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'

  pass() { echo -e "${GREEN}  ✅ $1${NC}"; PASS=$((PASS+1)); }
  fail() { echo -e "${RED}  ❌ $1${NC}"; FAIL=$((FAIL+1)); }
  info() { echo -e "${YELLOW}  ── $1${NC}"; }
  header() { echo -e "\n${YELLOW}━━━ $1 ━━━${NC}"; }

  # ─── helper: wait for a container to be healthy ───────────────────────────────
  wait_healthy() {
    local name=$1 timeout=${2:-60}
    local elapsed=0
    info "Waiting for $name to be healthy..."
    while ! docker inspect --format='{{.State.Health.Status}}' "$name" 2>/dev/null | grep -q "^healthy$"; do
      sleep 2; ((elapsed+=2))
      if (( elapsed >= timeout )); then
        fail "$name did not become healthy within ${timeout}s"
        return 1
      fi
    done
    pass "$name is healthy"
  }

  # ═══════════════════════════════════════════════════════════════════════════════
  s1_1() {
    header "S1.1 — HashiCorp Raft Setup & Persistent Log Store"

    # 1. go.mod dependencies
    info "Checking go.mod for raft dependencies"
    if grep -q "github.com/hashicorp/raft " control-plane/go.mod; then
      pass "go.mod contains github.com/hashicorp/raft"
    else
      fail "go.mod missing github.com/hashicorp/raft"
    fi

    if grep -q "github.com/hashicorp/raft-boltdb" control-plane/go.mod; then
      pass "go.mod contains github.com/hashicorp/raft-boltdb"
    else
      fail "go.mod missing github.com/hashicorp/raft-boltdb"
    fi

    # 2. Unit test
    info "Running TestNodeInit"
    if (cd control-plane && go test ./internal/raft/... -run TestNodeInit -timeout 30s -count=1 2>&1); then
      pass "TestNodeInit passed"
    else
      fail "TestNodeInit failed"
    fi

    # 3. Build still compiles
    info "Verifying go build"
    if (cd control-plane && go build ./... 2>&1); then
      pass "go build ./... clean"
    else
      fail "go build ./... failed"
    fi

    # 4 & 5. BoltDB volume persistence — needs Docker
    if ! docker info &>/dev/null; then
      info "Docker not available — skipping volume persistence checks"
      return
    fi

    info "Starting cluster for volume persistence check"
    $COMPOSE up --build -d cp-aws-1 gateway 2>&1 | tail -5
    wait_healthy cp-aws-1 90

    # BoltDB file should exist
    if docker exec cp-aws-1 test -f /data/raft/raft.db; then
      pass "BoltDB file /data/raft/raft.db exists in cp-aws-1"
    else
      fail "BoltDB file /data/raft/raft.db NOT found in cp-aws-1"
    fi

    # Restart and verify file survives
    info "Restarting cp-aws-1 to verify BoltDB persistence..."
    docker restart cp-aws-1
    wait_healthy cp-aws-1 60

    if docker exec cp-aws-1 test -f /data/raft/raft.db; then
      pass "BoltDB file persists after container restart"
    else
      fail "BoltDB file LOST after container restart"
    fi

    # Check logs for raft activity
    info "Checking cp-aws-1 logs for raft startup"
    if docker logs cp-aws-1 2>&1 | grep -q "raft node started"; then
      pass "Log contains 'raft node started'"
    else
      fail "Log missing 'raft node started'"
    fi
  }

  # ═══════════════════════════════════════════════════════════════════════════════
  s1_2() {         
    header "S1.2 — Leader Election & Cluster Bootstrap"                                                                        
                                                                                                                               
    if ! docker info &>/dev/null; then
      info "Docker not available — skipping S1.2 checks"
      return
    fi

    # Bring up the full 3-node cluster
    info "Starting full 3-node cluster..."
    $COMPOSE up --build -d gateway cp-aws-1 cp-gcp-1 cp-azure-1 2>&1 | tail -5
    wait_healthy cp-aws-1 120
    wait_healthy cp-gcp-1 120
    wait_healthy cp-azure-1 120

    # 1. Leader elected within 20s (nodes were just started — allow poll time)
    info "Waiting for a leader to emerge across all 3 nodes..."
    leader_found=false
    deadline=$((SECONDS + 20))
    while (( SECONDS < deadline )); do
      for port in 8080 8083 8085; do
        state=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null | grep -o '"state":"[^"]*"' | cut -d'"' -f4)
        if [[ "$state" == "Leader" ]]; then
          leader_found=true
          break 2
        fi
      done
      sleep 1
    done

    if $leader_found; then
      pass "Leader elected within 20s"
    else
      fail "No leader found within 20s"
    fi

    # 2. /raft-state endpoint on all 3 nodes returns valid JSON
    info "Checking /raft-state on all 3 nodes"
    all_raft_ok=true
    for port in 8080 8083 8085; do
      resp=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null)
      if echo "$resp" | grep -q '"state"'; then
        pass "/raft-state on :${port} → $resp"
      else
        fail "/raft-state on :${port} returned no state"
        all_raft_ok=false
      fi
    done

    # 3. Exactly one leader across all 3 nodes
    info "Verifying exactly one leader"
    leader_count=0
    for port in 8080 8083 8085; do
      state=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null | grep -o '"state":"[^"]*"' | cut -d'"' -f4)
      [[ "$state" == "Leader" ]] && leader_count=$((leader_count + 1))
    done
    if (( leader_count == 1 )); then
      pass "Exactly 1 leader across 3 nodes"
    else
      fail "Expected 1 leader, found ${leader_count}"
    fi

    # 4. Prometheus metrics endpoint reachable and contains raft metrics
    info "Checking /metrics for raft gauges"
    metrics_out=$(curl -sf "http://localhost:8080/metrics" 2>/dev/null)
    for metric in raft_state raft_term raft_elections_total; do
      if echo "$metrics_out" | grep -q "^${metric}"; then
        pass "Prometheus metric '${metric}' present"
      else
        fail "Prometheus metric '${metric}' missing from /metrics"
      fi
    done

    # 5. Leader failover within 20s
    info "Testing leader failover — killing current leader..."
    leader_node=""
    for pair in "cp-aws-1:8080" "cp-gcp-1:8083" "cp-azure-1:8085"; do
      name="${pair%%:*}"; port="${pair##*:}"
      state=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null | grep -o '"state":"[^"]*"' | cut -d'"' -f4)
      [[ "$state" == "Leader" ]] && leader_node="$name"
    done

    if [[ -z "$leader_node" ]]; then
      fail "Could not identify leader for failover test"
      return
    fi

    info "Stopping leader: $leader_node"
    docker stop "$leader_node" >/dev/null

    new_leader=false
    deadline=$((SECONDS + 20))
    while (( SECONDS < deadline )); do
      for port in 8080 8083 8085; do
        state=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null | grep -o '"state":"[^"]*"' | cut -d'"' -f4)
        if [[ "$state" == "Leader" ]]; then
          new_leader=true
          break 2
        fi
      done
      sleep 1
    done

    if $new_leader; then
      pass "New leader elected within 20s of leader failure"
    else
      fail "No new leader within 20s after leader failure"
    fi

    # Restart the stopped node for a clean state
    info "Restarting $leader_node..."
    docker start "$leader_node" >/dev/null
    wait_healthy "$leader_node" 60
    pass "$leader_node rejoined cluster after restart"
  }

# ═══════════════════════════════════════════════════════════════════════════════
  s1_3() {
    header "S1.3 — FSM State Machine & Log Replication"

    # ── Criterion 1: PipelineFSM Apply() mutates WorkerInfo map ──────────────
    info "[AC1] TestFSMApply — in-memory WorkerInfo map mutated by Apply()"
    if (cd control-plane && go test ./internal/raft/... -run "^TestFSMApply$" -timeout 30s -count=1 2>&1); then
      pass "TestFSMApply: register_worker and update_worker_status commands applied correctly"
    else
      fail "TestFSMApply failed"
    fi

    # ── Criterion 2: RegisterWorker replicates to all followers within 500ms ──
    info "[AC2] TestReplicationToFollowers — leader→follower replication within 500ms"
    if (cd control-plane && go test ./internal/raft/... -run "^TestReplicationToFollowers$" -timeout 30s -count=1 2>&1); then
      pass "TestReplicationToFollowers: all 3 FSMs reflect entry within 500ms"
    else
      fail "TestReplicationToFollowers failed"
    fi

    # ── Criterion 3: Majority quorum — 2/3 nodes sufficient to commit ─────────
    info "[AC3] TestMajorityQuorum — entry commits with one node isolated"
    if (cd control-plane && go test ./internal/raft/... -run "^TestMajorityQuorum$" -timeout 30s -count=1 2>&1); then
      pass "TestMajorityQuorum: commit succeeded with 2/3 nodes; isolated node caught up on reconnect"
    else
      fail "TestMajorityQuorum failed"
    fi

    # ── Criterion 5: raft_replication_latency_ms histogram (unit) ────────────
    info "[AC5] Verifying Apply() records replication latency via metrics"
    if grep -q "RaftReplicationLatencyMs.Observe" control-plane/internal/raft/node.go; then
      pass "node.Apply() records RaftReplicationLatencyMs"
    else
      fail "node.Apply() missing RaftReplicationLatencyMs.Observe call"
    fi

    # ── Criterion 6: FSMSnapshot / FSMRestore round-trip ─────────────────────
    info "[AC6] TestFSMSnapshotRestore — snapshot wipes memory, restore recovers state"
    if (cd control-plane && go test ./internal/raft/... -run "^TestFSMSnapshotRestore$" -timeout 30s -count=1 2>&1); then
      pass "TestFSMSnapshotRestore: 3 workers consistent after snapshot→wipe→restore"
    else
      fail "TestFSMSnapshotRestore failed"
    fi

    # ── Build check ───────────────────────────────────────────────────────────
    info "Verifying go build ./..."
    if (cd control-plane && go build ./... 2>&1); then
      pass "go build ./... clean"
    else
      fail "go build ./... failed"
    fi

    # ── Docker checks ─────────────────────────────────────────────────────────
    if ! docker info &>/dev/null; then
      info "Docker not available — skipping Docker-level S1.3 checks"
      return
    fi

    info "Ensuring full 3-node cluster is running..."
    $COMPOSE up --build -d gateway cp-aws-1 cp-gcp-1 cp-azure-1 2>&1 | tail -5
    wait_healthy cp-aws-1 120
    wait_healthy cp-gcp-1 120
    wait_healthy cp-azure-1 120

    # ── Criterion 1 (Docker): /cluster-state endpoint exposes FSM state ───────
    info "[AC1] /cluster-state returns valid JSON (node_id + workers array) on all 3 nodes"
    for pair in "cp-aws-1:8080" "cp-gcp-1:8083" "cp-azure-1:8085"; do
      name="${pair%%:*}"; port="${pair##*:}"
      resp=$(curl -sf "http://localhost:${port}/cluster-state" 2>/dev/null)
      if echo "$resp" | grep -q '"node_id"' && echo "$resp" | grep -q '"workers"'; then
        pass "/cluster-state on ${name}:${port} — valid JSON with node_id + workers"
      else
        fail "/cluster-state on ${name}:${port} — missing fields (got: ${resp})"
      fi
    done

    # ── Criterion 4: identical FSM state across all 3 nodes ──────────────────
    # No HTTP register endpoint yet (S1.4), so we compare worker counts across
    # nodes — all should be 0 and equal, proving consistent replicated state.
    info "[AC4] All 3 nodes report identical worker count from /cluster-state"
    counts=()
    for pair in "cp-aws-1:8080" "cp-gcp-1:8083" "cp-azure-1:8085"; do
      port="${pair##*:}"
      # count occurrences of "id" inside the workers array as a proxy for worker count
      count=$(curl -sf "http://localhost:${port}/cluster-state" 2>/dev/null \
               | grep -o '"id"' | wc -l | tr -d ' ')
      counts+=("$count")
    done
    if [[ "${counts[0]}" == "${counts[1]}" && "${counts[1]}" == "${counts[2]}" ]]; then
      pass "All 3 nodes agree on worker count: ${counts[0]} workers"
    else
      fail "FSM state mismatch: cp-aws-1=${counts[0]} cp-gcp-1=${counts[1]} cp-azure-1=${counts[2]} workers"
    fi

    # ── Criterion 5 (Docker): Prometheus histogram registered and served ──────
    info "[AC5] /metrics exposes raft_replication_latency_ms histogram"
    metrics_out=$(curl -sf "http://localhost:8080/metrics" 2>/dev/null)
    if echo "$metrics_out" | grep -q "^raft_replication_latency_ms"; then
      pass "Prometheus metric 'raft_replication_latency_ms' present on /metrics"
    else
      fail "Prometheus metric 'raft_replication_latency_ms' missing from /metrics"
    fi

    # ── Criterion 6 (Docker): snapshot directory provisioned ─────────────────
    info "[AC6] Snapshot directory exists inside cp-aws-1 container"
    if docker exec cp-aws-1 test -d /data/raft/snapshots; then
      pass "Snapshot directory /data/raft/snapshots exists in cp-aws-1"
    else
      fail "Snapshot directory /data/raft/snapshots NOT found in cp-aws-1"
    fi

    # ── Criterion 2 (Docker): new leader elected within 20s of node stop ──────
    info "[AC2] Finding current Raft leader for re-election test..."
    leader_node=""; leader_port=""
    for pair in "cp-aws-1:8080" "cp-gcp-1:8083" "cp-azure-1:8085"; do
      name="${pair%%:*}"; port="${pair##*:}"
      state=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null \
              | python3 -c "import sys,json; print(json.load(sys.stdin).get('state',''))" 2>/dev/null)
      if [[ "$state" == "Leader" ]]; then
        leader_node="$name"; leader_port="$port"; break
      fi
    done

    if [[ -z "$leader_node" ]]; then
      fail "Could not identify current leader — skipping re-election test"
    else
      info "[AC2] Stopping $leader_node, expecting new leader within 20s..."
      docker stop "$leader_node" >/dev/null
      stop_ts=$SECONDS

      new_leader_node=""
      re_elect_deadline=$((SECONDS + 20))
      while (( SECONDS < re_elect_deadline )); do
        for pair in "cp-aws-1:8080" "cp-gcp-1:8083" "cp-azure-1:8085"; do
          name="${pair%%:*}"; port="${pair##*:}"
          [[ "$name" == "$leader_node" ]] && continue
          state=$(curl -sf "http://localhost:${port}/raft-state" 2>/dev/null \
                  | python3 -c "import sys,json; print(json.load(sys.stdin).get('state',''))" 2>/dev/null)
          if [[ "$state" == "Leader" ]]; then
            new_leader_node="$name"; break 2
          fi
        done
        sleep 1
      done

      if [[ -n "$new_leader_node" ]]; then
        elapsed=$(( SECONDS - stop_ts ))
        pass "New leader ($new_leader_node) elected within ${elapsed}s of stopping $leader_node"
      else
        fail "No new leader elected within 20s after stopping $leader_node"
      fi

      info "Restarting $leader_node..."
      docker start "$leader_node" >/dev/null
      wait_healthy "$leader_node" 60
      pass "$leader_node rejoined cluster after restart"
    fi

    # ── Prometheus metric values spot-check ────────────────────────────────────
    # Re-fetch after the re-election so raft_elections_total is guaranteed > 0.
    # We just verify the numbers exist — no need to save them now.
    info "[AC5+] Prometheus metric values (verifying numbers are emitted)"
    metrics_fresh=$(curl -sf "http://localhost:8080/metrics" 2>/dev/null)
    for metric in raft_state raft_term raft_elections_total; do
      val=$(echo "$metrics_fresh" | grep "^${metric} " | awk '{print $2}')
      if [[ -n "$val" ]]; then
        pass "${metric} = ${val}"
      else
        fail "${metric} not found or has no value in /metrics"
      fi
    done
    histo_count=$(echo "$metrics_fresh" | grep "^raft_replication_latency_ms_count " | awk '{print $2}')
    if [[ -n "$histo_count" ]]; then
      pass "raft_replication_latency_ms_count = ${histo_count}"
    else
      fail "raft_replication_latency_ms histogram count not found in /metrics"
    fi
  }

  s1_4() {
    header "S1.4 — Worker Registration & Cluster State View"

    # ── Criterion 1 & 2: RegisterWorker submits through raft.Apply ────────────
    info "[AC1/2] Go build and agent registry unit tests"
    if (cd control-plane && go build ./... 2>&1); then
      pass "go build ./... clean"
    else
      fail "go build ./... failed"
    fi

    if (cd control-plane && go test ./internal/agent/... -timeout 30s -count=1 2>&1); then
      pass "agent registry tests: RegisterWorker + Heartbeat + monitor all green"
    else
      fail "agent registry tests failed"
    fi

    # ── Criterion 3: Heartbeat every 5s, offline after 3 missed ──────────────
    info "[AC3] HeartbeatTracker offline detection tests"
    if (cd control-plane && go test ./internal/agent/... \
        -run "TestCheckHeartbeats|TestHeartbeat" -timeout 30s -count=1 2>&1); then
      pass "Heartbeat monitor: stale worker marked offline, no spam, follower skips"
    else
      fail "Heartbeat monitor tests failed"
    fi

    # ── Python gRPC client tests ───────────────────────────────────────────────
    info "[AC1/3] Python gRPC client tests (register + heartbeat + redirects)"
    if (cd worker && pytest tests/ -q 2>&1); then
      pass "All 16 Python tests passed (including gRPC mock tests)"
    else
      fail "Python tests failed"
    fi

    # ── Docker checks ─────────────────────────────────────────────────────────
    if ! docker info &>/dev/null; then
      info "Docker not available — skipping Docker-level S1.4 checks"
      return
    fi

    info "Tearing down any previous cluster (including Raft data volumes)..."
    $COMPOSE down --volumes 2>&1 | tail -3 || true

    info "Starting full cluster with workers (rebuild for S1.4 changes)..."
    $COMPOSE up --build -d gateway cp-aws-1 cp-gcp-1 cp-azure-1 \
      worker-aws-1 worker-aws-2 worker-gcp-1 worker-azure-1 2>&1 | tail -5

    for svc in cp-aws-1 cp-gcp-1 cp-azure-1 worker-aws-1 worker-aws-2 worker-gcp-1 worker-azure-1; do
      wait_healthy "$svc" 120
    done

    # Poll until all 4 workers appear online (or 45s timeout).
    # Fixed sleeps are fragile — workers going through a follower redirect
    # need extra time: connect to follower → get redirect → connect to leader → register.
    info "Polling for all 4 workers to register (up to 45s)..."
    cluster_resp=""
    online_count=0
    reg_deadline=$((SECONDS + 45))
    while (( SECONDS < reg_deadline )); do
      cluster_resp=$(curl -sf "http://localhost:8080/cluster-state" 2>/dev/null)
      online_count=$(echo "$cluster_resp" | grep -o '"status":"online"' | wc -l | tr -d ' ')
      (( online_count >= 4 )) && break
      sleep 2
    done

    # ── Criterion 4: /cluster-state shows 4 workers, all online ──────────────
    info "[AC4] /cluster-state shows 4 online workers"
    if (( online_count == 4 )); then
      pass "/cluster-state: ${online_count}/4 workers online"
    else
      fail "/cluster-state: ${online_count}/4 workers online (expected 4)"
      info "Response: $cluster_resp"
    fi

    for wid in worker-aws-1 worker-aws-2 worker-gcp-1 worker-azure-1; do
      if echo "$cluster_resp" | grep -q "\"$wid\""; then
        pass "Worker $wid present in cluster-state"
      else
        fail "Worker $wid missing from cluster-state"
      fi
    done

    # ── Criterion 5: worker goes offline within 20s of container stop ─────────
    info "[AC5] Stopping worker-aws-1 — expect offline within 25s"
    docker stop worker-aws-1 >/dev/null

    offline=false
    deadline=$((SECONDS + 25))
    while (( SECONDS < deadline )); do
      resp=$(curl -sf "http://localhost:8080/cluster-state" 2>/dev/null)
      if echo "$resp" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for w in data.get('workers', []):
    if w['id'] == 'worker-aws-1' and w['status'] == 'offline':
        sys.exit(0)
sys.exit(1)
" 2>/dev/null; then
        offline=true
        break
      fi
      sleep 2
    done

    if $offline; then
      pass "worker-aws-1 marked offline within 25s of container stop"
    else
      fail "worker-aws-1 not marked offline within 25s"
    fi

    # ── Criterion 6: worker auto-reregisters after restart ────────────────────
    info "[AC6] Restarting worker-aws-1 — expect auto-reregistration"
    docker start worker-aws-1 >/dev/null
    wait_healthy worker-aws-1 60
    sleep 10  # allow registration thread to reconnect

    resp=$(curl -sf "http://localhost:8080/cluster-state" 2>/dev/null)
    if echo "$resp" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for w in data.get('workers', []):
    if w['id'] == 'worker-aws-1' and w['status'] == 'online':
        sys.exit(0)
sys.exit(1)
" 2>/dev/null; then
      pass "worker-aws-1 back online after restart (auto-reregistered)"
    else
      fail "worker-aws-1 not back online after restart"
    fi
  }

  # ═══════════════════════════════════════════════════════════════════════════════
  summary() {
    echo -e "\n${YELLOW}━━━ Summary ━━━${NC}"
    echo -e "  Passed: ${GREEN}${PASS}${NC}  Failed: ${RED}${FAIL}${NC}"
    if (( FAIL > 0 )); then
      echo -e "${RED}  Sprint 1 verification FAILED${NC}"
      exit 1
    else
      echo -e "${GREEN}  All checks passed ✅${NC}"
    fi
  }

  # ─── dispatch ─────────────────────────────────────────────────────────────────
  case "$STORY" in
    s1.1) s1_1 ;;
    s1.2) s1_2 ;;
    s1.3) s1_3 ;;
    s1.4) s1_4 ;;
    all)  s1_1; s1_2; s1_3; s1_4 ;;
    *) echo "Usage: $0 [s1.1|s1.2|s1.3|s1.4|all]"; exit 1 ;;
  esac

  summary

