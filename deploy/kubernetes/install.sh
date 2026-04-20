#!/usr/bin/env bash
#
# Install WSO2 API Discovery Controller (ADC) on Kubernetes.
#
# Kustomize's default security model blocks cross-directory file references,
# so `kubectl apply -k deploy/kubernetes/` cannot directly read the canonical
# config at config/config.toml. This script runs kustomize with the load
# restriction disabled and pipes the rendered manifests to kubectl apply —
# the only safe way to keep ONE config.toml for all deployment methods.
#
# Usage:
#   ./deploy/kubernetes/install.sh          # apply (default)
#   ./deploy/kubernetes/install.sh render   # print manifests, don't apply
#   ./deploy/kubernetes/install.sh delete   # tear everything down

set -euo pipefail

# Resolve the directory this script lives in (handles symlinks, spaces).
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

action="${1:-apply}"

render() {
  if ! command -v kubectl >/dev/null 2>&1; then
    echo "error: kubectl not found in PATH" >&2
    exit 1
  fi
  # kubectl's embedded kustomize supports --load-restrictor via `kubectl kustomize`
  # (but not `kubectl apply -k`). Render to stdout, then pipe to apply.
  kubectl kustomize --load-restrictor=LoadRestrictionsNone "${SCRIPT_DIR}"
}

case "${action}" in
  apply)
    render | kubectl apply -f -
    ;;
  render)
    render
    ;;
  delete)
    render | kubectl delete -f - --ignore-not-found=true
    ;;
  *)
    echo "usage: $0 [apply|render|delete]" >&2
    exit 2
    ;;
esac
