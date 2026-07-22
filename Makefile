SHELL := /bin/bash

# GCP Project detection (priority: Env Var > gcloud config)
PROJECT ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE_NAME := us-docker.pkg.dev/$(PROJECT)/gcr.io/tpu-controller

# Use USER-dev as default tag for iterative development
GIT_COMMIT := $(shell git rev-parse --short HEAD)
GIT_DIRTY := $(shell test -n "`git status --porcelain`" && echo "-dirty" || true)
IMAGE_TAG ?= $(USER)-$(GIT_COMMIT)$(GIT_DIRTY)
export IMAGE_TAG
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

TEST_NAME ?= .
.PHONY: e2e-test
e2e-test: docker-build docker-push docker-clean e2e-kustomize
	@echo "Running E2E tests..."
	@if [ -f e2e/local-env.sh ]; then \
		source e2e/local-env.sh && \
		export CONTROL_PLANE_NODE PROJECT ZONE E2E_CONTROL_PLANE_IP E2E_RESERVATION E2E_REGION && \
		source e2e/connect-gce-cluster.sh && \
		go test -v -tags=e2e ./e2e/... -run "$(TEST_NAME)" -timeout 20m \
			-args \
			-e2e-project="$$PROJECT" \
			-e2e-zone="$$ZONE" \
			-e2e-region="$$E2E_REGION" \
			-e2e-reservation="$$E2E_RESERVATION" \
			-e2e-control-plane-ip="$$E2E_CONTROL_PLANE_IP"; \
	else \
		go test -v -tags=e2e ./e2e/... -run "$(TEST_NAME)"; \
	fi



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

.PHONY: docker-clean
docker-clean:
	@if [ -z "$(PROJECT)" ]; then \
		echo "ERROR: PROJECT variable must be set or available via gcloud config"; \
		exit 1; \
	fi
	@echo "Cleaning up old images for $(USER)..."
	@for tag in $$(gcloud artifacts docker tags list $(IMAGE_NAME) --filter="tag~^$(USER)-" --format="get(tag)" 2>/dev/null | grep -v "^$(IMAGE_TAG)$$"); do \
		echo "Deleting tag: $$tag"; \
		gcloud artifacts docker tags delete $(IMAGE_NAME):$$tag --quiet || true; \
	done
