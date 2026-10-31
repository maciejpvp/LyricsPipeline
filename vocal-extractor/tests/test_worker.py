import json
import hashlib
import sys
import signal
import threading
from dataclasses import replace
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parents[1]))

from config import Settings
from events import parse_s3_jobs
from sqs_queue import QueueMessage
from worker import Worker


@pytest.fixture
def send_sigterm():
    def send(worker):
        previous = signal.signal(signal.SIGTERM, worker.stop)
        try:
            signal.raise_signal(signal.SIGTERM)
        finally:
            signal.signal(signal.SIGTERM, previous)
    return send


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
        vocals = output_dir / "vocals.wav"
        instrumental = output_dir / "song_(Instrumental).wav"
        vocals.write_bytes(b"vocals")
        instrumental.write_bytes(b"instrumental")
        return vocals


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


@pytest.mark.parametrize("filename", ["song.wav", "another mix.mp3", "vocals_(Instrumental).wav"])
def test_worker_processes_and_acknowledges_after_manifest(tmp_path, filename):
    queue = FakeQueue(event_body(f"input/job-1/{filename}"))
    store = FakeStore()
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker._handle_message(queue.messages[0])
    assert queue.deleted == ["receipt"]
    assert store.uploads == [("media", "output/job-1/vocals.wav", b"vocals", "audio/wav")]
    assert store.manifests[0][2]["status"] == "success"
    assert store.manifests[0][2]["output_keys"] == ["output/job-1/vocals.wav"]
    assert list(settings(tmp_path).input_dir.iterdir()) == []
    assert list(settings(tmp_path).output_dir.iterdir()) == []


def test_worker_uses_configured_output_prefix(tmp_path):
    queue = FakeQueue(event_body())
    store = FakeStore()
    configured = replace(settings(tmp_path), output_prefix="results/stems/")
    Worker(configured, queue, store, FakeProcessor())._handle_message(queue.messages[0])
    assert store.uploads == [("media", "results/stems/job-1/vocals.wav", b"vocals", "audio/wav")]
    assert store.manifests[0][1] == "results/stems/job-1/manifest.json"
    assert store.manifests[0][2]["output_keys"] == ["results/stems/job-1/vocals.wav"]
    assert queue.deleted == ["receipt"]


def test_worker_processes_flat_input(tmp_path):
    queue = FakeQueue(event_body("input/rick.mp3"))
    store = FakeStore()
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker._handle_message(queue.messages[0])
    job_id = hashlib.sha256("media/input/rick.mp3".encode()).hexdigest()[:16]
    assert queue.deleted == ["receipt"]
    assert store.uploads == [("media", f"output/{job_id}/vocals.wav", b"vocals", "audio/wav")]
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


def test_shutdown_does_not_acknowledge_active_job(tmp_path, send_sigterm):
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
    thread = threading.Thread(target=worker.run)
    thread.start()
    assert started.wait(timeout=2)

    send_sigterm(worker)
    assert queue.deleted == []

    release.set()
    thread.join(timeout=2)
    assert not thread.is_alive()
    assert queue.deleted == ["receipt"]


def test_sigterm_during_receive_releases_message_without_processing(tmp_path, send_sigterm):
    store = FakeStore()

    class PollingQueue(FakeQueue):
        def receive(self, wait_seconds, visibility_timeout):
            # Match the registered signal handler while the long poll is open.
            send_sigterm(worker)
            return super().receive(wait_seconds, visibility_timeout)

    queue = PollingQueue(event_body())
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker.run()
    assert queue.extended == [("receipt", 0)]
    assert queue.deleted == []
    assert store.downloads == []
    assert store.manifests == []


def test_shutdown_release_failure_leaves_message_for_visibility_expiry(tmp_path, caplog):
    class UnavailableQueue(FakeQueue):
        def receive(self, wait_seconds, visibility_timeout):
            worker.stop()
            return super().receive(wait_seconds, visibility_timeout)

        def change_visibility(self, receipt_handle, timeout):
            raise RuntimeError("SQS unavailable")

    queue = UnavailableQueue(event_body())
    store = FakeStore()
    worker = Worker(settings(tmp_path), queue, store, FakeProcessor())
    worker.run()
    assert queue.deleted == []
    assert store.downloads == []
    assert "failed to return SQS message" in caplog.text


