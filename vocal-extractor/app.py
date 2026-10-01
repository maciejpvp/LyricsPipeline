from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
import json
import logging
import os
import subprocess
import uuid
from pathlib import Path
from typing import Any

from fastapi import FastAPI, File, HTTPException, UploadFile


logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"), format="%(message)s")
logger = logging.getLogger("vocal-extractor")
SUPPORTED_EXTENSIONS = {".mp3", ".wav", ".flac", ".m4a", ".ogg", ".aac", ".wma"}


class Settings:
    input_dir = Path(os.getenv("INPUT_DIR", "/data/input"))
    output_dir = Path(os.getenv("OUTPUT_DIR", "/data/output"))
    model_dir = Path(os.getenv("MODEL_DIR", "/data/models"))
    model_filename = os.getenv("MODEL_FILENAME", "UVR-MDX-NET-Voc_FT.onnx")
    max_upload_bytes = int(os.getenv("MAX_UPLOAD_BYTES", str(500 * 1024 * 1024)))
    ffprobe_timeout_seconds = int(os.getenv("FFPROBE_TIMEOUT_SECONDS", "30"))


settings = Settings()
tasks: set[asyncio.Task[Any]] = set()


@asynccontextmanager
async def lifespan(_: FastAPI):
    for directory in (settings.input_dir, settings.output_dir, settings.model_dir):
        directory.mkdir(parents=True, exist_ok=True)
    yield
    for task in list(tasks):
        task.cancel()
    if tasks:
        await asyncio.gather(*tasks, return_exceptions=True)


app = FastAPI(title="Vocal Extractor", version="1.0.0", docs_url=None, redoc_url=None, lifespan=lifespan)


def _validate_name(name: str) -> str:
    normalized = Path(name).name
    if normalized != name or Path(normalized).suffix.lower() not in SUPPORTED_EXTENSIONS:
        raise HTTPException(status_code=415, detail="unsupported audio file type")
    return normalized


def _validate_audio(path: Path) -> None:
    result = subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "format=format_name", "-of", "json", str(path)],
        capture_output=True,
        text=True,
        timeout=settings.ffprobe_timeout_seconds,
        check=False,
    )
    if result.returncode != 0 or not result.stdout.strip():
        raise ValueError("invalid_audio")


def _run_separator(input_path: Path, output_dir: Path) -> list[Path]:
    from audio_separator.separator import Separator

    separator = Separator(output_dir=str(output_dir), output_format="WAV", model_file_dir=str(settings.model_dir))
    separator.load_model(model_filename=settings.model_filename)
    return [Path(path) for path in separator.separate(str(input_path))]


def _process(job_id: str, input_path: Path, output_dir: Path) -> None:
    _validate_audio(input_path)
    output_dir.mkdir(parents=True, exist_ok=True)
    output_root = output_dir.resolve()
    outputs = []
    for path in _run_separator(input_path, output_dir):
        candidate = path if path.is_absolute() else output_dir / path
        resolved = candidate.resolve()
        if resolved.parent != output_root:
            raise RuntimeError("separator produced an unsafe output path")
        outputs.append(resolved)
    if not any("vocal" in path.name.lower() for path in outputs):
        raise RuntimeError("separator did not produce a vocal stem")
    for path in outputs:
        if not path.is_file():
            raise RuntimeError("separator produced an unsafe output path")
    logger.info(json.dumps({"event": "job_completed", "job_id": job_id, "outputs": [path.name for path in outputs]}))


async def _execute(job_id: str, input_path: Path, output_dir: Path) -> None:
    try:
        logger.info(json.dumps({"event": "job_started", "job_id": job_id}))
        await asyncio.to_thread(_process, job_id, input_path, output_dir)
    except asyncio.CancelledError:
        logger.info(json.dumps({"event": "job_cancelled", "job_id": job_id}))
        raise
    except Exception:
        logger.exception(json.dumps({"event": "job_failed", "job_id": job_id}))
        if output_dir.exists():
            for path in output_dir.iterdir():
                if path.is_file():
                    path.unlink(missing_ok=True)
            output_dir.rmdir()
    finally:
        input_path.unlink(missing_ok=True)


def _track(task: asyncio.Task[Any]) -> None:
    tasks.add(task)
    task.add_done_callback(tasks.discard)


@app.post("/v1/jobs", status_code=202)
async def create_job(file: UploadFile | None = File(default=None)) -> dict[str, str]:
    if file is None:
        raise HTTPException(status_code=400, detail="multipart audio file is required")

    name = _validate_name(file.filename or "upload.bin")
    job_id = uuid.uuid4().hex
    input_path = settings.input_dir / f"{job_id}{Path(name).suffix.lower()}"
    output_dir = settings.output_dir / job_id
    settings.input_dir.mkdir(parents=True, exist_ok=True)

    size = 0
    try:
        with input_path.open("wb") as destination:
            while chunk := await file.read(1024 * 1024):
                size += len(chunk)
                if size > settings.max_upload_bytes:
                    raise HTTPException(status_code=413, detail="uploaded file exceeds MAX_UPLOAD_BYTES")
                destination.write(chunk)
    except HTTPException:
        input_path.unlink(missing_ok=True)
        raise
    except Exception:
        input_path.unlink(missing_ok=True)
        raise
    finally:
        await file.close()

    _track(asyncio.create_task(_execute(job_id, input_path, output_dir)))
    return {"job_id": job_id, "status": "accepted"}
