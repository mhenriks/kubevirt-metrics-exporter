IMAGE ?= quay.io/openshift-virtualization/kubevirt-metrics-exporter
TAG ?= latest

.PHONY: generate build test image push deploy deploy-kubernetes deploy-manifest deploy-manifest-kubernetes undeploy undeploy-kubernetes setup-test-e2e test-e2e cleanup-test-e2e clean lint fmt

generate:
	go generate ./pkg/ebpf/...

build: generate
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w" -o bin/kubevirt-metrics-exporter ./cmd/exporter/

test:
	go test -v -count=1 ./...

image:
	podman build -f Containerfile -t $(IMAGE):$(TAG) .

push: image
	podman push $(IMAGE):$(TAG)

KUSTOMIZE_IMAGE_PATTERN := image: quay.io/openshift-virtualization/kubevirt-metrics-exporter:latest
KUSTOMIZE_IMAGE_REPLACE := image: $(IMAGE):$(TAG)

deploy:
	kustomize build deploy/openshift/ | grep -qF '$(KUSTOMIZE_IMAGE_PATTERN)' || { echo "ERROR: base image not found in manifest; image substitution would silently fail" >&2; exit 1; }
	kustomize build deploy/openshift/ | sed 's|$(KUSTOMIZE_IMAGE_PATTERN)|$(KUSTOMIZE_IMAGE_REPLACE)|' | oc apply -f -

deploy-kubernetes:
	kustomize build deploy/kubernetes/ | grep -qF '$(KUSTOMIZE_IMAGE_PATTERN)' || { echo "ERROR: base image not found in manifest; image substitution would silently fail" >&2; exit 1; }
	kustomize build deploy/kubernetes/ | sed 's|$(KUSTOMIZE_IMAGE_PATTERN)|$(KUSTOMIZE_IMAGE_REPLACE)|' | kubectl apply -f -

deploy-manifest:
	@kustomize build deploy/openshift/ | sed 's|$(KUSTOMIZE_IMAGE_PATTERN)|$(KUSTOMIZE_IMAGE_REPLACE)|'

deploy-manifest-kubernetes:
	@kustomize build deploy/kubernetes/ | sed 's|$(KUSTOMIZE_IMAGE_PATTERN)|$(KUSTOMIZE_IMAGE_REPLACE)|'

undeploy:
	oc delete -k deploy/openshift/ --ignore-not-found

undeploy-kubernetes:
	kubectl delete -k deploy/kubernetes/ --ignore-not-found

setup-test-e2e:
	hack/e2e-setup.sh

test-e2e:
	go test ./test/e2e/... -v -ginkgo.v -tags=e2e -timeout 15m

cleanup-test-e2e:
	hack/e2e-teardown.sh

clean:
	rm -rf bin/
	rm -f pkg/ebpf/*.o

lint:
	golangci-lint run ./...

fmt:
	find . -name '*.go' -not -path './vendor/*' -exec gofmt -s -w {} +
