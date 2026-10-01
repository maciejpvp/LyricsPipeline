# Vocal Extractor

Small CPU-only Docker service that separates vocals from an uploaded audio file with UVR-MDX-NET through `audio-separator`.

## Run with Docker Compose

The model is read from `data/models` and completed stems are written to `data/output/{job_id}`.

```bash
DOCKER_UID="$(id -u)" DOCKER_GID="$(id -g)" docker compose up --build
```

Submit a job:

```bash
curl -F file=@song.mp3 http://localhost:8080/v1/jobs
```

The response contains the accepted `job_id`. The worker runs asynchronously; inspect `data/output/{job_id}` for the vocal and instrumental WAV files after processing.

Supported formats are MP3, WAV, FLAC, M4A, OGG, AAC, and WMA. The upload limit defaults to 500 MiB and can be changed with `MAX_UPLOAD_BYTES`.

## Tests

```bash
python -m pip install -r requirements-dev.txt
pytest -q
```
