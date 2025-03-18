#!/usr/bin/make -f

DOCKER := $(shell which docker)

proto-gen:
	$(DOCKER) run --volume "$(CURDIR):/workspace" --workdir /workspace bufbuild/buf mod update
	$(DOCKER) run --volume "$(CURDIR):/workspace" --workdir /workspace bufbuild/buf generate --config buf.yaml

.PHONY: proto-gen 