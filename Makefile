BINARY     := debsplash
BUILD_DIR  := ./build
INSTALL_DIR := /usr/local/bin
GO         := $(shell command -v go 2>/dev/null || { [ -x /usr/local/go/bin/go ] && echo /usr/local/go/bin/go; })

.PHONY: all build install uninstall clean test test-integration vet

all: build

build:
	@test -n "$(GO)" || { echo "Error: Go not found. Install Go 1.27.3 or newer from https://go.dev/dl/ or download the prebuilt binary from https://github.com/bradsec/debsplash/releases/latest"; exit 1; }
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) ./main.go
	@echo "Binary built at $(BUILD_DIR)/$(BINARY)"

install:
	@test -f $(BUILD_DIR)/$(BINARY) || { echo "Error: $(BUILD_DIR)/$(BINARY) not found. Run 'make build' first (as your user, not root)."; exit 1; }
	@echo "Installing to $(INSTALL_DIR)/$(BINARY)..."
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Done. Run: sudo debsplash help"

uninstall:
	@echo "Removing $(INSTALL_DIR)/$(BINARY)..."
	rm -f $(INSTALL_DIR)/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)

test:
	$(GO) test ./...

test-integration:
	$(GO) test -tags integration ./...

vet:
	$(GO) vet ./...
