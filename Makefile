SHELL := /bin/bash

# GCP Project detection (priority: Env Var > gcloud config)
PROJECT ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE_NAME := us-docker.pkg.dev/$(PROJECT)/gcr.io/tpu-controller

# Use USER-dev as default tag for iterative development
IMAGE_TAG ?= $(USER)-dev
FULL_IMAGE := $(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: all
all: generate manifests

.PHONY: manifests
manifests:
	mkdir -p deploy/crds
	# Generate CRDs
	go run sigs.k8s.io/controller-tools/cmd/controller-gen crd paths="./internal/apis/..." output:crd:artifacts:config=deploy/crds
	# Generate Controller RBAC
	mkdir -p deploy/controller
	go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=tpu-node-group-manager-role paths="./internal/controllers/tpunodegroup" output:rbac:artifacts:config=deploy/controller
	# Generate Device Plugin RBAC
	mkdir -p deploy/deviceplugin
	go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=tpu-device-plugin paths="./internal/controllers/tpunodegroup/deviceplugin" output:rbac:artifacts:config=deploy/deviceplugin

.PHONY: generate
generate:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object paths="./internal/apis/..."

.PHONY: test
test:
	@echo "Running unit tests..."
	go test -v ./internal/...

.PHONY: e2e-test
e2e-test:
	@echo "Running E2E tests..."
	go test -v -tags=e2e ./e2e/...

.PHONY: e2e-kustomize
e2e-kustomize:
	@if [ -z "$(PROJECT)" ]; then \
		echo "ERROR: PROJECT variable must be set or available via gcloud config"; \
		exit 1; \
	fi
	mkdir -p e2e/deploy
	sed -e 's|IMAGE_PLACEHOLDER|$(IMAGE_NAME)|g' \
	    -e 's|TAG_PLACEHOLDER|$(IMAGE_TAG)|g' \
	    e2e/deploy/kustomization.tmpl.yaml > e2e/deploy/kustomization.yaml


.PHONY: debug-docker
debug-docker:
	@echo "PROJECT:    $(PROJECT)"
	@echo "IMAGE_TAG:  $(IMAGE_TAG)"
	@echo "FULL_IMAGE: $(FULL_IMAGE)"

.PHONY: docker-build
docker-build:
	@if [ -z "$(PROJECT)" ]; then \
		echo "ERROR: PROJECT variable must be set or available via gcloud config"; \
		exit 1; \
	fi
	@echo "Building image: $(FULL_IMAGE)"
	docker build -t $(FULL_IMAGE) .

.PHONY: docker-push
docker-push:
	@if [ -z "$(PROJECT)" ]; then \
		echo "ERROR: PROJECT variable must be set or available via gcloud config"; \
		exit 1; \
	fi
	@echo "Pushing image: $(FULL_IMAGE)"
	docker push $(FULL_IMAGE)
