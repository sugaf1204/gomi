CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.21.0
ENVTEST = go run sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.2-0.20260925035230-4d452bc0517c
IMG ?= capgomi:dev
PLATFORM ?= linux/amd64

.PHONY: generate test build manifests integration docker-build

generate:
	$(CONTROLLER_GEN) object paths=./api/... crd output:crd:artifacts:config=config/crd

test:
	go test -race ./...
	go vet ./...

build:
	go build -trimpath -o bin/manager ./cmd/manager

manifests:
	mkdir -p dist
	kubectl kustomize config > dist/components.yaml
	sed 's|image: controller:dev|image: $(IMG)|' dist/components.yaml > dist/infrastructure-components.yaml
	rm dist/components.yaml
	cp metadata.yaml dist/metadata.yaml
	cp samples/cluster-template.yaml dist/cluster-template.yaml

integration:
	go mod download sigs.k8s.io/cluster-api@v1.14.2
	CAPI_CRD_DIR="$$(go env GOMODCACHE)/sigs.k8s.io/cluster-api@v1.14.2/core/config/crd/bases" KUBEBUILDER_ASSETS="$$($(ENVTEST) use 1.36.2 -p path)" go -C test/integration test -race -count=1 -v ./...

docker-build:
	docker build --platform=$(PLATFORM) -t $(IMG) .
