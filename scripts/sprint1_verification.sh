!/usr/bin/env bash
  # sprint1_verification.sh — run after each S1.x story, and as a full suite at sprint end.
  # Usage:
  #   bash scripts/sprint1_verification.sh s1.1        # S1.1 checks only
  #   bash scripts/sprint1_verification.sh s1.2        # S1.2 checks only
  #   bash scripts/sprint1_verification.sh all         # full sprint
  #   bash scripts/sprint1_verification.sh             # defaults to all

  set -euo pipefail

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
    while ! docker inspect --format='{{.State.Health.Status}}' "$name" 2>/dev/null | grep -q healthy; do
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

# =================================================================================

  s1_3() {
    header "S1.3 — FSM State Machine & Log Replication (placeholder)"
    info "S1.3 checks not yet implemented"
  }

  s1_4() {
    header "S1.4 — Worker Registration & Heartbeat (placeholder)"
    info "S1.4 checks not yet implemented"
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

