#!/usr/bin/env bash
# This script helps safely continue a Git merge or rebase operation.
# It verifies that there are no unresolved conflicts, that the project
# builds successfully, and that all unit tests pass before attempting
# to run 'git rebase --continue' or 'git merge --continue'.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
ROOT_DIR=$(dirname "${SCRIPT_DIR}")
cd "${ROOT_DIR}"

# 1. Check if there are still unresolved conflicts
UNRESOLVED=$(git diff --name-only --diff-filter=U)
if [ -n "$UNRESOLVED" ]; then
  echo "❌ Error: You still have unresolved conflicts in the following files:"
  echo "$UNRESOLVED"
  exit 1
fi

echo "🚀 Verifying build..."
go build ./...

echo "🧪 Running unit tests..."
go test ./...

echo "✅ All checks passed!"

# 2. Determine if we are in a rebase or merge and continue
if [ -d ".git/rebase-merge" ] || [ -d ".git/rebase-apply" ]; then
  echo "Continuing rebase..."
  git rebase --continue
elif [ -f ".git/MERGE_HEAD" ]; then
  echo "Continuing merge..."
  git merge --continue
else
  echo "❓ No active rebase or merge detected."
fi
