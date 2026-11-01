#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stack="${1:-dev}"
cd "$project_root/infra/separation"

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

repository_url="$(pulumi stack output repositoryUrl --stack "$stack")"
registry="${repository_url%%/*}"
repository_name="${repository_url##*/}"
region="$(pulumi config get aws:region --stack "$stack")"
tag="${GIT_SHA:-$(git rev-parse --short=12 HEAD)}"

if [[ -z "$region" ]]; then
	printf '%s\n' "aws:region must be configured for the selected Pulumi stack" >&2
	exit 1
fi

aws ecr get-login-password --region "$region" | docker login --username AWS --password-stdin "$registry"
docker build --platform linux/amd64 -t "$repository_url:$tag" "$project_root/vocal-extractor"
docker push "$repository_url:$tag" >/dev/null

digest="$(aws ecr describe-images --region "$region" --repository-name "$repository_name" --image-ids imageTag="$tag" --query 'imageDetails[0].imageDigest' --output text)"
image_reference="$repository_url@$digest"
pulumi config set image "$image_reference" --stack "$stack"

printf 'Published %s\n' "$image_reference"
printf '%s\n' "Run pulumi -C infra/separation up --stack $stack to deploy this image."
