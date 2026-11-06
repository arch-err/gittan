GO ?= go
BINARY ?= bin/gittan
SOURCES := $(wildcard *.go)

.PHONY: all build test vet fmt fmt-check clean

all: build

build: $(BINARY)

$(BINARY): $(SOURCES) go.mod go.sum
	mkdir -p $(@D)
	$(GO) build -trimpath -o $@ .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $(SOURCES)

fmt-check:
	@files="$$(gofmt -l $(SOURCES))"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

clean:
	rm -rf bin