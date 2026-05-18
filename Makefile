SHELL := /bin/bash

.PHONY: all
all: generate manifests

.PHONY: manifests
manifests:
	mkdir -p deploy/crds
	go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=manager-role crd paths="./pkg/apis/..." output:crd:artifacts:config=deploy/crds

.PHONY: generate
generate:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object paths="./pkg/apis/..."

.PHONY: test
test:
	@echo "Running unit tests..."
	go test -v ./pkg/...

.PHONY: e2e-test
e2e-test:
	@echo "Running E2E tests..."
	go test -v -tags=e2e ./hack/e2e/...
