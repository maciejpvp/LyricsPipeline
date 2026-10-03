#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

if ! command -v docker >/dev/null 2>&1; then
	printf '%s\n' "docker is required" >&2
	exit 1
fi
if ! command -v aws >/dev/null 2>&1; then
	printf '%s\n' "aws CLI is required" >&2
	exit 1
fi
if ! command -v pulumi >/dev/null 2>&1; then
	printf '%s\n' "pulumi CLI is required" >&2
	exit 1
fi

repository_url="$(pulumi stack output repositoryUrl)"
registry="${repository_url%%/*}"
repository_name="${repository_url##*/}"
region="${AWS_REGION:-$(aws configure get region)}"
tag="${GIT_SHA:-$(git rev-parse --short=12 HEAD)}"

if [[ -z "$region" ]]; then
	printf '%s\n' "AWS_REGION or an AWS CLI default region is required" >&2
	exit 1
fi

aws ecr get-login-password --region "$region" | docker login --username AWS --password-stdin "$registry"
docker build --platform linux/amd64 -t "$repository_url:$tag" ./vocal-extractor
docker push "$repository_url:$tag" >/dev/null

digest="$(aws ecr describe-images --region "$region" --repository-name "$repository_name" --image-ids imageTag="$tag" --query 'imageDetails[0].imageDigest' --output text)"
image_reference="$repository_url@$digest"
pulumi config set deploymentImage "$image_reference"

printf 'Published %s\n' "$image_reference"
printf '%s\n' "Run 'pulumi up' to deploy this image."
