#!/usr/bin/env bash
# deploy.sh brings the app up on (or takes it down from) the daily EKS cluster.
# Every environment value comes from the daily stack's `terraform output`, so
# nothing here repeats what tfvars already set. Usage:
#   deploy.sh up   [image-tag]   # helm-install the ALB controller, apply workloads, reseed, print URL
#   deploy.sh down               # release the ALB and delete workloads (run before terraform destroy)
#   deploy.sh render [image-tag] # write the kustomize overlay only (offline; used by tests)
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
gen="$here/.generated"
daily="$here/../../terraform/daily"
# Pin to a chart version tested against the cluster's Kubernetes version; check
# with `helm search repo eks/aws-load-balancer-controller`.
LBC_CHART_VERSION="${LBC_CHART_VERSION:-1.8.1}"

outputs_json() {
  if [ -n "${DEPLOY_OUTPUTS_JSON:-}" ]; then
    cat "$DEPLOY_OUTPUTS_JSON"
  else
    terraform -chdir="$daily" output -json
  fi
}

render() {
  local tag="${1:-latest}"
  local outputs
  outputs=$(outputs_json)
  out() { jq -r "$1" <<<"$outputs"; }
  local chat ui ingest os bucket
  chat=$(out '.ecr_repository_urls.value["knowledge-assistant/chat-api"]')
  ui=$(out '.ecr_repository_urls.value["knowledge-assistant/ui"]')
  ingest=$(out '.ecr_repository_urls.value["knowledge-assistant/ingestion"]')
  os=$(out '.opensearch_endpoint.value')
  bucket=$(out '.docs_bucket.value')
  mkdir -p "$gen"
  cat > "$gen/kustomization.yaml" <<YAML
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
# Stamp the namespace here so it applies to the generated ka-config ConfigMap
# too; the base manifests carry it in their own metadata, but a generator in
# this overlay would otherwise land in the default namespace.
namespace: knowledge-assistant
resources:
  - ../base
images:
  - name: ka/chat-api
    newName: $chat
    newTag: $tag
  - name: ka/ui
    newName: $ui
    newTag: $tag
  - name: ka/ingestion
    newName: $ingest
    newTag: $tag
configMapGenerator:
  - name: ka-config
    literals:
      - opensearch_url=https://$os
      - docs_bucket=$bucket
    options:
      disableNameSuffixHash: true
YAML
  echo "rendered overlay to $gen (tag $tag)"
}

up() {
  local tag="${1:-latest}"
  local outputs
  outputs=$(outputs_json)
  out() { jq -r "$1" <<<"$outputs"; }
  local cluster region vpc
  cluster=$(out '.cluster_name.value')
  region=$(out '.region.value')
  vpc=$(out '.vpc_id.value')

  aws eks update-kubeconfig --name "$cluster" --region "$region"

  # ALB controller into the kube-system SA 4a pre-bound via Pod Identity (no
  # IRSA annotation needed). Helm here, not Terraform, keeps the daily stack
  # AWS-provider-only and offline-testable.
  helm repo add eks https://aws.github.io/eks-charts >/dev/null 2>&1 || true
  helm repo update eks >/dev/null
  helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller \
    --version "$LBC_CHART_VERSION" \
    --namespace kube-system \
    --set clusterName="$cluster" \
    --set region="$region" \
    --set vpcId="$vpc" \
    --set serviceAccount.create=true \
    --set serviceAccount.name=aws-load-balancer-controller
  kubectl -n kube-system rollout status deploy/aws-load-balancer-controller --timeout=180s

  render "$tag"
  # A Job is immutable once created; on a re-run delete the previous one first.
  kubectl -n knowledge-assistant delete job reseed --ignore-not-found
  kubectl apply -k "$gen"
  kubectl -n knowledge-assistant rollout status deploy/chat-api --timeout=180s
  kubectl -n knowledge-assistant rollout status deploy/ui --timeout=180s
  kubectl -n knowledge-assistant wait --for=condition=complete job/reseed --timeout=600s

  local host=""
  local i
  for i in $(seq 1 60); do
    host=$(kubectl -n knowledge-assistant get ingress ka \
      -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
    [ -n "$host" ] && break
    sleep 5
  done
  if [ -n "$host" ]; then
    echo "app: http://$host/"
  else
    echo "ingress has no ALB hostname yet; check: kubectl -n knowledge-assistant get ingress ka"
  fi
}

down() {
  # Delete the Ingress FIRST and wait: the controller runs its finalizer, which
  # deletes the real ALB and its ENIs. Skipping this makes 4a's later
  # `terraform destroy` hang on subnet/ENI dependencies. This is the primitive
  # 4c's evening-down sequences before destroying the daily stack.
  kubectl -n knowledge-assistant delete ingress ka --ignore-not-found --wait=true
  kubectl delete namespace knowledge-assistant --ignore-not-found
  helm -n kube-system uninstall aws-load-balancer-controller >/dev/null 2>&1 || true
  echo "Ingress/ALB released and workloads deleted. Safe to 'terraform destroy' the daily stack."
}

cmd="${1:-up}"
shift || true
case "$cmd" in
  render) render "${1:-latest}" ;;
  up) up "${1:-latest}" ;;
  down) down ;;
  *) echo "usage: deploy.sh [up|down|render] [image-tag]" >&2; exit 2 ;;
esac
