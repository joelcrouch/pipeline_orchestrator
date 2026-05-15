import pytest
import numpy as np
from unittest.mock import MagicMock, patch
from worker.storage.client import StorageClient

def test_parse_uri():
    client = StorageClient()
    bucket, key = client._parse_uri("s3://pipeline-data/jobs/123/A_0_0.npy")
    assert bucket == "pipeline-data"
    assert key == "jobs/123/A_0_0.npy"

@patch("boto3.client")
def test_save_npy(mock_boto):
    mock_s3 = MagicMock()
    mock_boto.return_value = mock_s3
    
    client = StorageClient()
    data = np.random.rand(10, 10).astype(np.float32)
    client.save_npy("s3://test/data.npy", data)
    
    mock_s3.put_object.assert_called_once()
    args, kwargs = mock_s3.put_object.call_args
    assert kwargs["Bucket"] == "test"
    assert kwargs["Key"] == "data.npy"

@patch("boto3.client")
def test_load_npy(mock_boto):
    mock_s3 = MagicMock()
    mock_boto.return_value = mock_s3
    
    # Mock the S3 response body
    data = np.array([1, 2, 3], dtype=np.float32)
    import io
    buf = io.BytesIO()
    np.save(buf, data)
    buf.seek(0)
    
    mock_s3.get_object.return_value = {"Body": MagicMock(read=lambda: buf.read())}
    
    client = StorageClient()
    loaded = client.load_npy("s3://test/data.npy")
    
    assert np.array_equal(loaded, data)
    mock_s3.get_object.assert_called_once_with(Bucket="test", Key="data.npy")
