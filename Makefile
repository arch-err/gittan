GO ?= go
BINARY ?= bin/gittan
DIST_DIR ?= dist
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS ?= -s -w -X main.version=$(VERSION)
SOURCES := $(wildcard *.go)

.PHONY: all build test vet fmt fmt-check clean clean-dist release release-linux release-darwin release-windows checksums

all: build

build: $(BINARY)

$(BINARY): $(SOURCES) go.mod go.sum
	mkdir -p $(@D)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $@ .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $(SOURCES)

fmt-check:
	@files="$$(gofmt -l $(SOURCES))"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

clean: clean-dist
	rm -rf bin

clean-dist:
	rm -rf $(DIST_DIR)

release: clean-dist release-linux release-darwin release-windows checksums

release-linux:
	mkdir -p $(DIST_DIR)
	mkdir -p $(DIST_DIR)/linux_amd64 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/linux_amd64/gittan .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_linux_amd64.tar.gz -C $(DIST_DIR)/linux_amd64 gittan
	mkdir -p $(DIST_DIR)/linux_arm64 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/linux_arm64/gittan .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_linux_arm64.tar.gz -C $(DIST_DIR)/linux_arm64 gittan
	mkdir -p $(DIST_DIR)/linux_386 && CGO_ENABLED=0 GOOS=linux GOARCH=386 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/linux_386/gittan .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_linux_386.tar.gz -C $(DIST_DIR)/linux_386 gittan
	rm -rf $(DIST_DIR)/linux_amd64 $(DIST_DIR)/linux_arm64 $(DIST_DIR)/linux_386

release-darwin:
	mkdir -p $(DIST_DIR)
	mkdir -p $(DIST_DIR)/darwin_amd64 && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/darwin_amd64/gittan .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_darwin_amd64.tar.gz -C $(DIST_DIR)/darwin_amd64 gittan
	mkdir -p $(DIST_DIR)/darwin_arm64 && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/darwin_arm64/gittan .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_darwin_arm64.tar.gz -C $(DIST_DIR)/darwin_arm64 gittan
	rm -rf $(DIST_DIR)/darwin_amd64 $(DIST_DIR)/darwin_arm64

release-windows:
	mkdir -p $(DIST_DIR)
	mkdir -p $(DIST_DIR)/windows_amd64 && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/windows_amd64/gittan.exe .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_windows_amd64.tar.gz -C $(DIST_DIR)/windows_amd64 gittan.exe
	if command -v zip >/dev/null 2>&1; then (cd $(DIST_DIR)/windows_amd64 && zip ../gittan_$(VERSION)_windows_amd64.zip gittan.exe); fi
	mkdir -p $(DIST_DIR)/windows_arm64 && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/windows_arm64/gittan.exe .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_windows_arm64.tar.gz -C $(DIST_DIR)/windows_arm64 gittan.exe
	if command -v zip >/dev/null 2>&1; then (cd $(DIST_DIR)/windows_arm64 && zip ../gittan_$(VERSION)_windows_arm64.zip gittan.exe); fi
	mkdir -p $(DIST_DIR)/windows_386 && CGO_ENABLED=0 GOOS=windows GOARCH=386 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/windows_386/gittan.exe .
	tar -czf $(DIST_DIR)/gittan_$(VERSION)_windows_386.tar.gz -C $(DIST_DIR)/windows_386 gittan.exe
	if command -v zip >/dev/null 2>&1; then (cd $(DIST_DIR)/windows_386 && zip ../gittan_$(VERSION)_windows_386.zip gittan.exe); fi
	rm -rf $(DIST_DIR)/windows_amd64 $(DIST_DIR)/windows_arm64 $(DIST_DIR)/windows_386

checksums:
	if command -v sha256sum >/dev/null 2>&1; then \
		(cd $(DIST_DIR) && sha256sum gittan_* > checksums.txt); \
	elif command -v shasum >/dev/null 2>&1; then \
		(cd $(DIST_DIR) && shasum -a 256 gittan_* > checksums.txt); \
	fi