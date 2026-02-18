#!/usr/bin/env bash
# init-structure.sh
# Run from the repo root: bash scripts/init-structure.sh

set -e

echo "🚀 Scaffolding pipeline-orchestrator..."

# ── Directories ──────────────────────────────────────────────────────────────

mkdir -p control-plane/cmd/orchestrator
mkdir -p control-plane/internal/raft
mkdir -p control-plane/internal/agent
mkdir -p control-plane/internal/scheduler
mkdir -p control-plane/internal/storage
mkdir -p control-plane/internal/metrics

mkdir -p worker/worker/tasks
mkdir -p worker/worker/storage
mkdir -p worker/tests

mkdir -p proto/gen/go
mkdir -p proto/gen/python

mkdir -p docker/gateway
mkdir -p docker/init

mkdir -p scripts

mkdir -p data/raw
mkdir -p data/matrices
mkdir -p data/processed

# ── .gitkeep files in data/ ───────────────────────────────────────────────────

touch data/.gitkeep
touch data/raw/.gitkeep
touch data/matrices/.gitkeep
touch data/processed/.gitkeep
touch proto/gen/go/.gitkeep
touch proto/gen/python/.gitkeep

# ── Go module ────────────────────────────────────────────────────────────────

echo "🔧 Initialising Go module..."
cd control-plane
go mod init github.com/joelcrouch/pipeline-orchestrator/control-plane
touch cmd/orchestrator/main.go
touch internal/raft/node.go
touch internal/raft/log.go
touch internal/raft/election.go
touch internal/raft/replication.go
touch internal/raft/raft_test.go
touch internal/agent/registry.go
touch internal/agent/registry_test.go
touch internal/scheduler/scheduler.go
touch internal/storage/storage.go
touch internal/metrics/metrics.go
touch Dockerfile
cd ..

# ── Python worker placeholders ────────────────────────────────────────────────

touch worker/worker/__init__.py
touch worker/worker/main.py
touch worker/worker/heartbeat.py
touch worker/worker/tasks/__init__.py
touch worker/worker/tasks/map_task.py
touch worker/worker/tasks/reduce_task.py
touch worker/worker/storage/__init__.py
touch worker/worker/storage/client.py
touch worker/tests/test_health.py
touch worker/tests/test_heartbeat.py
touch worker/Dockerfile
touch worker/requirements-pip.txt

# ── Proto placeholders ────────────────────────────────────────────────────────

touch proto/raft.proto
touch proto/worker.proto
touch proto/task.proto

# ── Docker placeholders ───────────────────────────────────────────────────────

touch docker/docker-compose.yml
touch docker/docker-compose.override.yml
touch docker/gateway/Dockerfile
touch docker/init/init-buckets.sh

# ── Scripts ───────────────────────────────────────────────────────────────────

touch scripts/proto-gen.sh
touch scripts/test-storage.sh
touch scripts/sim-latency.sh
chmod +x scripts/proto-gen.sh scripts/test-storage.sh scripts/sim-latency.sh

# ── Root files ────────────────────────────────────────────────────────────────

touch Makefile
touch README.md
touch .env.example

# ── Initial commit ────────────────────────────────────────────────────────────

echo "📦 Creating initial commit..."
git add .
git commit -m "chore: initial project scaffold"

echo ""
echo "✅ Done! Structure created and committed."
echo ""
echo "Next steps:"
echo "  1. Edit control-plane/go.mod — replace 'your-org' with your actual GitHub username/org"
echo "  2. Copy environment.yml into worker/"
echo "  3. Copy .env.example contents into .env and fill in any values"
echo "  4. Start S0.1 — docker network setup"