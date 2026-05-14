import time
import torch
import torch.nn as nn
import logging
from worker.storage.client import StorageClient

logger = logging.getLogger(__name__)

class MatrixFactorization(nn.Module):
    def __init__(self, n_users, n_items, n_factors=20):
        super().__init__()
        self.user_factors = nn.Embedding(n_users, n_factors)
        self.item_factors = nn.Embedding(n_items, n_factors)

    def forward(self, user, item):
        return (self.user_factors(user) * self.item_factors(item)).sum(1)

def execute_recommender_task(task, storage: StorageClient) -> dict:
    """Trains a slice of the recommender system matrix."""
    logger.info(f"Executing RecommenderTask: {task.task_id}")
    
    # In a real scenario, we'd load user/item interaction data from storage
    # For this skeleton, we'll simulate a training step
    n_users, n_items = 1000, 1000
    n_factors = 20
    
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = MatrixFactorization(n_users, n_items, n_factors).to(device)
    optimizer = torch.optim.SGD(model.parameters(), lr=0.01)
    criterion = nn.MSELoss()
    
    t0 = time.perf_counter()
    
    # Simulate training on a batch
    users = torch.randint(0, n_users, (64,)).to(device)
    items = torch.randint(0, n_items, (64,)).to(device)
    ratings = torch.rand(64).to(device)
    
    optimizer.zero_grad()
    preds = model(users, items)
    loss = criterion(preds, ratings)
    loss.backward()
    optimizer.step()
    
    duration_ms = int((time.perf_counter() - t0) * 1000)
    
    # Save the updated embeddings (or a slice) to storage
    # storage.save_npy(task.output_uri, model.user_factors.weight.cpu().detach().numpy())
    
    return {
        "duration_ms": duration_ms,
        "loss": float(loss.item()),
        "device": str(device),
    }
