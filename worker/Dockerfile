FROM continuumio/miniconda3:24.1.2-0

WORKDIR /app

# Copy and create conda environment
COPY environment.yml .
RUN conda env create -f environment.yml && conda clean -afy

# Activate env by default
SHELL ["conda", "run", "-n", "pipeline-worker", "/bin/bash", "-c"]

# Install any pip-only deps
COPY requirements-pip.txt .
RUN pip install -r requirements-pip.txt

# Copy source
COPY worker/ ./worker/

ENV PATH=/opt/conda/envs/pipeline-worker/bin:$PATH
ENV PYTHONUNBUFFERED=1

EXPOSE 8081

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

CMD ["python", "-m", "uvicorn", "worker.main:app", "--host", "0.0.0.0", "--port", "8081"]