#!/usr/bin/env bash
# Sprint 0 deliverables verification
# Usage: bash scripts/verify-sprint0.sh
# Requires: cluster running (make sim-up) for S0.1 / S0.4 / S0.5 checks

set -uo pipefail

COMPOSE_FILE="docker/docker-compose.yml"
PASS=0
FAIL=0

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

pass() { echo -e "  ${GREEN}✓ PASS${NC}  $1"; PASS=$((PASS + 1)); }
fail() { echo -e "  ${RED}✗ FAIL${NC}  $1"; FAIL=$((FAIL + 1)); }
skip() { echo -e "  ${YELLOW}⚠ SKIP${NC}  $1"; }
header() { echo ""; echo -e "${BOLD}── $1${NC}"; }

container_healthy() {
  [ "$(docker inspect "$1" --format '{{.State.Health.Status}}' 2>/dev/null)" = "healthy" ]
}

cluster_up() {
  container_healthy gateway
}

# ── S0.1 — Multi-Cloud Networks ───────────────────────────────────────────────
header "S0.1 — Simulated Multi-Cloud Networks"

for net in docker_net-aws docker_net-gcp docker_net-azure; do
  if docker network ls --format '{{.Name}}' | grep -q "^${net}$"; then
    pass "Network ${net} exists"
  else
    fail "Network ${net} not found"
  fi
done

if cluster_up; then
  pass "gateway container is healthy"

  ping_avg() {
    docker exec gateway ping -c4 -q "$1" 2>/dev/null \
      | grep rtt | awk -F'/' '{print $5}'
  }

  avg=$(ping_avg 10.10.0.1)
  if awk "BEGIN { exit !($avg < 1.0) }"; then
    pass "Intra-cloud latency ${avg}ms < 1ms"
  else
    fail "Intra-cloud latency ${avg}ms — expected <1ms"
  fi

  avg=$(ping_avg 10.20.0.1)
  if awk "BEGIN { exit !($avg >= 40 && $avg <= 80) }"; then
    pass "AWS→GCP latency ${avg}ms (~50ms)"
  else
    fail "AWS→GCP latency ${avg}ms — expected ~50ms"
  fi

  avg=$(ping_avg 10.30.0.1)
  if awk "BEGIN { exit !($avg >= 55 && $avg <= 120) }"; then
    pass "AWS→Azure latency ${avg}ms (~75ms)"
  else
    fail "AWS→Azure latency ${avg}ms — expected ~75ms"
  fi
else
  skip "Latency tests — cluster not running (make sim-up first)"
fi

# ── S0.2 — Go Control Plane ───────────────────────────────────────────────────
header "S0.2 — Go Control Plane Scaffold"

if (cd control-plane && go build ./... 2>/dev/null); then
  pass "go build ./... clean"
else
  fail "go build ./... failed"
fi

if (cd control-plane && go test ./... 2>/dev/null); then
  pass "go test ./... all passing"
else
  fail "go test ./... failed"
fi

if command -v golangci-lint &>/dev/null; then
  if (cd control-plane && golangci-lint run 2>/dev/null); then
    pass "golangci-lint 0 issues"
  else
    fail "golangci-lint reported issues"
  fi
else
  skip "golangci-lint not on host PATH"
fi

if docker image inspect docker-cp-aws-1 &>/dev/null; then
  size_mb=$(docker image inspect docker-cp-aws-1 --format='{{.Size}}' \
    | awk '{printf "%.1f", $1/1024/1024}')
  if awk "BEGIN { exit !($size_mb < 50) }"; then
    pass "Control plane image ${size_mb}MB < 50MB"
  else
    fail "Control plane image ${size_mb}MB — expected <50MB"
  fi
else
  skip "docker-cp-aws-1 image not built"
fi

# ── S0.3 — Python Worker ──────────────────────────────────────────────────────
header "S0.3 — Python Worker Scaffold"

if (cd worker && micromamba run -n pipeline-worker python -m pytest tests/ -q 2>/dev/null); then
  pass "python -m pytest all passing"
else
  fail "python -m pytest failed"
fi

if container_healthy worker-aws-1; then
  pass "worker /health endpoint returns 200 (container healthy)"
else
  skip "worker-aws-1 not running — start cluster to verify /health"
fi

# ── S0.4 — 9-Container Cluster ────────────────────────────────────────────────
header "S0.4 — 9-Container Cluster with Health Checks"

if cluster_up; then
  for c in gateway minio cp-aws-1 cp-gcp-1 cp-azure-1 \
            worker-aws-1 worker-aws-2 worker-gcp-1 worker-azure-1; do
    if container_healthy "$c"; then
      pass "${c} is healthy"
    else
      status=$(docker inspect "$c" --format '{{.State.Health.Status}}' 2>/dev/null || echo "missing")
      fail "${c} status: ${status}"
    fi
  done

  for cp in cp-aws-1 cp-gcp-1 cp-azure-1; do
    nets=$(docker inspect "$cp" \
      --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' 2>/dev/null)
    if echo "$nets" | grep -q "net-aws" \
    && echo "$nets" | grep -q "net-gcp" \
    && echo "$nets" | grep -q "net-azure"; then
      pass "${cp} connected to all 3 networks"
    else
      fail "${cp} not on all 3 networks (got: ${nets})"
    fi
  done

  for w in worker-aws-1 worker-aws-2; do
    count=$(docker inspect "$w" --format '{{len .NetworkSettings.Networks}}' 2>/dev/null || echo 0)
    if [ "$count" -eq 1 ]; then
      pass "${w} on single network only"
    else
      fail "${w} on ${count} networks — expected 1"
    fi
  done
else
  skip "S0.4 container checks — cluster not running (make sim-up first)"
fi

# ── S0.5 — MinIO Storage ──────────────────────────────────────────────────────
header "S0.5 — Shared MinIO Storage"

if cluster_up; then
  if docker exec worker-aws-1 micromamba run -n pipeline-worker python3 -c "
import boto3
s3 = boto3.client('s3', endpoint_url='http://minio:9000',
    aws_access_key_id='minioadmin', aws_secret_access_key='minioadmin',
    region_name='us-east-1')
s3.head_bucket(Bucket='pipeline-data')
" 2>/dev/null; then
    pass "Bucket pipeline-data exists"
  else
    fail "Bucket pipeline-data not found"
  fi

  if bash scripts/test-storage.sh &>/dev/null; then
    pass "PUT/GET 1MB from all 3 clouds (aws, gcp, azure)"
  else
    fail "MinIO smoke test failed — run bash scripts/test-storage.sh for details"
  fi
else
  skip "S0.5 storage checks — cluster not running (make sim-up first)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════"
total=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
  echo -e "  ${GREEN}${BOLD}Sprint 0 COMPLETE${NC} — ${PASS}/${total} checks passed"
else
  echo -e "  ${RED}${BOLD}Sprint 0 INCOMPLETE${NC} — ${PASS} passed, ${FAIL} failed (${total} total)"
fi
echo "════════════════════════════════════════════════"
echo ""

[ "$FAIL" -eq 0 ]
