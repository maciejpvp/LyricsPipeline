# Vocal Extractor

CPU-only S3/SQS worker that separates vocals from audio with UVR-MDX-NET through `audio-separator`.

## Job contract

Upload source audio to either `s3://$MEDIA_BUCKET/input/{filename}` or `s3://$MEDIA_BUCKET/input/{job_id}/{filename}`. For a flat `input/{filename}` key, the worker derives a stable 16-character job ID from the bucket and object key. S3 sends the object-created notification to SQS, and Fargate workers process one message at a time. Each completed job writes only `vocals.wav` and `manifest.json` to `output/{job_id}/`. The audio filename is always `vocals.wav`, regardless of the input filename; the manifest's `output_keys` contains only that audio object's key. The separator saves only the vocal stem, and no instrumental is uploaded. `OUTPUT_PREFIX` can override the default `output/` prefix. Source objects are retained.

Existing S3 results are left unchanged. Jobs with an existing manifest are skipped as before, so updating the worker does not rename old results or reprocess completed jobs.

The worker acknowledges a message only after the successful manifest is uploaded. Failed or interrupted messages can be delivered up to five times by default (`maxReceiveCount=5`), then move to the dead-letter queue. On redelivery the worker checks for a success manifest and skips completed jobs. This is at-least-once processing: concurrent duplicates can still perform the same work; there is no exactly-once guarantee or distributed lock.

Use `worker_started`, `job_started`, and `job_succeeded` to follow a job in the ECS logs. Invalid notifications are reported as `message_ignored`; valid notifications for another bucket or prefix are also logged before being acknowledged.

The service uses **Fargate Spot only**, with a default minimum of zero and maximum of two workers (2 vCPU, 8 GiB each). The cycle is `0 → queued work → processing → 5 idle minutes → 0`. Native CloudWatch metric math and Step Scaling add one worker with a shared 300-second cooldown. At zero reported capacity, any visible work wakes the first worker if in-flight is explicitly zero; ECS counts may be absent at zero. Once running, expansion requires `running=desired>0`, `pending=0`, and at least `backlogPerWorker=5` visible messages per running worker: 5 waiting for one worker, 10 for two, 15 for three. Active jobs are excluded. Standard Container Insights provides the ECS counts; starting capacity and missing readiness metrics suppress normal expansion. No message-age trigger, Lambda, schedule or database is used. Metrics are approximate and delayed; an outage of all ECS counts can resemble zero capacity, so this is not an instantaneous capacity guarantee.

A second alarm sets capacity exactly to `minTasks` after `idleMinutes` consecutive 60-second periods (default five) with both visible and in-flight counts reported as zero. Each count uses the minute's maximum. Missing data prevents scale-in, including when just one input metric is absent: the expression fills each missing count with one rather than allowing arithmetic to treat it as zero. Delayed or missing metrics can postpone shutdown.

Cold starts include metric publication and alarm evaluation, image pull and container/model startup, and available Spot capacity. After a queue has been inactive for more than six hours, [SQS metrics can take up to 15 minutes to resume](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/monitoring-using-cloudwatch.html). There is no wake-up Lambda. When [Spot capacity is unavailable](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/fargate-capacity-providers.html), ECS keeps retrying placement; messages wait in SQS subject to queue retention. There is no On-Demand fallback.

On SIGTERM the worker stops polling and tries to finish the active message. ECS retains `stopTimeout=120`; if the worker is forcibly stopped before finishing, the unacknowledged message becomes available after its current visibility timeout expires (initially 900 seconds, extended by the processing heartbeat). A message returned by an outstanding long poll after SIGTERM is released immediately with visibility zero without starting processing; if that call fails, it becomes visible when the existing timeout expires. Successful matching jobs are acknowledged only after their manifest is stored. A crash after manifest upload but before acknowledgement is handled by the same manifest check on retry.

Scaling workers to zero saves task compute charges, **not NAT charges**. The VPC, subnets, NAT gateway and routing remain provisioned and billable as before. See the [repository deployment and verification instructions](../README.md).

Supported formats are MP3, WAV, FLAC, M4A, OGG, AAC, and WMA.

## Local container

Set `MEDIA_BUCKET` and `QUEUE_URL` in `.env`, then run:

```bash
DOCKER_UID="$(id -u)" DOCKER_GID="$(id -g)" docker compose up --build
```

The compose file expects an AWS-compatible SQS endpoint or a real queue; it does not expose an HTTP upload API.

## Deployment

The Pulumi stack creates the media bucket, SQS queue/DLQ, S3 notification, IAM roles, ECR repository, VPC/NAT networking, ECS Fargate service, logs, and queue-depth scaling.

Infrastructure settings are stored as one typed JSON object in Pulumi configuration rather than as individual environment variables. Set `minTasks` and `maxTasks` to control the autoscaling floor and ceiling, `backlogPerWorker` for waiting jobs per ready worker (default 5), `scaleOutCooldownSeconds` for the shared scale-out cooldown (default 300), `idleMinutes` for the idle window, and `maxReceiveCount` for retry deliveries. Both new scaling settings must be positive integers; omitted fields receive defaults in existing stacks. The following initializes a new stack; for an existing stack use the preserving migration in the repository README:

```bash
# Select the intended existing stack. Shared values come from Pulumi.yaml.
pulumi stack select <stack>
pulumi preview
pulumi up
```

The first `pulumi up` can omit `image`; Pulumi uses an ECR `:bootstrap` reference to create the task definition. From the repository root, build and publish the worker separately from the infrastructure:

```bash
./scripts/publish-vocal-extractor-image.sh
pulumi up
```

The script builds only `./vocal-extractor`, pushes a Git-based tag to the ECR repository created by Pulumi, resolves the immutable image digest, and stores it in the Pulumi `image` setting. It does not modify or rebuild the rest of the application. AWS credentials can use the normal AWS profile/environment configuration. The publisher reads the region from the selected stack's `aws:region` setting, matching the provider and worker.

## Tests

```bash
python -m pip install -r requirements-dev.txt -r requirements.txt
pytest -q
```
