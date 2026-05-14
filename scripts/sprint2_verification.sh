#!/usr/bin/env bash
# scripts/sprint2_verification.sh
set -eu

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

COMPOSE="docker compose -f docker/docker-compose.yml"

echo -e "${GREEN}━━━ Sprint 2: High-Throughput MapReduce Core ━━━${NC}"

wait_healthy() {
  local name=$1 timeout=${2:-60}
  local elapsed=0
  echo -e "${YELLOW}  ── Waiting for $name to be healthy...${NC}"
  while ! docker inspect --format='{{.State.Health.Status}}' "$name" 2>/dev/null | grep -q "^healthy$"; do
    sleep 2; ((elapsed+=2))
    if (( elapsed >= timeout )); then
      echo -e "${RED}  ❌ $name did not become healthy within ${timeout}s${NC}"
      return 1
    fi
  done
  echo -e "${GREEN}  ✅ $name is healthy${NC}"
}

s2_1() {
  echo -e "\n${GREEN}━━━ S2.1 — Matrix Chunking & Manifest ━━━${NC}"
  
  # 1. Verify chunker logic via Python unit test
  echo "  ── [AC1/AC2/AC3/AC5] Verifying chunker.py via unit tests..."
  if mamba run -n pipeline-worker python -m pytest worker/tests/test_chunker.py; then
    echo -e "  ${GREEN}✅ Chunker unit tests passed${NC}"
  else
    echo -e "  ${RED}❌ Chunker unit tests failed${NC}"
    exit 1
  fi

  # 2. Verify Go build for TaskService
  echo "  ── [AC4] Verifying Go build for control-plane..."
  if (cd control-plane && go build ./...); then
    echo -e "  ${GREEN}✅ Go build clean${NC}"
  else
    echo -e "  ${RED}❌ Go build failed${NC}"
    exit 1
  fi
}

s2_2_3() {
  echo -e "\n${GREEN}━━━ S2.2 & S2.3 — MapReduce Execution & Correctness ━━━${NC}"

  echo "  ── Ensuring cluster is up and healthy..."
  $COMPOSE up --build -d
  
  for svc in cp-aws-1 cp-gcp-1 cp-azure-1 minio gateway; do
    wait_healthy "$svc" 120
  done

  echo "  ── [AC4/AC5] Running end-to-end numerical verification..."
  if mamba run -n pipeline-worker python scripts/verify_job.py --m 256 --k 256 --n 256 --block-size 128; then
    echo -e "  ${GREEN}✅ Numerical correctness verified across all clouds${NC}"
  else
    echo -e "  ${RED}❌ Numerical correctness failed${NC}"
    exit 1
  fi
}

# Run all or specific part
COMMAND=${1:-all}

case $COMMAND in
  s2.1) s2_1 ;;
  s2.2) s2_2_3 ;;
  s2.3) s2_2_3 ;;
  all)  s2_1; s2_2_3 ;;
  *) echo "Usage: $0 {s2.1|s2.2|s2.3|all}"; exit 1 ;;
esac

echo -e "\n${GREEN}All checks passed ✅${NC}"
