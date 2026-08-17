GOOS := $(shell go env GOOS)

ifeq ($(GOOS),windows)
BIN := bin/devagent.exe
else
BIN := bin/devagent
endif

.PHONY: build test vet integration cross clean daemon sidecar

build:
	go build -o $(BIN) ./cmd/devagent/

test:
	go test ./... -v

vet:
	go vet ./...

integration:
	go run ./cmd/integration_test/

cross:
	@echo "==> windows/amd64"
	GOOS=windows GOARCH=amd64 go build -o bin/devagent-windows-amd64.exe ./cmd/devagent/
	@echo "==> linux/amd64"
	GOOS=linux GOARCH=amd64 go build -o bin/devagent-linux-amd64 ./cmd/devagent/
	@echo "==> linux/arm64"
	GOOS=linux GOARCH=arm64 go build -o bin/devagent-linux-arm64 ./cmd/devagent/
	@echo "==> darwin/arm64"
	GOOS=darwin GOARCH=arm64 go build -o bin/devagent-darwin-arm64 ./cmd/devagent/
	@echo "==> openwrt/mipsle (CGO_ENABLED=0)"
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -ldflags="-s -w" -o bin/devagent-openwrt-mipsle ./cmd/devagent/
	@echo "==> done"

clean:
	rm -rf bin/

daemon: build
	./$(BIN) -mode daemon -port 8080 -config configs/example_device.yaml

sidecar: build
	./$(BIN) -mode sidecar
