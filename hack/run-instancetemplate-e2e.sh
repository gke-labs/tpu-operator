#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
ROOT_DIR=$(dirname "${SCRIPT_DIR}")
cd "${ROOT_DIR}"

CONTROLLER_LOG="/tmp/controller_e2e.log"
CONTROLLER_PID=""

cleanup() {
  local exit_code=$?
  echo "=== Cleaning up ==="
  if [[ -n "${CONTROLLER_PID:-}" ]]; then
    echo "Terminating controller process (PID: ${CONTROLLER_PID})..."
    kill "${CONTROLLER_PID}" 2>/dev/null || true
  fi
  if [[ ${exit_code} -ne 0 ]]; then
    echo "=== TEST FAILED: Controller Logs ==="
    cat "${CONTROLLER_LOG}" || true
  fi
  exit "${exit_code}"
}
trap cleanup EXIT

echo "=== Refreshing CRDs ==="
make manifests
kubectl apply -f deploy/crds/

echo "=== Cleaning Environment ==="
kubectl delete instancetemplates --all --ignore-not-found

echo "=== Starting Controller ==="
go run cmd/main.go --kube-config "${HOME}/.kube/config" > "${CONTROLLER_LOG}" 2>&1 &
CONTROLLER_PID=$!

# Give controller a moment to start
sleep 3

MANIFEST="pkg/controllers/instancetemplate/testdata/test_template.yaml"
CR_NAME=$(grep 'name:' "${MANIFEST}" | awk '{print $2}')
PROJECT=$(grep 'project:' "${MANIFEST}" | awk '{print $2}')

echo "=== Applying Test Manifest ==="
kubectl apply -f "${MANIFEST}"

TIMEOUT=120
INTERVAL=5
ELAPSED=0

echo "=== Waiting for InstanceTemplate to become Ready ==="
while true; do
  READY=$(kubectl get instancetemplate "${CR_NAME}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
  URI=$(kubectl get instancetemplate "${CR_NAME}" -o jsonpath='{.status.uri}' 2>/dev/null || true)

  if [[ "${READY}" == "True" && -n "${URI}" ]]; then
    echo "InstanceTemplate is Ready. URI: ${URI}"
    break
  fi

  if [[ ${ELAPSED} -ge ${TIMEOUT} ]]; then
    echo "ERROR: Timeout waiting for InstanceTemplate to become Ready."
    kubectl get instancetemplate "${CR_NAME}" -o yaml
    exit 1
  fi

  sleep ${INTERVAL}
  ELAPSED=$((ELAPSED + INTERVAL))
done

echo "=== Verifying GCP Resource Creation ==="
echo "Checking GCP Instance Template: ${CR_NAME} in project ${PROJECT}..."
if ! gcloud compute instance-templates describe "${CR_NAME}" --project "${PROJECT}" >/dev/null 2>&1; then
  echo "ERROR: GCP Instance Template not found in project ${PROJECT}!"
  exit 1
fi
echo "GCP resource verified."

echo "=== Teardown Verification ==="
echo "Deleting InstanceTemplate CR..."
kubectl delete instancetemplate "${CR_NAME}" --timeout=120s

echo "Verifying GCP resource deletion..."
if gcloud compute instance-templates describe "${CR_NAME}" --project "${PROJECT}" >/dev/null 2>&1; then
  echo "ERROR: GCP Instance Template still exists after CR deletion!"
  exit 1
else
  echo "GCP Instance Template deleted successfully."
fi

echo "=== E2E Test Passed ==="
