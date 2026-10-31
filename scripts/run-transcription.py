#!/usr/bin/env python3
"""Start ASR for an existing S3 vocal stem, without deploying or invoking separation."""
import argparse
import json
from pathlib import Path
import subprocess
import uuid

import boto3


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stack", required=True, help="Fully qualified transcription stack")
    parser.add_argument("--s3-key", required=True)
    parser.add_argument("--attempt", default="default", help="Use a new identifier to retry a FAILED Transcribe job")
    args = parser.parse_args()
    project = Path(__file__).resolve().parents[1] / "infra/transcription"
    output = subprocess.run(["pulumi", "-C", str(project), "stack", "output", "--json", "--stack", args.stack],
                            check=True, text=True, capture_output=True)
    values = json.loads(output.stdout)
    session = boto3.Session(region_name=values["region"])
    account = session.client("sts").get_caller_identity()["Account"]
    if account != values["accountId"]:
        raise ValueError("AWS credentials belong to a different account than the transcription stack")
    key, bucket = args.s3_key, values["mediaBucket"]
    prefix = values["outputPrefix"] + "/"
    relative = key.removeprefix(prefix).split("/")
    if not key.startswith(prefix) or len(relative) != 2 or not relative[0] or relative[1] != "vocals.wav":
        raise ValueError("expected outputPrefix/job_id/vocals.wav")
    head = session.client("s3").head_object(Bucket=bucket, Key=key)
    event = {"bucket": bucket, "key": key, "etag": head["ETag"].strip('"'), "attempt": args.attempt}
    result = session.client("stepfunctions").start_execution(stateMachineArn=values["stateMachineArn"],
                 name="manual-" + uuid.uuid4().hex, input=json.dumps(event))
    print(result["executionArn"])
    print(f"Result: s3://{bucket}/{prefix}{relative[0]}/transcription/transcript.json")


if __name__ == "__main__":
    main()
