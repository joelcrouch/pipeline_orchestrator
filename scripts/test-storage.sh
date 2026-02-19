#!/usr/bin/env bash
# S0.5 smoke test: PUT 1MB → MinIO → GET back → verify checksum
# Runs from each cloud's worker so reachability across all three networks is confirmed.
set -euo pipefail

BUCKET=pipeline-data

PYTHON_TEST=$(cat <<'PYEOF'
import boto3, hashlib, os, sys

cloud = os.environ.get("WORKER_CLOUD_TAG", "unknown")
data  = os.urandom(1024 * 1024)          # 1 MB
key   = f"smoke-test-{cloud}.bin"
sha_before = hashlib.sha256(data).hexdigest()

s3 = boto3.client(
    "s3",
    endpoint_url=os.environ.get("MINIO_ENDPOINT", "http://minio:9000"),
    aws_access_key_id=os.environ.get("MINIO_ROOT_USER", "minioadmin"),
    aws_secret_access_key=os.environ.get("MINIO_ROOT_PASSWORD", "minioadmin"),
    region_name="us-east-1",
)

s3.put_object(Bucket="pipeline-data", Key=key, Body=data)
body = s3.get_object(Bucket="pipeline-data", Key=key)["Body"].read()
sha_after = hashlib.sha256(body).hexdigest()

if sha_before != sha_after:
    print(f"  ✗ CHECKSUM MISMATCH on {cloud}", file=sys.stderr)
    sys.exit(1)

s3.delete_object(Bucket="pipeline-data", Key=key)
print(f"  ✓ {cloud}: PUT/GET 1MB OK  sha256={sha_before[:16]}...")
PYEOF
)

echo "── MinIO smoke test ─────────────────────────────────"
echo ""

for worker in worker-aws-1 worker-gcp-1 worker-azure-1; do
    printf "▶ Testing from %s\n" "$worker"
    docker exec "$worker" \
        micromamba run -n pipeline-worker python3 -c "$PYTHON_TEST"
done

echo ""
echo "All nodes can reach MinIO ✓"
