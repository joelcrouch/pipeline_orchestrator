import time
import torch
import torch.nn as nn
import logging
from worker.storage.client import StorageClient

logger = logging.getLogger(__name__)

class TimeSeriesCNN(nn.Module):
    def __init__(self, in_channels=1, num_classes=10):
        super().__init__()
        self.conv1 = nn.Conv1d(in_channels, 32, kernel_size=3, padding=1)
        self.pool = nn.MaxPool1d(2)
        self.fc = nn.Linear(32 * 32, num_classes) # Assuming input size of 64

    def forward(self, x):
        x = self.pool(torch.relu(self.conv1(x)))
        x = x.view(x.size(0), -1)
        return self.fc(x)

def execute_timeseries_cnn_task(task, storage: StorageClient) -> dict:
    """Processes a window of time-series data using a 1D-CNN."""
    logger.info(f"Executing TimeSeriesCNNTask: {task.task_id}")
    
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = TimeSeriesCNN().to(device)
    
    t0 = time.perf_counter()
    
    # Simulate processing a batch of 1D signal windows
    # (batch_size, channels, length)
    batch = torch.randn(32, 1, 64).to(device)
    
    with torch.no_grad():
        features = model(batch)
        
    duration_ms = int((time.perf_counter() - t0) * 1000)
    
    # storage.save_npy(task.output_uri, features.cpu().numpy())
    
    return {
        "duration_ms": duration_ms,
        "device": str(device),
        "shape": list(features.shape),
    }
