import pytest
import numpy as np
from unittest.mock import MagicMock
from worker.tasks.map_task import execute_map_task
from worker.tasks.reduce_task import execute_reduce_task

def test_execute_map_task():
    # Setup mocks
    mock_storage = MagicMock()
    A = np.ones((64, 64), dtype=np.float32) * 2
    B = np.ones((64, 64), dtype=np.float32) * 3
    mock_storage.load_npy.side_effect = [A, B]
    
    mock_task = MagicMock()
    mock_task.task_id = "test-map"
    mock_task.i, mock_task.j, mock_task.k = 0, 0, 0
    mock_task.a_uri = "s3://a"
    mock_task.b_uri = "s3://b"
    mock_task.output_uri = "s3://out"
    
    # Execute
    result = execute_map_task(mock_task, mock_storage)
    
    # Verify
    assert result["shape"] == [64, 64]
    # 2 * 3 * 64 (dot product) = 384
    expected_val = 2 * 3 * 64
    
    # Check what was saved
    mock_storage.save_npy.assert_called_once()
    saved_arr = mock_storage.save_npy.call_args[0][1]
    assert np.allclose(saved_arr, expected_val)

def test_execute_reduce_task():
    # Setup mocks
    mock_storage = MagicMock()
    P1 = np.ones((64, 64), dtype=np.float32)
    P2 = np.ones((64, 64), dtype=np.float32)
    mock_storage.load_npy.side_effect = [P1, P2]
    
    mock_task = MagicMock()
    mock_task.task_id = "test-reduce"
    mock_task.input_uris = ["s3://p1", "s3://p2"]
    mock_task.output_uri = "s3://out"
    
    # Execute
    result = execute_reduce_task(mock_task, mock_storage)
    
    # Verify
    assert result["num_partials"] == 2
    mock_storage.save_npy.assert_called_once()
    saved_arr = mock_storage.save_npy.call_args[0][1]
    assert np.allclose(saved_arr, 2.0)
