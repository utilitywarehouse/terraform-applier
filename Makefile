
# Image URL to use all building/pushing image targets
IMG ?= terraform-applier:master

.PHONY: generate
generate:
	go generate ./...

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: generate fmt vet ## Run tests.
	go test -v -cover ./...

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

.PHONY: run
run: generate fmt vet ## Run a controller from your host.
	go run ./main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} .


BJS_VERSION="5.3.3"
update-bootstrap-js:
	(cd /tmp/ && curl -L -O https://github.com/twbs/bootstrap/releases/download/v$(BJS_VERSION)/bootstrap-$(BJS_VERSION)-dist.zip)
	(cd /tmp/ && unzip bootstrap-$(BJS_VERSION)-dist.zip)
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/js/bootstrap.js webserver/static/bootstrap/js/bootstrap.js
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/js/bootstrap.min.js webserver/static/bootstrap/js/bootstrap.min.js
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/js/bootstrap.min.js.map webserver/static/bootstrap/js/bootstrap.min.js.map
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/css/bootstrap.css webserver/static/bootstrap/css/bootstrap.css
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/css/bootstrap.min.css webserver/static/bootstrap/css/bootstrap.min.css
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/css/bootstrap.min.css.map webserver/static/bootstrap/css/bootstrap.min.css.map

PRISM_VERSION="1.29.0"
update-prism:
	curl https://cdnjs.cloudflare.com/ajax/libs/prism/$(PRISM_VERSION)/themes/prism.min.css >> webserver/static/prism/css/prism.min.css
	curl https://cdnjs.cloudflare.com/ajax/libs/prism/$(PRISM_VERSION)/components/prism-core.min.js >> webserver/static/prism/js/prism-core.min.js
	curl https://cdnjs.cloudflare.com/ajax/libs/prism/$(PRISM_VERSION)/components/prism-hcl.min.js >> webserver/static/prism/js/prism-hcl.min.js

release:
	@sd "master" "$(VERSION)" manifests/base/namespaced/tf-applier.yaml
	@git add -- manifests/base/namespaced/tf-applier.yaml
	@git commit -m "Release $(VERSION)"
	@sd "$(VERSION)" "master" manifests/base/namespaced/tf-applier.yaml
	@git add -- manifests/base/namespaced/tf-applier.yaml
	@git commit -m "Clean up release $(VERSION)"
