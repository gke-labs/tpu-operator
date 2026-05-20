SHELL := /bin/bash

.PHONY: all
all: generate manifests

.PHONY: manifests
manifests:
	mkdir -p deploy/crds
	# Generate CRDs
	go run sigs.k8s.io/controller-tools/cmd/controller-gen crd paths="./pkg/apis/..." output:crd:artifacts:config=deploy/crds
	# Generate Controller RBAC
	mkdir -p deploy/controller
	go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=tpu-node-group-manager-role paths="./pkg/controllers/tpunodegroup" output:rbac:artifacts:config=deploy/controller
	# Generate Device Plugin RBAC
	mkdir -p deploy/deviceplugin
	go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=tpu-device-plugin paths="./pkg/controllers/tpunodegroup/deviceplugin" output:rbac:artifacts:config=deploy/deviceplugin

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
