.PHONY: all
all: generate manifests

.PHONY: manifests
manifests:
	mkdir -p deploy/crds
	go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=manager-role crd paths="./pkg/api/..." output:crd:artifacts:config=deploy/crds

.PHONY: generate
generate:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object paths="./pkg/api/..."
