# Detect OS and architecture
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

# Default build target based on current platform
ifeq ($(GOOS),linux)
    LIB_EXT := .so
else ifeq ($(GOOS),darwin)
    LIB_EXT := .dylib
else ifeq ($(GOOS),windows)
    LIB_EXT := .dll
else
    LIB_EXT := .so
endif

.DEFAULT_GOAL := build

build:
	go mod tidy
	go build -buildmode=c-shared -o libxatu$(LIB_EXT) .
	@echo "Built libxatu$(LIB_EXT) for $(GOOS)/$(GOARCH)"

build-linux:
	go mod tidy
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o libxatu.so .

build-darwin:
	go mod tidy
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -buildmode=c-shared -o libxatu.dylib .

build-windows:
	go mod tidy
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -buildmode=c-shared -o libxatu.dll .

build-all: build-linux build-darwin build-windows

clean:
	rm -f libxatu.so libxatu.dylib libxatu.dll libxatu.h