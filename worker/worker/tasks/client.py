import logging
import time
import grpc
import threading
from worker.gen import task_pb2, task_pb2_grpc
from worker.storage.client import StorageClient
from worker.tasks.map_task import execute_map_task
from worker.tasks.reduce_task import execute_reduce_task
from worker.tasks.recommender_task import execute_recommender_task
from worker.tasks.timeseries_cnn_task import execute_timeseries_cnn_task
from worker.tasks.llm_vision_task import execute_llm_vision_task

logger = logging.getLogger(__name__)

class TaskWorker:
    def __init__(self, worker_id: str, orchestrator_addr: str):
        self.worker_id = worker_id
        self.orchestrator_addr = orchestrator_addr
        self.storage = StorageClient()
        self._stop_event = threading.Event()

    def run(self):
        logger.info(f"TaskWorker started — polling {self.orchestrator_addr}")
        while not self._stop_event.is_set():
            try:
                with grpc.insecure_channel(self.orchestrator_addr) as channel:
                    stub = task_pb2_grpc.TaskServiceStub(channel)
                    
                    # Poll for Map tasks
                    self._poll_and_execute(stub, "map")
                    
                    # Poll for Reduce tasks
                    self._poll_and_execute(stub, "reduce")
                    
            except Exception as e:
                logger.error(f"TaskWorker loop error: {e}")
            
            time.sleep(1)

    def _poll_and_execute(self, stub, task_type: str):
        try:
            req = task_pb2.PollTaskRequest(worker_id=self.worker_id, task_type=task_type)
            resp = stub.PollTask(req, timeout=5)
            
            if resp.has_task:
                if task_type == "map":
                    task = resp.map_task
                    model_type = task.metadata.get("model_type", "matmul")
                    
                    if model_type == "recommender":
                        result = execute_recommender_task(task, self.storage)
                    elif model_type == "timeseries_cnn":
                        result = execute_timeseries_cnn_task(task, self.storage)
                    elif model_type == "llm_vision":
                        result = execute_llm_vision_task(task, self.storage)
                    else:
                        result = execute_map_task(task, self.storage)
                        
                    task_id = task.task_id
                    job_id = task.job_id
                else:
                    result = execute_reduce_task(resp.reduce_task, self.storage)
                    task_id = resp.reduce_task.task_id
                    job_id = resp.reduce_task.job_id
                
                # Report result
                report = task_pb2.ReportResultRequest(
                    job_id=job_id,
                    task_id=task_id,
                    success=True,
                    duration_ms=result.get("duration_ms", 0)
                )
                stub.ReportTaskResult(report, timeout=5)
                logger.info(f"Task {task_id} completed and reported")
                
        except grpc.RpcError as e:
            if e.code() != grpc.StatusCode.UNAVAILABLE:
                logger.warning(f"PollTask RPC error ({task_type}): {e}")
        except Exception as e:
            logger.error(f"Error executing {task_type} task: {e}")

    def stop(self):
        self._stop_event.set()
