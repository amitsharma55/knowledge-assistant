#!/usr/bin/env bash
# smoke.sh checks the deployed app after `deploy.sh up`: the ALB controller,
# chat-api and ui are Ready, the reseed Job completed, the Ingress has an ALB
# hostname, /healthz answers 200, and the unauthenticated /v1/teams answers 200.
# Run with the credentials/kubeconfig deploy.sh set up.
set -euo pipefail
ns=knowledge-assistant
fail=0
pass() { echo "PASS  $1"; }
failc() { echo "FAIL  $1"; fail=1; }

ready() { kubectl -n "$1" get deploy "$2" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true; }
[ "$(ready kube-system aws-load-balancer-controller)" = "1" ] && pass "ALB controller Ready" || failc "ALB controller not Ready"
[ "$(ready "$ns" chat-api)" -ge 1 ] 2>/dev/null && pass "chat-api Ready" || failc "chat-api not Ready"
[ "$(ready "$ns" ui)" -ge 1 ] 2>/dev/null && pass "ui Ready" || failc "ui not Ready"

jobc=$(kubectl -n "$ns" get job reseed -o jsonpath='{.status.succeeded}' 2>/dev/null || true)
[ "$jobc" = "1" ] && pass "reseed Job complete" || failc "reseed Job not complete (succeeded=$jobc)"

host=$(kubectl -n "$ns" get ingress ka -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
[ -n "$host" ] && pass "Ingress ALB hostname: $host" || failc "Ingress has no ALB hostname"

if [ -n "$host" ]; then
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://$host/healthz" || true)
  [ "$code" = "200" ] && pass "/healthz 200" || failc "/healthz returned $code"
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Dev-Groups: everyone' "http://$host/v1/teams" || true)
  [ "$code" = "200" ] && pass "/v1/teams 200" || failc "/v1/teams returned $code"
fi

[ "$fail" -eq 0 ] || { echo "$fail smoke check(s) failed"; exit 1; }
echo "all smoke checks passed"
