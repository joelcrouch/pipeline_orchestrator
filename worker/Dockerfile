FROM mambaorg/micromamba:1.5.8

WORKDIR /app

COPY environment.yml .
RUN micromamba env create -f environment.yml && micromamba clean -afy

COPY requirements-pip.txt .
RUN micromamba run -n pipeline-worker pip install -r requirements-pip.txt

RUN micromamba install -n pipeline-worker -c conda-forge curl -y && micromamba clean -afy

COPY worker/ ./worker/

ENV PATH=/opt/conda/envs/pipeline-worker/bin:$PATH
ENV PYTHONUNBUFFERED=1

EXPOSE 8081

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

CMD ["micromamba", "run", "-n", "pipeline-worker", "python", "-m", "uvicorn", "worker.main:app", "--host", "0.0.0.0", "--port", "8081"]