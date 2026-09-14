#!/usr/bin/env bash
# Build the three service images for linux/amd64 and push them to ECR.
# Runs on the owner's arm64 laptop; nodes are amd64, so --platform is mandatory
# (the Go build stages run under emulation, hidden behind `terraform apply`).
# The registry comes from FOUNDATION, not the daily stack: builds run in
# parallel with `terraform apply` on daily, so daily outputs are mid-flux, while
# ECR is persistent foundation infrastructure.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
foundation="$root/terraform/foundation"
tag="${1:-latest}"
platform="${KA_IMAGE_PLATFORM:-linux/amd64}"

outputs_json() {
  if [ -n "${IMAGES_OUTPUTS_JSON:-}" ]; then
    cat "$IMAGES_OUTPUTS_JSON"
  else
    terraform -chdir="$foundation" output -json
  fi
}
outputs=$(outputs_json)
out() { jq -r "$1" <<<"$outputs"; }
region=$(out '.aws_region.value')
chat=$(out '.ecr_repository_urls.value["knowledge-assistant/chat-api"]')
ingest=$(out '.ecr_repository_urls.value["knowledge-assistant/ingestion"]')
ui=$(out '.ecr_repository_urls.value["knowledge-assistant/ui"]')
registry="${chat%%/*}"   # <acct>.dkr.ecr.<region>.amazonaws.com

aws ecr get-login-password --region "$region" \
  | docker login --username AWS --password-stdin "$registry"

# A buildx builder that can target linux/amd64 (via QEMU) must exist. Reuse one
# named ka-builder across runs; create it the first time.
docker buildx inspect ka-builder >/dev/null 2>&1 || docker buildx create --name ka-builder --use >/dev/null
docker buildx use ka-builder

build() { # <repo-uri> <dockerfile>
  docker buildx build --platform "$platform" --push \
    -t "$1:$tag" -f "$2" "$root"
}
build "$chat"   "$root/deploy/docker/chat-api.Dockerfile"
build "$ingest" "$root/deploy/docker/ingestion.Dockerfile"
build "$ui"     "$root/deploy/docker/ui.Dockerfile"

echo "pushed chat-api, ingestion, ui to $registry (platform $platform, tag $tag)"
