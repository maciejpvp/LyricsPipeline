# Word transcription

Amazon Transcribe batch transcription, fixed to English (`en-US`). No ECS worker, model
container, VPC, NAT or database is required. Deploy `infra/transcription` after the base;
see [deployment commands](../README.md#fresh-deployment-base-and-transcription-only).

Both Lambda entrypoints are written in Go using `aws-lambda-go` and AWS SDK for Go v2.
`cmd/bootstrap` dispatches `prepare` or `finalize` through the required
`TRANSCRIPTION_ACTION` environment variable. The infrastructure uses `provided.al2023`
and explicitly selects ARM64. The owning layer uses `internal/lambdabuild` to compile a
static Linux `bootstrap` and create an executable, reproducible ZIP in ignored `bin/transcription/`; both functions use the
same package. No Python runtime or manual Lambda build step is required.

Run `go test ./transcription/...` for the runtime tests and `go test -race ./transcription/...`
for concurrent publication checks. The local `scripts/run-transcription.py` helper remains
Python and installs its dependencies from `scripts/requirements.txt`; it is not deployed.

## Execution and artifacts

An EventBridge `Object Created` event for the configured bucket and `output/*/vocals.wav`
starts a Standard Step Functions execution. The preparation Lambda validates the exact
`outputPrefix/job_id/vocals.wav` shape and current ETag, then conditionally creates
`output/{job_id}/transcription/source.json`. This pins the source even before transcription
succeeds. The extraction manifest is not read.

The run hash includes account, bucket/key/ETag, pipeline version and attempt ID. It produces
one deterministic Transcribe job name. The encoding preserves the previous run hashes,
including Unicode keys, so changing the Lambda implementation does not duplicate in-flight jobs. Duplicate executions either reuse the final result,
start the same provider job, or catch its name conflict and poll the existing job. Only
bounded transient errors are retried, up to five retries by default. The Go S3 adapter
returns `TransientError` for throttling, service failures, network errors and conditional
409 conflicts; validation and permanent permission failures are not retried. Polling is every 60 seconds;
the execution timeout is six hours. Values live in the transcription project's `Pulumi.yaml`.
Transcribe queueing is enabled with a dedicated data-access role.

Provider output: `output/{job_id}/transcription/runs/{run_id}/raw.json`.
Application output: `output/{job_id}/transcription/transcript.json`.
Only object references/status are passed through Step Functions; raw transcript JSON remains
in S3, avoiding the workflow payload limit. Lambdas process metadata/text, never download audio.

The final Lambda verifies the current source again, validates the raw provider job/status,
and conditionally publishes the final JSON. Concurrent identical publication is safe. A failed
normalization never publishes a success artifact. The ETag is an object identity token,
not an assertion that it is an MD5 content hash. New audio must use a new job ID. Source replacement
is rejected when observed; external overwrite/delete races are not prevented by S3 versioning or
a write-once bucket policy. The application contract requires immutable input objects.

A failed provider job remains failed for the same deterministic name. The manual helper accepts
`--attempt retry-1` after correcting its cause. A completed transcript is reused regardless of
attempt; a changed pipeline version requires a new job ID to avoid silently returning old output.

## JSON interface, version 1

```json
{
  "schema_version": 1,
  "pipeline_version": "1",
  "job_id": "example",
  "language": "en-US",
  "source": {"bucket": "media", "key": "output/example/vocals.wav", "etag": "source-etag"},
  "text": "Hello world!",
  "words": [
    {"text": "Hello", "start": 1.25, "end": 1.7, "confidence": 0.98},
    {"text": "world!", "start": 1.8, "end": 2.4, "confidence": 0.97}
  ]
}
```

`start` and `end` are finite numbers in **seconds from the beginning of vocals.wav**.
Leading silence is retained; no resampling, trimming, translation or offset is applied.
The player can highlight a word when `start <= currentTime < end`. Use `words` for timed
rendering and `text` for the full transcript. Punctuation attaches to words without generating
new timing intervals. Confidence is a number in [0,1], or null when absent from provider output.
An empty transcript is valid with an empty word list. Missing/invalid/non-monotonic times fail
normalization instead of being estimated. Simultaneous singers and ASR mistakes are not repaired.

The manual workflow input is `{"bucket":"...","key":"...","etag":"...","attempt":"default"}`.
ETag may be omitted for a manual request; preparation reads it from S3. EventBridge passes its
native event unchanged, including its expected ETag; its keys are not URL-decoded a second time.

## Failures, permissions and monitoring

EventBridge delivery has an explicit retry policy and SQS DLQ. A second rule sends workflow
`FAILED`, `TIMED_OUT` and `ABORTED` events to the same error queue. CloudWatch alarms cover
workflow failures/timeouts/aborts, both Lambda error metrics, and visible queue messages.
No external email/chat notification subscription is configured. Step Functions execution
history records the flow and Transcribe's failure reason.

The preparation/finalization roles can read vocal objects and transcription artifacts, list
only transcription prefixes, and write only transcription artifacts. The Transcribe data role
reads vocals and writes only raw run outputs. The workflow can invoke its two functions,
get `lyrics-*` Transcribe jobs, and pass only its Transcribe role to that service.
`StartTranscriptionJob` requires IAM resource `*`; its statement restricts the output bucket
and output-key pattern using Transcribe's supported condition keys.

Lambda logs use dedicated groups with configured retention. Completed-event logging contains
job/run IDs and word count, not the transcript text. The only queue consumer for errors is the
operator; fixing and rerunning a workflow is an explicit action, not an endless retry loop.

AWS references: [batch output](https://docs.aws.amazon.com/transcribe/latest/dg/how-input.html),
[Transcribe authorization](https://docs.aws.amazon.com/service-authorization/latest/reference/list_transcribe.html),
[SDK workflow integrations](https://docs.aws.amazon.com/step-functions/latest/dg/supported-services-awssdk.html).
