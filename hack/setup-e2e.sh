#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
ROOT_DIR=$(dirname "${SCRIPT_DIR}")
cd "${ROOT_DIR}"

echo "=== Verifying Application Default Credentials (ADC) ==="
if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
  echo "ERROR: Application Default Credentials not found or invalid."
  echo "Please run: gcloud auth application-default login"
  exit 1
fi
echo "ADC verification passed."

CLUSTER_NAME="tpu-controller-e2e"
echo "=== Setting up KinD Cluster: ${CLUSTER_NAME} ==="
if ! kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}\$"; then
  echo "Creating KinD cluster ${CLUSTER_NAME}..."
  kind create cluster --name "${CLUSTER_NAME}"
else
  echo "KinD cluster ${CLUSTER_NAME} already exists."
fi

echo "=== Installing/Updating CRDs ==="
make manifests
kubectl apply -f deploy/crds/

echo "=== Setup Complete ==="
