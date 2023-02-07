# SPDX-FileCopyrightText: 2022-present Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0

SHELL = bash -e -o pipefail

export CGO_ENABLED=1
export GO111MODULE=on

.PHONY: build

LINK_AGENT_VERSION ?= latest

build-tools:=$(shell if [ ! -d "./build/build-tools" ]; then mkdir -p build && cd build && git clone https://github.com/onosproject/build-tools.git; fi)
include ./build/build-tools/make/onf-common.mk

mod-update: # @HELP Download the dependencies to the vendor folder
	go mod tidy
	go mod vendor

mod-lint: mod-update # @HELP ensure that the required dependencies are in place
	# dependencies are vendored, but not committed, go.sum is the only thing we need to check
	bash -c "diff -u <(echo -n) <(git diff go.sum)"

local-deps: # @HELP imports local deps in the vendor folder
local-deps: local-helmit local-onos-api local-onos-lib-go local-onos-test

build: # @HELP build the Go binaries and run all validations (default)
build: mod-update local-deps
	go build -mod=vendor -o build/_output/discovery-agent ./cmd/discovery-agent

test: # @HELP run the unit tests and source code validation producing a golang style report
test: mod-lint build linters license
	go test -race github.com/onosproject/discovery-agent/...

jenkins-test: # @HELP run the unit tests and source code validation producing a junit style report for Jenkins
jenkins-test: jenkins-tools mod-lint build linters license
	TEST_PACKAGES=github.com/onosproject/discovery-agent/... ./build/build-tools/build/jenkins/make-unit

integration-tests: integration-test-namespace # @HELP run helmit integration tests locally
	make basic -C test

discovery-agent-docker: mod-update local-deps # @HELP build discovery-agent base Docker image
	docker build --platform linux/amd64 . -f build/discovery-agent/Dockerfile \
		-t ${DOCKER_REPOSITORY}discovery-agent:${LINK_AGENT_VERSION}

images: # @HELP build all Docker images
images: discovery-agent-docker

docker-push-latest: docker-login
	docker push onosproject/discovery-agent:latest

kind: # @HELP build Docker images and add them to the currently configured kind cluster
kind: images kind-only

kind-only: # @HELP deploy the image without rebuilding first
kind-only:
	@if [ "`kind get clusters`" = '' ]; then echo "no kind cluster found" && exit 1; fi
	kind load docker-image --name ${KIND_CLUSTER_NAME} ${DOCKER_REPOSITORY}discovery-agent:${LINK_AGENT_VERSION}

all: build images

publish: # @HELP publish version on github and dockerhub
	./build/build-tools/publish-version ${VERSION} onosproject/discovery-agent

jenkins-publish: images docker-push-latest # @HELP Jenkins calls this to publish artifacts
	./build/build-tools/release-merge-commit
	./build/build-tools/build/docs/push-docs

clean:: # @HELP remove all the build artifacts
	rm -rf ./build/_output ./vendor ./cmd/discovery-agent/discovery-agent ./cmd/onos/onos
	go clean -testcache github.com/onosproject/discovery-agent/...

local-helmit: # @HELP Copies a local version of the helmit dependency into the vendor directory
ifdef LOCAL_HELMIT
	rm -rf vendor/github.com/onosproject/helmit/go
	mkdir -p vendor/github.com/onosproject/helmit/go
	cp -r ${LOCAL_HELMIT}/go/* vendor/github.com/onosproject/helmit/go
endif

local-onos-api: # @HELP Copies a local version of the onos-api dependency into the vendor directory
ifdef LOCAL_ONOS_API
	rm -rf vendor/github.com/onosproject/onos-api/go
	mkdir -p vendor/github.com/onosproject/onos-api/go
	cp -r ${LOCAL_ONOS_API}/go/* vendor/github.com/onosproject/onos-api/go
endif

local-onos-lib-go: # @HELP Copies a local version of the onos-lib-go dependency into the vendor directory
ifdef LOCAL_ONOS_LIB_GO
	rm -rf vendor/github.com/onosproject/onos-lib-go
	mkdir -p vendor/github.com/onosproject/onos-lib-go
	cp -r ${LOCAL_ONOS_LIB_GO}/* vendor/github.com/onosproject/onos-lib-go
endif

local-onos-test: # @HELP Copies a local version of the onos-test dependency into the vendor directory
ifdef LOCAL_ONOS_TEST
	rm -rf vendor/github.com/onosproject/onos-test
	mkdir -p vendor/github.com/onosproject/onos-test
	cp -r ${LOCAL_ONOS_TEST}/* vendor/github.com/onosproject/onos-test
endif