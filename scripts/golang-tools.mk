## Targets to install golang tools (golangci, goimports, etc...)


# find or download controller-gen
controller-gen: FORCE
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.3.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# find or download kustomize
kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$KUSTOMIZE_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# find or download golangci
install-golangci:
ifeq (, $(shell which golangci-lint))
	@{ \
	set -e ;\
	echo gocache: $$GOCACHE ; echo gobin $$GOBIN \; 
	GOLANGCI_TMP_DIR=$$(mktemp -d) ;\
	cd $$GOLANGCI_TMP_DIR ;\
	go mod init tmp ;\
	go get github.com/golangci/golangci-lint/cmd/golangci-lint@v1.31.0 ;\
	go get -u  mvdan.cc/gofumpt  && go install $_ ;\
	go get github.com/daixiang0/gci && go install $_ ;\
	rm -rf $$GOLANGCI_TMP_DIR ;\
	}
GOLANGCI=$(GOBIN)/golangci-lint
else
GOLANGCI=$(shell which golangci-lint)
endif

# find or download golangci
install-goimports:
ifeq (, $(shell which goimports))
	@{ \
	set -e ;\
	GOIMPORTS_TMP_DIR=$$(mktemp -d) ;\
	cd $$GOIMPORTS_TMP_DIR ;\
	go mod init tmp ;\
	go get golang.org/x/tools/cmd/goimports ;\
	rm -rf $$GOIMPORTS_TMP_DIR ;\
	}
GOIMPORTS=$(GOBIN)/goimports
else
GOIMPORTS=$(shell which goimports)
endif

FORCE:
	@echo ""
# Set globan env vars, especially needed for go cache
.EXPORT_ALL_VARIABLES:
GOLANGCI_LINT_CACHE := $(shell pwd)/build/_output/golangci-lint-cache
XDG_CACHE_HOME := $(shell pwd)/build/_output/xdgcache
GOCACHE := $(shell pwd)/build/_output/gocache
CGO_ENABLED := 0

