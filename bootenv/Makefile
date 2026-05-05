GO ?= go
TINYGO ?= tinygo
GOCACHE ?= /tmp/go-build
GOMODCACHE ?= /tmp/go-mod-gomi-bootenv
GOARCH ?= amd64
SPEC ?= bootenvs/ubuntu-minimal-cloud-amd64.yaml
OUT ?= dist/ubuntu-minimal-cloud-amd64
RENDER_OUT ?= dist/rendered
RUNNER_OUT ?= dist/bin/gomi-deploy-runner
VERSION ?=

.PHONY: test validate plan render build build-runner build-runner-tinygo clean

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GO) test ./...

validate:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GO) run ./cmd/gomi-bootenv validate -f $(SPEC)

plan:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GO) run ./cmd/gomi-bootenv plan -f $(SPEC)

render:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GO) run ./cmd/gomi-bootenv render -f $(SPEC) -o $(RENDER_OUT)

build-runner:
	mkdir -p $(dir $(RUNNER_OUT))
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GO) build -trimpath -ldflags "-s -w" -o $(RUNNER_OUT) ./cmd/gomi-deploy-runner

build-runner-tinygo:
	mkdir -p $(dir $(RUNNER_OUT))
	GOOS=linux GOARCH=$(GOARCH) GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(TINYGO) build -tags tinygo -no-debug -opt z -o $(RUNNER_OUT) ./cmd/gomi-deploy-runner

build: build-runner
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) $(GO) run ./cmd/gomi-bootenv build -f $(SPEC) -o $(OUT) -runner $(RUNNER_OUT) -version "$(VERSION)"

clean:
	rm -rf dist
