#!/bin/sh
set -e

mc alias set local http://minio:9000 "${MINIO_ROOT_USER:-minioadmin}" "${MINIO_ROOT_PASSWORD:-minioadmin}"
mc mb --ignore-existing local/pipeline-data
echo "Bucket pipeline-data ready"
