import json
import hashlib
import sys
import threading
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parents[1]))

from config import Settings
from events import parse_s3_jobs
from sqs_queue import QueueMessage
from worker import Worker


class FakeQueue:
    def __init__(self, body):
        self.messages = [QueueMessage("message", "receipt", body, 1)]
        self.deleted = []
        self.extended = []

    def receive(self, wait_seconds, visibility_timeout):
        if self.messages:
            return [self.messages.pop(0)]
        return []

    def delete(self, receipt_handle):
        self.deleted.append(receipt_handle)

    def change_visibility(self, receipt_handle, timeout):
        self.extended.append((receipt_handle, timeout))


class FakeStore:
    def __init__(self):
        self.downloads = []
        self.uploads = []
        self.manifests = []
        self.completed = set()

    def download(self, bucket, key, destination):
        self.downloads.append((bucket, key))
        destination.write_bytes(b"audio")

    def exists(self, bucket, key):
        return key in self.completed

    def upload_file(self, bucket, key, source, content_type):
        self.uploads.append((bucket, key, source.read_bytes(), content_type))

    def upload_json(self, bucket, key, document):
        self.manifests.append((bucket, key, document))
        self.completed.add(key)


class FakeProcessor:
    def validate_name(self, name):
        assert name.endswith((".wav", ".mp3"))

    def validate_audio(self, path):
        assert path.exists()

    def separate(self, input_path, output_dir):
        vocals = output_dir / "song_(Vocals).wav"
        instrumental = output_dir / "song_(Instrumental).wav"
        vocals.write_bytes(b"vocals")
        instrumental.write_bytes(b"instrumental")
        return [vocals, instrumental]


def settings(tmp_path):
    return Settings("eu-central-1", "media", "queue", "input/", "output/",
                    tmp_path / "input", tmp_path / "output", tmp_path / "models",
                    "model.onnx", 30, 0, 60, 60, 1, "INFO")


def event_body(key="input/job-1/song.wav"):
    return json.dumps({"Records": [{"eventSource": "aws:s3", "s3": {
        "bucket": {"name": "media"}, "object": {"key": key.replace(" ", "+")}}}]})


def test_event_parser_decodes_keys_and_filters_prefix():
    jobs = parse_s3_jobs(event_body("input/job-1/song+mix.wav"), "input/")
    assert jobs[0].job_id == "job-1"
    assert jobs[0].filename == "song mix.wav"
    assert parse_s3_jobs(event_body("output/job-1/result.wav"), "input/") == []


def test_event_parser_accepts_flat_input_and_generates_stable_job_id():
    jobs = parse_s3_jobs(event_body("input/rick.mp3"), "input/")
    expected = hashlib.sha256("media/input/rick.mp3".encode()).hexdigest()[:16]
    assert jobs[0].job_id == expected
    assert jobs[0].filename == "rick.mp3"
    assert parse_s3_jobs(event_body("input/rick.mp3"), "input/") == jobs


def test_event_parser_rejects_malformed_notification():
    with pytest.raises(ValueError, match="no Records"):
        parse_s3_jobs(json.dumps({"event": "not-s3"}), "input/")


def test_worker_processes_and_acknowledges_after_manifest(tmp_path):
    queue = FakeQueue(event_body())
    store = FakeStore()
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker._handle_message(queue.messages[0])
    assert queue.deleted == ["receipt"]
    assert [item[1] for item in store.uploads] == [
        "output/job-1/song_(Vocals).wav", "output/job-1/song_(Instrumental).wav"
    ]
    assert store.manifests[0][2]["status"] == "success"


def test_worker_processes_flat_input(tmp_path):
    queue = FakeQueue(event_body("input/rick.mp3"))
    store = FakeStore()
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker._handle_message(queue.messages[0])
    job_id = hashlib.sha256("media/input/rick.mp3".encode()).hexdigest()[:16]
    assert queue.deleted == ["receipt"]
    assert store.manifests[0][1] == f"output/{job_id}/manifest.json"


def test_worker_does_not_acknowledge_malformed_notification(tmp_path, caplog):
    queue = FakeQueue(json.dumps({"event": "not-s3"}))
    worker = Worker(settings(tmp_path), queue, FakeStore(), FakeProcessor())
    worker._handle_message(queue.messages[0])
    assert queue.deleted == []
    assert '"event": "message_ignored"' in caplog.text


def test_worker_does_not_ack_failed_job(tmp_path):
    queue = FakeQueue(event_body())
    store = FakeStore()

    class FailingProcessor(FakeProcessor):
        def separate(self, input_path, output_dir):
            raise RuntimeError("separator failed")

    worker = Worker(settings(tmp_path), queue, store, FailingProcessor())
    worker._handle_message(queue.messages[0])
    assert queue.deleted == []


def test_duplicate_manifest_is_acknowledged_without_reprocessing(tmp_path):
    queue = FakeQueue(event_body())
    store = FakeStore()
    store.completed.add("output/job-1/manifest.json")
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker._handle_message(queue.messages[0])
    assert queue.deleted == ["receipt"]
    assert store.downloads == []


def test_shutdown_does_not_acknowledge_active_job(tmp_path):
    queue = FakeQueue(event_body())
    store = FakeStore()
    started = threading.Event()
    release = threading.Event()

    class SlowProcessor(FakeProcessor):
        def separate(self, input_path, output_dir):
            started.set()
            assert release.wait(timeout=2)
            return super().separate(input_path, output_dir)

    worker = Worker(settings(tmp_path), queue, store, SlowProcessor())
    thread = threading.Thread(target=worker._handle_message, args=(queue.messages[0],))
    thread.start()
    assert started.wait(timeout=2)

    worker.stop()
    time.sleep(0.05)
    assert queue.deleted == []

    release.set()
    thread.join(timeout=2)
    assert not thread.is_alive()
    assert queue.deleted == ["receipt"]
