from dataclasses import dataclass
import os
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    aws_region: str
    media_bucket: str
    queue_url: str
    input_prefix: str
    output_prefix: str
    input_dir: Path
    output_dir: Path
    model_dir: Path
    model_filename: str
    ffprobe_timeout_seconds: int
    sqs_wait_seconds: int
    visibility_timeout_seconds: int
    visibility_extension_seconds: int
    visibility_heartbeat_seconds: int
    log_level: str

    @classmethod
    def from_env(cls) -> "Settings":
        def required(name: str) -> str:
            value = os.getenv(name, "").strip()
            if not value:
                raise ValueError(f"{name} is required")
            return value

        return cls(
            aws_region=os.getenv("AWS_REGION", "eu-central-1"),
            media_bucket=required("MEDIA_BUCKET"),
            queue_url=required("QUEUE_URL"),
            input_prefix=os.getenv("INPUT_PREFIX", "input").strip("/") + "/",
            output_prefix=os.getenv("OUTPUT_PREFIX", "output").strip("/") + "/",
            input_dir=Path(os.getenv("INPUT_DIR", "/tmp/vocal-extractor/input")),
            output_dir=Path(os.getenv("OUTPUT_DIR", "/tmp/vocal-extractor/output")),
            model_dir=Path(os.getenv("MODEL_DIR", "/data/models")),
            model_filename=os.getenv("MODEL_FILENAME", "UVR-MDX-NET-Voc_FT.onnx"),
            ffprobe_timeout_seconds=int(os.getenv("FFPROBE_TIMEOUT_SECONDS", "30")),
            sqs_wait_seconds=int(os.getenv("SQS_WAIT_SECONDS", "20")),
            visibility_timeout_seconds=int(os.getenv("VISIBILITY_TIMEOUT_SECONDS", "900")),
            visibility_extension_seconds=int(os.getenv("VISIBILITY_EXTENSION_SECONDS", "600")),
            visibility_heartbeat_seconds=int(os.getenv("VISIBILITY_HEARTBEAT_SECONDS", "120")),
            log_level=os.getenv("LOG_LEVEL", "INFO"),
        )
