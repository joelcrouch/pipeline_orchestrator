#!/usr/bin/env bash
# scripts/deploy_worker.sh — Setup and start a worker on a remote or local machine
set -eu

# Default values
ORCHESTRATOR_IP=${1:-"localhost"}
ORCHESTRATOR_PORT=${2:-"50051"}
MINIO_IP=${3:-"localhost"}
WORKER_ID=$(hostname)-worker-$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 4)

echo "🚀 Deploying Worker: $WORKER_ID"
echo "🔗 Orchestrator: $ORCHESTRATOR_IP:$ORCHESTRATOR_PORT"
echo "📦 Storage: http://$MINIO_IP:9000"

# 1. Detect Hardware
if command -v nvidia-smi &>/dev/null; then
    echo "Found NVIDIA GPU, using CUDA environment..."
    ENV_FILE="worker/environment.yml"
    ENV_NAME="pipeline-worker"
elif command -v rocm-smi &>/dev/null; then
    echo "Found AMD GPU, using ROCm environment (WIP)..."
    ENV_FILE="worker/environment.yml" # We'll adapt this for ROCm in Phase 2.1
    ENV_NAME="pipeline-worker"
else
    echo "No compatible GPU found, using CPU-only environment..."
    ENV_FILE="worker/environment-cpu.yml"
    ENV_NAME="pipeline-worker-cpu"
fi

# 2. Setup Conda/Mamba Environment
if ! command -v micromamba &>/dev/null; then
    echo "micromamba not found. Please install it first."
    exit 1
fi

echo "Checking environment $ENV_NAME..."
if ! micromamba env list | grep -q "$ENV_NAME"; then
    echo "Creating environment from $ENV_FILE..."
    micromamba env create -f "$ENV_FILE" -y
fi

# 3. Generate Protos (Python only)
echo "Generating Python protobuf stubs..."
micromamba run -n "$ENV_NAME" python -m grpc_tools.protoc \
    --proto_path=proto \
    --python_out=worker/worker/gen \
    --grpc_python_out=worker/worker/gen \
    proto/worker.proto proto/task.proto

# Fix relative imports in generated files
sed -i 's/^import worker_pb2 as worker__pb2$/from . import worker_pb2 as worker__pb2/' worker/worker/gen/worker_pb2_grpc.py
sed -i 's/^import task_pb2 as task__pb2$/from . import task_pb2 as task__pb2/' worker/worker/gen/task_pb2_grpc.py
touch worker/worker/gen/__init__.py

# 4. Start Worker
echo "Starting worker..."
export WORKER_ID="$WORKER_ID"
export ORCHESTRATOR_ADDR="$ORCHESTRATOR_IP:$ORCHESTRATOR_PORT"
export MINIO_ENDPOINT="http://$MINIO_IP:9000"

micromamba run -n "$ENV_NAME" python -m worker.main
