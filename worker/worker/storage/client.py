import io
import os
import numpy as np
import boto3
from urllib.parse import urlparse

class StorageClient:
    def __init__(self):
        self.endpoint = os.environ.get("MINIO_ENDPOINT", "http://minio:9000")
        self.access_key = os.environ.get("MINIO_ROOT_USER", "minioadmin")
        self.secret_key = os.environ.get("MINIO_ROOT_PASSWORD", "minioadmin")
        
        self.s3 = boto3.client(
            "s3",
            endpoint_url=self.endpoint,
            aws_access_key_id=self.access_key,
            aws_secret_access_key=self.secret_key,
            use_ssl=False,
        )

    def load_npy(self, uri: str) -> np.ndarray:
        bucket, key = self._parse_uri(uri)
        obj = self.s3.get_object(Bucket=bucket, Key=key)
        return np.load(io.BytesIO(obj["Body"].read()))

    def save_npy(self, uri: str, arr: np.ndarray):
        bucket, key = self._parse_uri(uri)
        buf = io.BytesIO()
        np.save(buf, arr)
        buf.seek(0)
        self.s3.put_object(Bucket=bucket, Key=key, Body=buf)

    def _parse_uri(self, uri: str):
        # s3://bucket/key
        parsed = urlparse(uri)
        bucket = parsed.netloc
        key = parsed.path.lstrip("/")
        return bucket, key
