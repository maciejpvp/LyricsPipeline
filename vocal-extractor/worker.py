from __future__ import annotations

import json
import logging
import signal
import threading
from datetime import datetime, timezone
from pathlib import Path

import boto3

from audio import AudioProcessor
from config import Settings
from events import S3Job, parse_s3_jobs
from sqs_queue import MessageQueue, QueueMessage, SQSQueue
from storage import ObjectStore, S3Store

logger = logging.getLogger("vocal-extractor")


class VisibilityExtender:
    def __init__(self, queue: MessageQueue, receipt_handle: str, timeout: int, heartbeat: int):
        self.queue, self.receipt_handle = queue, receipt_handle
        self.timeout, self.heartbeat = timeout, heartbeat
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self._run, name="sqs-visibility", daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.stop_event.set()
        self.thread.join(timeout=self.heartbeat + 1)

    def _run(self) -> None:
        while not self.stop_event.wait(self.heartbeat):
            try:
                self.queue.change_visibility(self.receipt_handle, self.timeout)
            except Exception:
                logger.exception("failed to extend SQS visibility timeout")


class Worker:
    def __init__(self, settings: Settings, queue: MessageQueue, store: ObjectStore, processor: AudioProcessor):
        self.settings, self.queue, self.store, self.processor = settings, queue, store, processor
        self.stop_event = threading.Event()

    def stop(self, *_):
        logger.info(json.dumps({"event": "shutdown_requested"}))
        self.stop_event.set()

    def run(self) -> None:
        self.settings.input_dir.mkdir(parents=True, exist_ok=True)
        self.settings.output_dir.mkdir(parents=True, exist_ok=True)
        logger.info(json.dumps({"event": "worker_started"}))
        while not self.stop_event.is_set():
            for message in self.queue.receive(self.settings.sqs_wait_seconds, self.settings.visibility_timeout_seconds):
                if self.stop_event.is_set():
                    return
                self._handle_message(message)
        logger.info(json.dumps({"event": "worker_stopped"}))

    def _handle_message(self, message: QueueMessage) -> None:
        try:
            jobs = [job for job in parse_s3_jobs(message.body, self.settings.input_prefix)
                    if job.bucket == self.settings.media_bucket]
            if not jobs:
                self.queue.delete(message.receipt_handle)
                return
            with VisibilityExtender(self.queue, message.receipt_handle,
                                    self.settings.visibility_extension_seconds,
                                    self.settings.visibility_heartbeat_seconds):
                for job in jobs:
                    self.process_job(job)
            self.queue.delete(message.receipt_handle)
        except Exception:
            logger.exception(json.dumps({"event": "message_failed", "message_id": message.message_id}))

    def process_job(self, job: S3Job) -> None:
        self.processor.validate_name(job.filename)
        manifest_key = f"{self.settings.output_prefix}{job.job_id}/manifest.json"
        if self.store.exists(job.bucket, manifest_key):
            logger.info(json.dumps({"event": "job_duplicate", "job_id": job.job_id}))
            return
        input_path = self.settings.input_dir / f"{job.job_id}{Path(job.filename).suffix.lower()}"
        output_dir = self.settings.output_dir / job.job_id
        try:
            logger.info(json.dumps({"event": "job_started", "job_id": job.job_id, "key": job.key}))
            input_path.parent.mkdir(parents=True, exist_ok=True)
            self.store.download(job.bucket, job.key, input_path)
            self.processor.validate_audio(input_path)
            output_dir.mkdir(parents=True, exist_ok=True)
            outputs = self.processor.separate(input_path, output_dir)
            output_keys = []
            for output in outputs:
                key = f"{self.settings.output_prefix}{job.job_id}/{output.name}"
                self.store.upload_file(job.bucket, key, output, "audio/wav")
                output_keys.append(key)
            self.store.upload_json(job.bucket, manifest_key, {
                "job_id": job.job_id, "source_key": job.key, "output_keys": output_keys,
                "status": "completed", "completed_at": datetime.now(timezone.utc).isoformat(),
            })
            logger.info(json.dumps({"event": "job_completed", "job_id": job.job_id, "outputs": output_keys}))
        finally:
            input_path.unlink(missing_ok=True)
            if output_dir.exists():
                for path in output_dir.iterdir():
                    path.unlink(missing_ok=True)
                output_dir.rmdir()


def build_worker() -> Worker:
    settings = Settings.from_env()
    logging.basicConfig(level=settings.log_level, format="%(message)s")
    session = boto3.Session(region_name=settings.aws_region)
    return Worker(settings, SQSQueue(session.client("sqs"), settings.queue_url),
                  S3Store(session.client("s3")),
                  AudioProcessor(settings.model_dir, settings.model_filename,
                                  settings.ffprobe_timeout_seconds))


if __name__ == "__main__":
    worker = build_worker()
    signal.signal(signal.SIGTERM, worker.stop)
    signal.signal(signal.SIGINT, worker.stop)
    worker.run()
