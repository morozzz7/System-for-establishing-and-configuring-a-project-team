# ML service: Python 3.12-slim + FastAPI + uvicorn.
# Phase 3.1: extraction pipeline with LLM integration.

FROM python:3.12-slim AS runtime

WORKDIR /app

# Install deps first for layer caching.
COPY ml/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code.
COPY ml/ .

# Copy taxonomy (required for extraction).
COPY contracts/taxonomy.yaml /app/contracts/taxonomy.yaml

# Set taxonomy path for the app.
ENV TAXONOMY_PATH=/app/contracts/taxonomy.yaml

EXPOSE 8000

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=6 \
  CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:8000/healthz')"]

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
