#!/bin/bash
# e2e/run-test.sh
# Usage: ./e2e/run-test.sh [test_name_regexp]
# Example: ./e2e/run-test.sh ^TestTPUNodeGroup$

set -e

# Change to repo root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

# Load environment variables
if [[ -f "e2e/local-env.sh" ]]; then
  echo "=== Loading environment from e2e/local-env.sh ==="
  source e2e/local-env.sh
  export CONTROL_PLANE_NODE PROJECT ZONE
else
  echo "Error: e2e/local-env.sh not found."
  exit 1
fi

# Connect to cluster (exports KUBECONFIG and establishes SSH tunnel)
echo "=== Connecting to GCE cluster ==="
source e2e/connect-gce-cluster.sh

# Run test
# If no argument is provided, runs all tests in the package
TEST_NAME="${1:-.}"

echo "=== Running E2E test matching '${TEST_NAME}' with 20m timeout ==="
go test -v ./e2e -tags=e2e -run "${TEST_NAME}" -timeout 20m "${@:2}"
