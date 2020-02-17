export GO15VENDOREXPERIMENT := 1


all:
	hack/dockerized "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} IMAGE_PULL_POLICY=${IMAGE_PULL_POLICY} VERBOSITY=${VERBOSITY} ./hack/build-manifests.sh && \
	    hack/bazel-fmt.sh && hack/bazel-build.sh"

go-all:
	hack/dockerized "KUBEVIRT_VERSION=${KUBEVIRT_VERSION} ./hack/build-go.sh install ${WHAT} && ./hack/build-copy-artifacts.sh ${WHAT} && DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} IMAGE_PULL_POLICY=${IMAGE_PULL_POLICY} VERBOSITY=${VERBOSITY} ./hack/build-manifests.sh"

bazel-generate:
	SYNC_VENDOR=true hack/dockerized "./hack/bazel-generate.sh"

bazel-build:
	hack/dockerized "hack/bazel-fmt.sh && hack/bazel-build.sh"

bazel-build-images:
	hack/dockerized "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} DOCKER_TAG_ALT=${DOCKER_TAG_ALT} IMAGE_PREFIX=${IMAGE_PREFIX} IMAGE_PREFIX_ALT=${IMAGE_PREFIX_ALT} ./hack/bazel-build-images.sh"

bazel-push-images:
	hack/dockerized "hack/bazel-fmt.sh && DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} DOCKER_TAG_ALT=${DOCKER_TAG_ALT} IMAGE_PREFIX=${IMAGE_PREFIX} IMAGE_PREFIX_ALT=${IMAGE_PREFIX_ALT} KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER} ./hack/bazel-push-images.sh"

push: bazel-push-images

bazel-test:
	hack/dockerized "hack/bazel-fmt.sh && hack/bazel-test.sh"

generate:
	hack/dockerized "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} IMAGE_PULL_POLICY=${IMAGE_PULL_POLICY} VERBOSITY=${VERBOSITY} ./hack/generate.sh"
	SYNC_VENDOR=true hack/dockerized "./hack/bazel-generate.sh && hack/bazel-fmt.sh"

apidocs:
	hack/dockerized "./hack/generate.sh && ./hack/gen-swagger-doc/gen-swagger-docs.sh v1 html"

client-python: generate
	hack/dockerized "TRAVIS_TAG=${TRAVIS_TAG} ./hack/gen-client-python/generate.sh"

go-build:
	hack/dockerized "KUBEVIRT_VERSION=${KUBEVIRT_VERSION} ./hack/build-go.sh install ${WHAT}" && ./hack/build-copy-artifacts.sh ${WHAT}

coverage:
	hack/dockerized "./hack/coverage.sh ${WHAT}"

goveralls: go-build
	SYNC_OUT=false hack/dockerized "TRAVIS_JOB_ID=${TRAVIS_JOB_ID} TRAVIS_PULL_REQUEST=${TRAVIS_PULL_REQUEST} TRAVIS_BRANCH=${TRAVIS_BRANCH} ./hack/goveralls.sh"

go-test: go-build
	SYNC_OUT=false hack/dockerized "./hack/build-go.sh test ${WHAT}"

test: bazel-test

functest:
	hack/dockerized "hack/build-func-tests.sh"
	hack/functests.sh

dump: bazel-build
	hack/dump.sh

functest-image-build:
	hack/func-tests-image.sh build

functest-image-push: functest-image-build
	hack/func-tests-image.sh push

clean:
	hack/dockerized "./hack/build-go.sh clean ${WHAT} && rm _out/* -rf"
	hack/dockerized "bazel clean --expunge"
	rm -f tools/openapispec/openapispec tools/resource-generator/resource-generator tools/manifest-templator/manifest-templator tools/vms-generator/vms-generator tools/marketplace/marketplace

distclean: clean
	hack/dockerized "rm -rf vendor/ && rm -f go.sum && GO111MODULE=on go clean -modcache"
	rm -rf vendor/

deps-update-patch:
	SYNC_VENDOR=true hack/dockerized " ./hack/dep-update.sh -u=patch && ./hack/dep-prune.sh && ./hack/bazel-generate.sh"

deps-update:
	SYNC_VENDOR=true hack/dockerized " ./hack/dep-update.sh && ./hack/dep-prune.sh && ./hack/bazel-generate.sh"

build-verify:
	hack/build-verify.sh

manifests:
	hack/dockerized "CSV_VERSION=${CSV_VERSION} QUAY_REPOSITORY=${QUAY_REPOSITORY} \
	  DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} \
	  IMAGE_PULL_POLICY=${IMAGE_PULL_POLICY} VERBOSITY=${VERBOSITY} PACKAGE_NAME=${PACKAGE_NAME} ./hack/build-manifests.sh"

.release-functest:
	make functest > .release-functest 2>&1

release-announce: .release-functest
	./hack/release-announce.sh $(RELREF) $(PREREF)

cluster-up:
	./cluster-up/up.sh

cluster-down:
	./cluster-up/down.sh

cluster-build:
	./hack/cluster-build.sh

cluster-clean:
	./hack/cluster-clean.sh

cluster-deploy: cluster-clean
	./hack/cluster-deploy.sh

cluster-sync: cluster-build cluster-deploy

builder-build:
	./hack/builder/build.sh

builder-publish:
	./hack/builder/publish.sh

olm-verify:
	hack/dockerized "./hack/olm.sh verify"

current-dir := $(shell pwd)

build-prom-spec-dumper:
	hack/dockerized "go build -o rule-spec-dumper ./hack/prom-rule-ci/rule-spec-dumper.go"

prom-rules-verify: build-prom-spec-dumper
	./hack/prom-rule-ci/verify-rules.sh ${current-dir}/rule-spec-dumper

olm-push:
	hack/dockerized "DOCKER_TAG=${DOCKER_TAG} CSV_VERSION=${CSV_VERSION} QUAY_USERNAME=${QUAY_USERNAME} \
	    QUAY_PASSWORD=${QUAY_PASSWORD} QUAY_REPOSITORY=${QUAY_REPOSITORY} PACKAGE_NAME=${PACKAGE_NAME} ./hack/olm.sh push"

bump-kubevirtci:
	./hack/bump-kubevirtci.sh

.PHONY: \
	build-verify \
	go-build \
	go-test \
	go-all \
	bazel-generate \
	bazel-build \
	bazel-build-images \
	bazel-push-images \
	bazel-test \
	functest-image-build \
	functest-image-push \
	test \
	clean \
	distclean \
	sync \
	manifests \
	functest \
	release-announce \
	cluster-up \
	cluster-down \
	cluster-clean \
	cluster-deploy \
	cluster-sync \
	olm-verify \
	olm-push
