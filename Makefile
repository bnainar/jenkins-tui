GOFLAGS ?=
BINARY ?= jenkins-tui
PKG ?= ./cmd/jenkins-tui
TIMEOUT ?= 120s
HOST_OS ?= $(shell uname -s | tr A-Z a-z)
_UNAME_M := $(shell uname -m)
HOST_ARCH ?= $(if $(filter x86_64,$(_UNAME_M)),amd64,$(if $(filter aarch64,$(_UNAME_M)),arm64,$(_UNAME_M)))
DOCKER_GO_IMAGE ?= golang:1.26-alpine
DOCKER_GO_BIN ?= /usr/local/go/bin/go
DOCKER_HOST_ALIAS ?= --add-host host.docker.internal:host-gateway
DOCKER_RUN_BASE = docker run --rm -v "$(PWD)":/work -w /work \
	$(DOCKER_HOST_ALIAS) \
	-v jenkins-tui_go_mod_cache:/go/pkg/mod \
	-v jenkins-tui_go_build_cache:/root/.cache/go-build \
	-v jenkins-tui_app_cache:/root/.cache/jenkins-tui
DOCKER_RUN = $(DOCKER_RUN_BASE) $(DOCKER_GO_IMAGE)

.PHONY: build run run-docker test tidy fmt clean docker-up docker-down docker-logs

build:
	$(DOCKER_RUN) sh -lc 'GOOS=$(HOST_OS) GOARCH=$(HOST_ARCH) $(DOCKER_GO_BIN) build $(GOFLAGS) -o $(BINARY) $(PKG)'

run: build
	./$(BINARY) -timeout $(TIMEOUT)

run-docker:
	$(DOCKER_RUN_BASE) -it $(DOCKER_GO_IMAGE) sh -lc 'unset NO_COLOR CI; export TERM=xterm-256color COLORTERM=truecolor CLICOLOR_FORCE=1; $(DOCKER_GO_BIN) run $(GOFLAGS) $(PKG) -timeout $(TIMEOUT)'

test:
	$(DOCKER_RUN) sh -lc '$(DOCKER_GO_BIN) test $(GOFLAGS) ./...'

tidy:
	$(DOCKER_RUN) sh -lc '$(DOCKER_GO_BIN) mod tidy'

fmt:
	$(DOCKER_RUN) sh -lc '$(DOCKER_GO_BIN) fmt ./...'

clean:
	rm -f $(BINARY)

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
