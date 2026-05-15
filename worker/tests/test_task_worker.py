import pytest
from unittest.mock import MagicMock, patch
from worker.tasks.client import TaskWorker
from worker.gen import task_pb2

@pytest.fixture
def mock_storage():
    with patch("worker.tasks.client.StorageClient") as mock:
        yield mock.return_value

def test_poll_and_execute_map(mock_storage):
    worker = TaskWorker("worker-1", "localhost:50051")
    stub = MagicMock()
    
    # Mock a successful PollTask response with a Map task
    mock_task = task_pb2.MapTask(job_id="j1", task_id="m1")
    mock_task.metadata["model_type"] = "matmul"
    
    resp = task_pb2.PollTaskResponse(has_task=True, map_task=mock_task)
    stub.PollTask.return_value = resp
    
    # Mock the execution function
    with patch("worker.tasks.client.execute_map_task") as mock_exec:
        mock_exec.return_value = {"duration_ms": 100}
        worker._poll_and_execute(stub, "map")
        
        mock_exec.assert_called_once()
        stub.ReportTaskResult.assert_called_once()
        args = stub.ReportTaskResult.call_args[0][0]
        assert args.job_id == "j1"
        assert args.task_id == "m1"
        assert args.success is True

def test_poll_and_execute_reduce(mock_storage):
    worker = TaskWorker("worker-1", "localhost:50051")
    stub = MagicMock()
    
    # Mock a successful PollTask response with a Reduce task
    mock_task = task_pb2.ReduceTask(job_id="j1", task_id="r1")
    resp = task_pb2.PollTaskResponse(has_task=True, reduce_task=mock_task)
    stub.PollTask.return_value = resp
    
    with patch("worker.tasks.client.execute_reduce_task") as mock_exec:
        mock_exec.return_value = {"duration_ms": 200}
        worker._poll_and_execute(stub, "reduce")
        
        mock_exec.assert_called_once()
        stub.ReportTaskResult.assert_called_once()

def test_poll_no_task(mock_storage):
    worker = TaskWorker("worker-1", "localhost:50051")
    stub = MagicMock()
    stub.PollTask.return_value = task_pb2.PollTaskResponse(has_task=False)
    
    worker._poll_and_execute(stub, "map")
    stub.ReportTaskResult.assert_not_called()

def test_poll_error_handling(mock_storage):
    worker = TaskWorker("worker-1", "localhost:50051")
    stub = MagicMock()
    stub.PollTask.side_effect = Exception("network error")
    
    # Should not raise exception
    worker._poll_and_execute(stub, "map")
