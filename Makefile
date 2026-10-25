.PHONY: build test check
build:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o bin/agent-vm ./cmd/agent-vm
test:
	go test ./...
check:
	sh scripts/check.sh
