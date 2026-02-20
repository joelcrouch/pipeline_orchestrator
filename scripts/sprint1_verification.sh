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
    header "S1.2 — Leader Election & Cluster Bootstrap (placeholder)"
    info "S1.2 checks not yet implemented"
  }

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

