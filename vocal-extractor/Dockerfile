FROM python:3.11-slim-bookworm@sha256:4b4c524dc3dce996864e030c7bd9c6b0e517597189fee48f48e05b499442444b AS builder

ENV PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1
RUN apt-get update && apt-get install -y --no-install-recommends build-essential libsamplerate0-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY requirements.txt .
RUN python -m pip install --upgrade pip && python -m pip wheel --wheel-dir /wheels -r requirements.txt

FROM python:3.11-slim-bookworm@sha256:4b4c524dc3dce996864e030c7bd9c6b0e517597189fee48f48e05b499442444b

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    NUMBA_CACHE_DIR=/tmp/numba-cache \
    INPUT_DIR=/tmp/vocal-extractor/input \
    OUTPUT_DIR=/tmp/vocal-extractor/output \
    MODEL_DIR=/data/models

RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg libsamplerate0 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 appuser \
    && mkdir -p /data/models /tmp/vocal-extractor /tmp/numba-cache \
    && chown -R appuser:appuser /data /tmp/numba-cache
WORKDIR /app
COPY --from=builder /wheels /wheels
COPY requirements.txt .
RUN python -m pip install --no-index --find-links /wheels -r requirements.txt && rm -rf /wheels
COPY --chown=appuser:appuser *.py .
COPY --chown=appuser:appuser data/models /data/models

USER appuser
CMD ["python", "worker.py"]
