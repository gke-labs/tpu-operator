#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <target>"
  echo "Targets: instancetemplate, tpunodegroup"
  exit 1
fi

TARGET=$1

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
ROOT_DIR=$(dirname "${SCRIPT_DIR}")
cd "${ROOT_DIR}"

CONTROLLER_LOG="/tmp/controller_e2e_${TARGET}.log"
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
kubectl apply -f deploy/crds/ --request-timeout=30s

echo "=== Starting Controller ==="
go run cmd/main.go --kube-config "${HOME}/.kube/config" > "${CONTROLLER_LOG}" 2>&1 &
CONTROLLER_PID=$!

# Give controller a moment to start
sleep 3

echo "=== Cleaning Environment ==="

check_stuck_resources() {
  local resource_type=$1
  local stuck
  stuck=$(kubectl get "${resource_type}" -o jsonpath='{.items[?(@.metadata.deletionTimestamp)].metadata.name}' 2>/dev/null || true)
  if [[ -n "${stuck}" ]]; then
    echo "ERROR: Found ${resource_type} stuck in deletion: ${stuck}"
    echo "This usually means they have finalizers and no controller is running to handle them."
    echo "Please clean them up manually (e.g., by removing finalizers) before running tests."
    return 1
  fi
  return 0
}

case "${TARGET}" in
  instancetemplate)
    check_stuck_resources instancetemplates || exit 1
    kubectl delete instancetemplates --all --ignore-not-found --timeout=60s --request-timeout=30s
    ;;
  tpunodegroup)
    check_stuck_resources tpunodegroups || exit 1
    check_stuck_resources instancetemplates || exit 1
    kubectl delete tpunodegroups --all --ignore-not-found --timeout=60s --request-timeout=30s
    kubectl delete instancetemplates --all --ignore-not-found --timeout=60s --request-timeout=30s
    ;;
  *)
    echo "Unknown target: ${TARGET}"
    exit 1
    ;;
esac


case "${TARGET}" in
  instancetemplate)
    MANIFEST="pkg/controllers/instancetemplate/testdata/test_template.yaml"
    CR_NAME=$(grep 'name:' "${MANIFEST}" | awk '{print $2}')
    PROJECT=$(grep 'project:' "${MANIFEST}" | awk '{print $2}')

    echo "=== Applying Test Manifest ==="
    kubectl apply -f "${MANIFEST}" --request-timeout=30s

    TIMEOUT=120
    INTERVAL=5
    ELAPSED=0

    echo "=== Waiting for InstanceTemplate to become Ready ==="
    while true; do
      READY=$(kubectl get instancetemplate "${CR_NAME}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' --request-timeout=30s 2>/dev/null || true)
      URI=$(kubectl get instancetemplate "${CR_NAME}" -o jsonpath='{.status.uri}' --request-timeout=30s 2>/dev/null || true)

      if [[ "${READY}" == "True" && -n "${URI}" ]]; then
        echo "InstanceTemplate is Ready. URI: ${URI}"
        break
      fi

      if [[ ${ELAPSED} -ge ${TIMEOUT} ]]; then
        echo "ERROR: Timeout waiting for InstanceTemplate to become Ready."
        kubectl get instancetemplate "${CR_NAME}" -o yaml --request-timeout=30s
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
    kubectl delete instancetemplate "${CR_NAME}" --timeout=120s --request-timeout=30s

    echo "Verifying GCP resource deletion..."
    if gcloud compute instance-templates describe "${CR_NAME}" --project "${PROJECT}" >/dev/null 2>&1; then
      echo "ERROR: GCP Instance Template still exists after CR deletion!"
      exit 1
    else
      echo "GCP Instance Template deleted successfully."
    fi
    ;;

  tpunodegroup)
    MANIFEST="pkg/controllers/tpunodegroup/testdata/test_nodegroup.yaml"
    CR_NAME=$(grep 'name:' "${MANIFEST}" | awk '{print $2}')

    echo "=== Applying Test Manifest ==="
    kubectl apply -f "${MANIFEST}" --request-timeout=30s

    TIMEOUT=120
    INTERVAL=5
    ELAPSED=0

    echo "=== Waiting for child InstanceTemplate to be created ==="
    while true; do
      if kubectl get instancetemplate "${CR_NAME}-template" --request-timeout=30s >/dev/null 2>&1; then
        echo "Child InstanceTemplate ${CR_NAME}-template created."
        break
      fi

      if [[ ${ELAPSED} -ge ${TIMEOUT} ]]; then
        echo "ERROR: Timeout waiting for child InstanceTemplate to be created."
        kubectl get tpunodegroup "${CR_NAME}" -o yaml --request-timeout=30s
        exit 1
      fi

      sleep ${INTERVAL}
      ELAPSED=$((ELAPSED + INTERVAL))
    done

    echo "=== Verifying TPUNodeGroup Status ==="
    CONDITION_STATUS=$(kubectl get tpunodegroup "${CR_NAME}" -o jsonpath='{.status.conditions[?(@.type=="InstanceTemplateReady")].status}' --request-timeout=30s 2>/dev/null || true)
    CONDITION_REASON=$(kubectl get tpunodegroup "${CR_NAME}" -o jsonpath='{.status.conditions[?(@.type=="InstanceTemplateReady")].reason}' --request-timeout=30s 2>/dev/null || true)

    echo "TPUNodeGroup InstanceTemplateReady status: ${CONDITION_STATUS}, reason: ${CONDITION_REASON}"

    if [[ "${CONDITION_STATUS}" != "True" && "${CONDITION_STATUS}" != "False" ]]; then
      echo "ERROR: InstanceTemplateReady condition not found or invalid."
      kubectl get tpunodegroup "${CR_NAME}" -o yaml --request-timeout=30s
      exit 1
    fi

    echo "=== Teardown Verification ==="
    echo "Deleting TPUNodeGroup CR..."
    kubectl delete tpunodegroup "${CR_NAME}" --timeout=120s --request-timeout=30s

    echo "Verifying child InstanceTemplate deletion..."
    TIMEOUT=60
    INTERVAL=5
    ELAPSED=0
    while true; do
      if ! kubectl get instancetemplate "${CR_NAME}-template" --request-timeout=30s >/dev/null 2>&1; then
        echo "Child InstanceTemplate deleted successfully."
        break
      fi

      if [[ ${ELAPSED} -ge ${TIMEOUT} ]]; then
        echo "ERROR: Timeout waiting for child InstanceTemplate to be deleted."
        kubectl get instancetemplate "${CR_NAME}-template" -o yaml --request-timeout=30s
        exit 1
      fi

      sleep ${INTERVAL}
      ELAPSED=$((ELAPSED + INTERVAL))
    done
    ;;
esac

echo "=== E2E Test Passed ==="