def test_no_acknowledgement_until_manifest_write_succeeds(tmp_path):
    queue = FakeQueue(event_body())

    class CheckingStore(FakeStore):
        def upload_file(self, *args):
            assert queue.deleted == []
            super().upload_file(*args)

        def upload_json(self, *args):
            assert queue.deleted == []
            assert len(self.uploads) == 1
            super().upload_json(*args)

    store = CheckingStore()
    Worker(settings(tmp_path), queue, store, FakeProcessor())._handle_message(queue.messages[0])
    assert queue.deleted == ["receipt"]


def test_manifest_failure_is_not_acknowledged(tmp_path):
    class UnavailableStore(FakeStore):
        def upload_json(self, *args):
            raise RuntimeError("S3 unavailable")

    queue = FakeQueue(event_body())
    store = UnavailableStore()
    Worker(settings(tmp_path), queue, store, FakeProcessor())._handle_message(queue.messages[0])
    assert len(store.uploads) == 1
    assert store.manifests == []
    assert queue.deleted == []


def test_audio_upload_failure_is_retried_before_manifest_and_ack(tmp_path):
    class UnavailableStore(FakeStore):
        def upload_file(self, *args):
            raise RuntimeError("S3 unavailable")

    queue = FakeQueue(event_body())
    store = UnavailableStore()
    Worker(settings(tmp_path), queue, store, FakeProcessor())._handle_message(queue.messages[0])
    assert store.uploads == []
    assert store.manifests == []
    assert queue.deleted == []
    assert list(settings(tmp_path).input_dir.iterdir()) == []
    assert list(settings(tmp_path).output_dir.iterdir()) == []

    store.upload_file = FakeStore.upload_file.__get__(store)
    redelivered = QueueMessage("message", "new-receipt", event_body(), 2)
    Worker(settings(tmp_path), queue, store, FakeProcessor())._handle_message(redelivered)
    assert store.uploads == [("media", "output/job-1/vocals.wav", b"vocals", "audio/wav")]
    assert len(store.manifests) == 1
    assert queue.deleted == ["new-receipt"]


def test_interrupted_job_is_redelivered_and_completed(tmp_path):
    # BaseException models forced process death: normal worker exception handling
    # must not acknowledge it. SQS visibility expiry itself is simulated below.
    class ForcedStop(BaseException):
        pass

    class InterruptedProcessor(FakeProcessor):
        def separate(self, input_path, output_dir):
            worker.stop()
            raise ForcedStop()

    queue = FakeQueue(event_body())
    store = FakeStore()
    worker = Worker(settings(tmp_path), queue, store, InterruptedProcessor())
    with pytest.raises(ForcedStop):
        worker._handle_message(queue.messages[0])
    assert queue.deleted == []
    assert store.manifests == []

    redelivered = QueueMessage("message", "new-receipt", event_body(), 2)
    Worker(settings(tmp_path), queue, store, FakeProcessor())._handle_message(redelivered)
    assert queue.deleted == ["new-receipt"]
    assert len(store.manifests) == 1
    assert len(store.downloads) == 2


def test_redelivery_after_manifest_but_before_ack_skips_completed_job(tmp_path):
    class AckFailureQueue(FakeQueue):
        def delete(self, receipt_handle):
            raise RuntimeError("connection lost before acknowledgement")

    queue = AckFailureQueue(event_body())
    store = FakeStore()
    Worker(settings(tmp_path), queue, store, FakeProcessor())._handle_message(queue.messages[0])
    assert len(store.manifests) == 1
    assert queue.deleted == []
    retry_queue = FakeQueue(event_body())
    Worker(settings(tmp_path), retry_queue, store, FakeProcessor())._handle_message(retry_queue.messages[0])
    assert retry_queue.deleted == ["receipt"]
    assert len(store.downloads) == 1
    assert len(store.manifests) == 1
