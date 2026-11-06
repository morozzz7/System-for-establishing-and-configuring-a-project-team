# ML service: Python 3.12-slim + FastAPI + uvicorn.
# Phase 1.3: stub endpoints only. No heavy ML libs yet.

FROM python:3.12-slim AS runtime

WORKDIR /app

# Install deps first for layer caching.
COPY ml/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code.
COPY ml/ .

EXPOSE 8000

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=6 \
  CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:8000/healthz')"]

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
