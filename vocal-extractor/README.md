# Vocal Extractor

CPU-only S3/SQS worker that separates vocals from audio with UVR-MDX-NET through `audio-separator`.

## Job contract

Upload source audio to `s3://$MEDIA_BUCKET/input/{job_id}/{filename}`. S3 sends the object-created notification to SQS, and Fargate workers process one message at a time. Completed WAV stems and `manifest.json` are written to `output/{job_id}/`. Source objects are retained.

The worker acknowledges a message only after the manifest is uploaded. Failed messages remain available for retry and eventually move to the configured dead-letter queue. A completed manifest makes redelivery safe.

Supported formats are MP3, WAV, FLAC, M4A, OGG, AAC, and WMA.

## Local container

Set `MEDIA_BUCKET` and `QUEUE_URL` in `.env`, then run:

```bash
DOCKER_UID="$(id -u)" DOCKER_GID="$(id -g)" docker compose up --build
```

The compose file expects an AWS-compatible SQS endpoint or a real queue; it does not expose an HTTP upload API.

## Deployment

The Pulumi stack creates the media bucket, SQS queue/DLQ, S3 notification, IAM roles, ECR repository, VPC/NAT networking, ECS Fargate service, logs, and queue-depth scaling.

Infrastructure settings are stored as one typed JSON object in Pulumi configuration rather than as individual environment variables:

```bash
pulumi config set deployment "$(tr -d '\n' < deployment.example.json)"
pulumi up
```

The first `pulumi up` can omit `image`; because the service starts with zero tasks, Pulumi uses an ECR `:bootstrap` reference only to create the task definition. From the repository root, build and publish the worker separately from the infrastructure:

```bash
./scripts/publish-vocal-extractor-image.sh
pulumi up
```

The script builds only `./vocal-extractor`, pushes a Git-based tag to the ECR repository created by Pulumi, resolves the immutable image digest, and stores it in the Pulumi `deploymentImage` setting. It does not modify or rebuild the rest of the application. AWS credentials and the AWS provider's region can still be supplied through the normal AWS profile/environment configuration.

## Tests

```bash
python -m pip install -r requirements-dev.txt -r requirements.txt
pytest -q
```
