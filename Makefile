GO ?= go
GOFLAGS ?=
BINARY ?= jenx
PKG ?= ./cmd/jenx

.PHONY: build run test tidy fmt clean docker-up docker-down docker-logs

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) $(PKG)

run: build
	./$(BINARY)

test:
	$(GO) test $(GOFLAGS) ./...

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

clean:
	rm -f $(BINARY)

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f --tail=200
