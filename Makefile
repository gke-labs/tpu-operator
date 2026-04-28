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

.PHONY: codegen-client
codegen-client:
	@echo "Generating client, informers, and listers..."
	@CODEGEN_PKG=$$(go list -mod=mod -m -f '{{.Dir}}' k8s.io/code-generator); \
	source "$$CODEGEN_PKG/kube_codegen.sh"; \
	kube::codegen::gen_client \
		--with-watch \
		--with-applyconfig \
		--output-dir pkg/generated \
		--output-pkg gke-internal.googlesource.com/tpu-node-group/pkg/generated \
		--boilerplate "hack/boilerplate.go.txt" \
		pkg/apis
