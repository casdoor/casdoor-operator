IMG ?= casbin/casdoor-operator:latest
CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.22.0

.PHONY: all
all: build

.PHONY: generate
generate: ## Generate DeepCopy methods, the CRD and the ClusterRole.
	$(CONTROLLER_GEN) object:headerFile=hack/boilerplate.go.txt paths=./api/v1alpha1
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths=./api/v1alpha1 paths=./internal/controller 		output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: build
build: generate fmt vet ## Build the manager binary.
	go build -o bin/manager ./cmd

.PHONY: run
run: generate fmt vet ## Run the operator against the cluster in ~/.kube/config.
	go run ./cmd

.PHONY: docker-build
docker-build:
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push:
	docker push $(IMG)

.PHONY: install
install: generate ## Install the CRD.
	kubectl apply --server-side -k config/crd

.PHONY: uninstall
uninstall:
	kubectl delete -k config/crd

.PHONY: deploy
deploy: generate ## Deploy the operator.
	kubectl apply --server-side -k config/default

.PHONY: undeploy
undeploy:
	kubectl delete -k config/default
