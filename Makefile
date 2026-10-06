VERSION ?= dev
# Run everything inside the dev container, as the host user.
RUN := docker compose run --rm --user $(shell id -u):$(shell id -g) dev

.PHONY: image test build lint fmt tidy shell
image:
	docker compose build dev

test:
	$(RUN) go test ./...

build:
	$(RUN) go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/dsi ./cmd/dsi

lint:
	$(RUN) sh -c 'go vet ./... && test -z "$$(gofmt -l .)"'

fmt:
	$(RUN) gofmt -w .

tidy:
	$(RUN) go mod tidy

shell:
	$(RUN) bash
