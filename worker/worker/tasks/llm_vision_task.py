import time
import torch
import torch.nn as nn
import logging
from worker.storage.client import StorageClient

logger = logging.getLogger(__name__)

class SimpleVisionLLM(nn.Module):
    def __init__(self, vision_dim=512, text_dim=512, proj_dim=256):
        super().__init__()
        # Vision projection (simulating frozen backbone output)
        self.vision_proj = nn.Linear(vision_dim, proj_dim)
        # Text projection (simulating frozen LLM token output)
        self.text_proj = nn.Linear(text_dim, proj_dim)

    def forward(self, vision_feat, text_feat):
        v_emb = self.vision_proj(vision_feat)
        t_emb = self.text_proj(text_feat)
        # L2-normalize for cosine similarity
        v_emb = v_emb / v_emb.norm(dim=-1, keepdim=True)
        t_emb = t_emb / t_emb.norm(dim=-1, keepdim=True)
        return v_emb, t_emb

def execute_llm_vision_task(task, storage: StorageClient) -> dict:
    """Simulates a fine-tuning/alignment step for a vision-language model."""
    logger.info(f"Executing LLMVisionTask: {task.task_id}")
    
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = SimpleVisionLLM().to(device)
    optimizer = torch.optim.Adam(model.parameters(), lr=1e-5)
    
    t0 = time.perf_counter()
    
    # Simulate a batch of image and text features from an LLM and Vision Backbone
    batch_size = 16
    vision_feats = torch.randn(batch_size, 512).to(device)
    text_feats = torch.randn(batch_size, 512).to(device)
    
    optimizer.zero_grad()
    v_emb, t_emb = model(vision_feats, text_feats)
    
    # Simulate a contrastive loss step (simple version)
    logits = v_emb @ t_emb.T
    targets = torch.arange(batch_size).to(device)
    loss = nn.CrossEntropyLoss()(logits, targets)
    
    loss.backward()
    optimizer.step()
    
    duration_ms = int((time.perf_counter() - t0) * 1000)
    
    # storage.save_npy(task.output_uri, model.vision_proj.weight.cpu().detach().numpy())
    
    return {
        "duration_ms": duration_ms,
        "loss": float(loss.item()),
        "device": str(device),
    }
