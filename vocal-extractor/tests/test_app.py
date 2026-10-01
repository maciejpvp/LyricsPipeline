import asyncio
import importlib
import sys
from pathlib import Path

import httpx
import pytest


@pytest.fixture()
def service(tmp_path, monkeypatch):
    monkeypatch.setenv("INPUT_DIR", str(tmp_path / "input"))
    monkeypatch.setenv("OUTPUT_DIR", str(tmp_path / "output"))
    monkeypatch.setenv("MODEL_DIR", str(tmp_path / "models"))
    monkeypatch.setenv("MAX_UPLOAD_BYTES", "10")
    sys.path.insert(0, str(Path(__file__).parents[1]))
    sys.modules.pop("app", None)
    return importlib.import_module("app")


class FakeUpload:
    def __init__(self, content: bytes, filename: str):
        self.content = content
        self.filename = filename

    async def read(self, _: int) -> bytes:
        content, self.content = self.content, b""
        return content

    async def close(self) -> None:
        return None


def test_rejects_missing_file(service):
    async def scenario():
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=service.app), base_url="http://test") as client:
            return await client.post("/v1/jobs")

    response = asyncio.run(scenario())
    assert response.status_code == 400


def test_rejects_unsupported_audio(service):
    async def scenario():
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=service.app), base_url="http://test") as client:
            return await client.post("/v1/jobs", files={"file": ("song.txt", b"audio")})

    response = asyncio.run(scenario())
    assert response.status_code == 415


def test_rejects_oversized_upload(service):
    async def scenario():
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=service.app), base_url="http://test") as client:
            return await client.post("/v1/jobs", files={"file": ("song.wav", b"01234567890")})

    response = asyncio.run(scenario())
    assert response.status_code == 413


def test_job_writes_outputs_and_removes_input(service, monkeypatch):
    monkeypatch.setattr(service, "_validate_audio", lambda path: None)

    async def run_inline(function, *args):
        return function(*args)

    monkeypatch.setattr(service.asyncio, "to_thread", run_inline)

    def fake_separator(input_path, output_dir):
        vocals = output_dir / "song_(Vocals).wav"
        instrumental = output_dir / "song_(Instrumental).wav"
        vocals.write_bytes(b"vocals")
        instrumental.write_bytes(b"instrumental")
        return [vocals, instrumental]

    monkeypatch.setattr(service, "_run_separator", fake_separator)
    async def scenario():
        response = await service.create_job(FakeUpload(b"audio", "song.wav"))
        job_id = response["job_id"]
        output_dir = service.settings.output_dir / job_id
        await asyncio.gather(*service.tasks)
        return output_dir

    output_dir = asyncio.run(scenario())

    assert (output_dir / "song_(Vocals).wav").read_bytes() == b"vocals"
    assert (output_dir / "song_(Instrumental).wav").read_bytes() == b"instrumental"
    assert not any(service.settings.input_dir.iterdir())


def test_job_accepts_relative_separator_outputs(service, monkeypatch):
    monkeypatch.setattr(service, "_validate_audio", lambda path: None)

    async def run_inline(function, *args):
        return function(*args)

    monkeypatch.setattr(service.asyncio, "to_thread", run_inline)

    def fake_separator(input_path, output_dir):
        (output_dir / "song_(Vocals).wav").write_bytes(b"vocals")
        (output_dir / "song_(Instrumental).wav").write_bytes(b"instrumental")
        return [Path("song_(Vocals).wav"), Path("song_(Instrumental).wav")]

    monkeypatch.setattr(service, "_run_separator", fake_separator)

    async def scenario():
        response = await service.create_job(FakeUpload(b"audio", "song.wav"))
        output_dir = service.settings.output_dir / response["job_id"]
        await asyncio.gather(*service.tasks)
        return output_dir

    output_dir = asyncio.run(scenario())
    assert (output_dir / "song_(Vocals).wav").exists()
    assert (output_dir / "song_(Instrumental).wav").exists()


def test_failed_job_removes_partial_outputs_and_input(service, monkeypatch):
    monkeypatch.setattr(service, "_validate_audio", lambda path: None)

    async def run_inline(function, *args):
        return function(*args)

    monkeypatch.setattr(service.asyncio, "to_thread", run_inline)

    def failed_separator(input_path, output_dir):
        (output_dir / "partial.wav").write_bytes(b"partial")
        raise RuntimeError("separator failed")

    monkeypatch.setattr(service, "_run_separator", failed_separator)
    async def scenario():
        response = await service.create_job(FakeUpload(b"audio", "song.wav"))
        job_id = response["job_id"]
        output_dir = service.settings.output_dir / job_id
        await asyncio.gather(*service.tasks)
        return output_dir

    output_dir = asyncio.run(scenario())

    assert not output_dir.exists()
    assert not any(service.settings.input_dir.iterdir())
