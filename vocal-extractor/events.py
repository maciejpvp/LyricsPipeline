from __future__ import annotations

import json
from dataclasses import dataclass
from urllib.parse import unquote_plus


@dataclass(frozen=True)
class S3Job:
    bucket: str
    key: str
    job_id: str
    filename: str


def parse_s3_jobs(body: str, input_prefix: str) -> list[S3Job]:
    document = json.loads(body)
    if "Records" not in document:
        if "Message" in document:
            return parse_s3_jobs(document["Message"], input_prefix)
        return []
    jobs: list[S3Job] = []
    for record in document["Records"]:
        if record.get("eventSource") != "aws:s3":
            continue
        bucket = record.get("s3", {}).get("bucket", {}).get("name")
        key = unquote_plus(record.get("s3", {}).get("object", {}).get("key", ""))
        if not bucket or not key.startswith(input_prefix):
            continue
        relative = key[len(input_prefix):]
        pieces = relative.split("/", 1)
        if len(pieces) == 2 and pieces[0] and pieces[1]:
            jobs.append(S3Job(bucket=bucket, key=key, job_id=pieces[0], filename=pieces[1]))
    return jobs
