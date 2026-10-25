# List available commands
@_default:
    just --list --unsorted

# Build the macOS ARM64 executable without CGO
build:
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o bin/agent-vm ./cmd/agent-vm

# Run all Go tests
test:
    go test ./...

# Run all verification checks
check:
    sh scripts/check.sh
