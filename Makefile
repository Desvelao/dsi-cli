VERSION ?= dev
# Run everything inside the dev container, as the host user.
RUN := docker compose run --rm --user $(shell id -u):$(shell id -g) dev

.PHONY: image test build lint fmt tidy shell golden parity
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

# Regenerate golden fixtures from the Python reference implementation.
PY_DEPS := typer rich opyml vobject rfeed markdown pillow qrcode cryptography requests boto3 feedgen
golden:
	docker compose run --rm py sh -c 'pip install -q --disable-pip-version-check $(PY_DEPS) && PYTHONPATH=. python testdata/generate.py && chown -R $(shell id -u):$(shell id -g) testdata'

# Differential check: the same command lines through the Python CLI and the Go binary.
parity: build
	docker compose run --rm py sh -c 'pip install -q --disable-pip-version-check $(PY_DEPS) && PYTHONPATH=. python testdata/parity/parity.py $(ARGS)'
