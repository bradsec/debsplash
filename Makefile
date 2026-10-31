BINARY     := debsplash
BUILD_DIR  := ./build
INSTALL_DIR := /usr/local/bin
GO         := $(shell which go 2>/dev/null || echo /usr/local/go/bin/go)

.PHONY: all build install uninstall clean test test-integration vet

all: build

build:
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) ./main.go
	@echo "Binary built at $(BUILD_DIR)/$(BINARY)"

install:
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
