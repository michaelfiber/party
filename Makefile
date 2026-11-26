# Party
#
# Raspberry Pi 5 (uname: aarch64) → GOOS=linux GOARCH=arm64

APP       := party
PKG       := ./cmd/party
BUILD_DIR := dist

PI_GOOS   := linux
PI_GOARCH := arm64
PI_OUT    := $(BUILD_DIR)/$(APP)-linux-arm64

GETFILE   := getfile
GETFILE_PI_OUT := $(BUILD_DIR)/$(GETFILE)-linux-arm64

.PHONY: all build pi test clean

all: build

build:
	go build -o $(APP) $(PKG)
	go build -o $(GETFILE) ./cmd/getfile

pi:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(PI_GOOS) GOARCH=$(PI_GOARCH) go build -o $(PI_OUT) $(PKG)
	CGO_ENABLED=0 GOOS=$(PI_GOOS) GOARCH=$(PI_GOARCH) go build -o $(GETFILE_PI_OUT) ./cmd/getfile
	@echo "built $(PI_OUT)"
	@echo "built $(GETFILE_PI_OUT)"

test:
	go test ./...

clean:
	rm -f $(APP) $(GETFILE)
	rm -rf $(BUILD_DIR)
