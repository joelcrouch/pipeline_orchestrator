import pytest
from worker.tasks.chunker import generate_map_tasks

def test_generate_map_tasks_exact():
    # A(4096x4096), B(4096x4096), block_size=1024
    # bi = 4, bj = 4, bk = 4
    # Total = 4 * 4 * 4 = 64
    tasks = generate_map_tasks("test_job", 4096, 4096, 4096, 1024)
    assert len(tasks) == 64
    
    # Check first task
    t0 = tasks[0]
    assert t0.task_id == "map_0_0_0"
    assert t0.i == 0
    assert t0.j == 0
    assert t0.k == 0
    assert t0.a_uri == "s3://pipeline-data/jobs/test_job/blocks/A_0_0.npy"
    assert t0.b_uri == "s3://pipeline-data/jobs/test_job/blocks/B_0_0.npy"
    assert t0.output_uri == "s3://pipeline-data/jobs/test_job/partial/C_0_0_0.npy"

def test_generate_map_tasks_padding():
    # A(100, 100), B(100, 100), block_size=60
    # bi = 2, bj = 2, bk = 2
    # Total = 8
    tasks = generate_map_tasks("pad_job", 100, 100, 100, 60)
    assert len(tasks) == 8
    
    # Check last task map_1_1_1
    t7 = tasks[7]
    assert t7.task_id == "map_1_1_1"
    assert t7.i == 1
    assert t7.j == 1
    assert t7.k == 1
