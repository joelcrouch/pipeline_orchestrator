#!/usr/bin/env bash
# proto-gen.sh — generates Go and Python stubs from proto/worker.proto
# Usage: bash scripts/proto-gen.sh
set -eu

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ── Toolchain checks / installs ───────────────────────────────────────────────
if ! command -v protoc &>/dev/null; then
  echo "ERROR: protoc not found."
  echo "  Install with: sudo apt-get install -y protobuf-compiler"
  exit 1
fi

GOBIN="$(go env GOPATH)/bin"
export PATH="$PATH:$GOBIN"

for tool in protoc-gen-go protoc-gen-go-grpc; do
  if ! command -v "$tool" &>/dev/null; then
    echo "Installing $tool..."
    case "$tool" in
      protoc-gen-go)      go install google.golang.org/protobuf/cmd/protoc-gen-go@latest ;;
      protoc-gen-go-grpc) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest ;;
    esac
  fi
done

# ── Output directories ────────────────────────────────────────────────────────
WORKER_GO_OUT="$REPO_ROOT/control-plane/internal/gen/worker"
TASK_GO_OUT="$REPO_ROOT/control-plane/internal/gen/task"
PY_OUT="$REPO_ROOT/worker/worker/gen"
WORKER_PROTO="$REPO_ROOT/proto/worker.proto"
TASK_PROTO="$REPO_ROOT/proto/task.proto"

mkdir -p "$WORKER_GO_OUT" "$TASK_GO_OUT" "$PY_OUT"

# ── Go stubs (Worker) ─────────────────────────────────────────────────────────
echo "Generating Go stubs (Worker) → $WORKER_GO_OUT"
protoc \
  --proto_path="$REPO_ROOT/proto" \
  --go_out="$WORKER_GO_OUT"      --go_opt=paths=source_relative \
  --go-grpc_out="$WORKER_GO_OUT" --go-grpc_opt=paths=source_relative \
  "$WORKER_PROTO"

# ── Go stubs (Task) ───────────────────────────────────────────────────────────
echo "Generating Go stubs (Task) → $TASK_GO_OUT"
protoc \
  --proto_path="$REPO_ROOT/proto" \
  --go_out="$TASK_GO_OUT"      --go_opt=paths=source_relative \
  --go-grpc_out="$TASK_GO_OUT" --go-grpc_opt=paths=source_relative \
  "$TASK_PROTO"

# ── Python stubs ──────────────────────────────────────────────────────────────
echo "Generating Python stubs → $PY_OUT"
~/.local/share/mamba/envs/pipeline-worker/bin/python \
  -m grpc_tools.protoc \
  --proto_path="$REPO_ROOT/proto" \
  --python_out="$PY_OUT" \
  --grpc_python_out="$PY_OUT" \
  "$WORKER_PROTO" "$TASK_PROTO"

# grpc_tools generates bare imports which break when the files live inside a package.
# Fix them to relative imports.
sed -i 's/^import worker_pb2 as worker__pb2$/from . import worker_pb2 as worker__pb2/' "$PY_OUT/worker_pb2_grpc.py"
sed -i 's/^import task_pb2 as task__pb2$/from . import task_pb2 as task__pb2/' "$PY_OUT/task_pb2_grpc.py"

# Ensure the gen/ directory is a proper Python package
touch "$PY_OUT/__init__.py"

echo "Proto generation complete."
