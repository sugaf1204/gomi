OUT ?= dist/ubuntu-minimal-cloud-amd64
VERSION ?= dev

.PHONY: test validate build clean

test:
	bash -n scripts/build scripts/render-manifest scripts/gomi-deploy-runner scripts/test-build-script
	scripts/test-build-script
	if command -v shellcheck >/dev/null 2>&1; then shellcheck scripts/build scripts/render-manifest scripts/gomi-deploy-runner scripts/test-build-script; fi

validate: test
	test -f mkosi.conf
	test -x scripts/build
	test -x scripts/render-manifest
	test -x scripts/gomi-deploy-runner
	test -x scripts/test-build-script

build: validate
	scripts/build "$(OUT)" "$(VERSION)"

clean:
	rm -rf dist .build
