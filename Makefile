DOCKER := $(shell which docker)
DOCKER_BUF := $(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace bufbuild/buf

# Define pkgs, run, and cover vairables for test so that we can override them in
# the terminal more easily.
pkgs := $(shell go list ./...)
run := .
count := 1

## help: Show this help message
help: Makefile
	@echo " Choose a command run in "$(PROJECTNAME)":"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'
.PHONY: help

## clean: clean testcache
clean:
	@echo "--> Clearing testcache"
	@go clean --testcache
.PHONY: clean

## cover: generate to code coverage report.
cover:
	@echo "--> Generating Code Coverage"
	@go install github.com/ory/go-acc@latest
	@go-acc -o coverage.txt $(pkgs)
.PHONY: cover

## deps: Install dependencies
deps:
	@echo "--> Installing dependencies"
	@go mod download
	@go mod tidy
.PHONY: deps

## lint: Run linters golangci-lint and markdownlint.
lint: vet
	@echo "--> Running golangci-lint"
	@golangci-lint run --fix

.PHONY: lint

## fmt: Run fixes for linters. Currently only markdownlint.
fmt:
	@echo "--> Formatting markdownlint"
	@markdownlint --config .markdownlint.yaml '**/*.md' -f
.PHONY: fmt

## gci: Format Go imports
gci:
	@echo "--> Formatting Go imports"
	@gci write --section standard --section default --section "prefix(github.com/rollkit/go-execution-abci)" --section "prefix(github.com/rollkit)" .
.PHONY: gci

## vet: Run go vet
vet: 
	@echo "--> Running go vet"
	@go vet $(pkgs)
.PHONY: vet

## test: Running unit tests
test: vet
	@echo "--> Running unit tests"
	@go test -v -race -covermode=atomic -coverprofile=coverage.txt $(pkgs) -run $(run) -count=$(count)
.PHONY: test

proto-gen:
	$(DOCKER) run --volume "$(CURDIR):/workspace" --workdir /workspace bufbuild/buf mod update
	$(DOCKER) run --volume "$(CURDIR):/workspace" --workdir /workspace bufbuild/buf generate --config buf.yaml

.PHONY: proto-gen 