# Party
#
# Raspberry Pi 5 (uname: aarch64) → GOOS=linux GOARCH=arm64

APP       := party
PKG       := ./cmd/party
BUILD_DIR := dist

PI_GOOS   := linux
PI_GOARCH := arm64
PI_OUT    := $(BUILD_DIR)/$(APP)-linux-arm64

.PHONY: all build pi test clean

all: build

build:
	go build -o $(APP) $(PKG)

pi:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(PI_GOOS) GOARCH=$(PI_GOARCH) go build -o $(PI_OUT) $(PKG)
	@echo "built $(PI_OUT)"

test:
	go test ./...

clean:
	rm -f $(APP)
	rm -rf $(BUILD_DIR)
