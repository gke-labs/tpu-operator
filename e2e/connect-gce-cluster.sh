#!/bin/bash

# Configuration
LOCAL_PORT=${LOCAL_PORT:-6443}
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$(pwd)/e2e/remote-kubeconfig.yaml}"

# Load local overrides if present
LOCAL_ENV="$(dirname "$0")/local-env.sh"
if [ -f "${LOCAL_ENV}" ]; then
  source "${LOCAL_ENV}"
fi

# Ensure required variables are set
if [ -z "${CONTROL_PLANE_NODE}" ] || [ -z "${PROJECT}" ] || [ -z "${ZONE}" ]; then
  echo "Error: CONTROL_PLANE_NODE, PROJECT, and ZONE must be set."
  echo "You can set them in environment variables or create a local config file:"
  echo "  $(dirname "$0")/local-env.sh"
  echo "Example local-env.sh:"
  echo "  CONTROL_PLANE_NODE=\"your-control-plane-node\""
  echo "  PROJECT=\"your-gcp-project\""
  echo "  ZONE=\"your-gce-zone\""
  exit 1
fi

echo "=== Connecting to GCE K8s Cluster ==="

# 1. Fetch Kubeconfig from control plane
echo "Fetching /etc/kubernetes/admin.conf from ${CONTROL_PLANE_NODE}..."
gcloud compute ssh "${CONTROL_PLANE_NODE}" \
  --project="${PROJECT}" \
  --zone="${ZONE}" \
  --command="sudo cat /etc/kubernetes/admin.conf" > "${KUBECONFIG_PATH}"

if [ $? -ne 0 ]; then
  echo "Error: Failed to fetch kubeconfig."
  exit 1
fi

# 2. Localize Kubeconfig
echo "Localizing kubeconfig to use localhost:${LOCAL_PORT} and skip TLS verify..."
# Use sed to replace the server and add insecure-skip-tls-verify
# We also remove the certificate-authority-data to avoid conflicts with localhost cert
sed -i "s|server: https://.*:6443|server: https://127.0.0.1:${LOCAL_PORT}|" "${KUBECONFIG_PATH}"
sed -i "/certificate-authority-data:/d" "${KUBECONFIG_PATH}"
# Insert insecure-skip-tls-verify: true into the cluster section
sed -i "/cluster:/a \    insecure-skip-tls-verify: true" "${KUBECONFIG_PATH}"

# 3. Establish SSH Tunnel
echo "Establishing SSH tunnel to ${CONTROL_PLANE_NODE}:${LOCAL_PORT}..."
# Kill existing tunnel if any
pkill -f "ssh.*-L ${LOCAL_PORT}:localhost:${LOCAL_PORT}"

gcloud compute ssh "${CONTROL_PLANE_NODE}" \
  --project="${PROJECT}" \
  --zone="${ZONE}" \
  -- -L "${LOCAL_PORT}:localhost:6443" -N -f

if [ $? -ne 0 ]; then
  echo "Error: Failed to establish SSH tunnel."
  exit 1
fi

export KUBECONFIG="${KUBECONFIG_PATH}"
export E2E_PROJECT="${PROJECT}"
export E2E_ZONE="${ZONE}"
export E2E_REGION="us-central1"

echo "=== Connection Successful ==="
echo "Kubeconfig saved to: ${KUBECONFIG}"
echo "Environment variables exported:"
echo "  KUBECONFIG=${KUBECONFIG}"
echo "  E2E_PROJECT=${E2E_PROJECT}"
echo "  E2E_ZONE=${E2E_ZONE}"
echo "  E2E_REGION=${E2E_REGION}"
