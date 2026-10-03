from __future__ import annotations

import json
from pathlib import Path
from typing import Protocol


class ObjectStore(Protocol):
    def download(self, bucket: str, key: str, destination: Path) -> None: ...
    def exists(self, bucket: str, key: str) -> bool: ...
    def upload_file(self, bucket: str, key: str, source: Path, content_type: str) -> None: ...
    def upload_json(self, bucket: str, key: str, document: dict) -> None: ...


class S3Store:
    def __init__(self, client):
        self.client = client

    def download(self, bucket: str, key: str, destination: Path) -> None:
        destination.parent.mkdir(parents=True, exist_ok=True)
        self.client.download_file(bucket, key, str(destination))

    def exists(self, bucket: str, key: str) -> bool:
        try:
            self.client.head_object(Bucket=bucket, Key=key)
            return True
        except self.client.exceptions.ClientError as error:
            if error.response.get("Error", {}).get("Code") in {"404", "NoSuchKey", "NotFound"}:
                return False
            raise

    def upload_file(self, bucket: str, key: str, source: Path, content_type: str) -> None:
        self.client.upload_file(str(source), bucket, key, ExtraArgs={"ContentType": content_type})

    def upload_json(self, bucket: str, key: str, document: dict) -> None:
        self.client.put_object(
            Bucket=bucket,
            Key=key,
            Body=json.dumps(document, sort_keys=True).encode("utf-8"),
            ContentType="application/json",
        )
