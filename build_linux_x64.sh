#!/usr/bin/env bash
# Build Linux x64 binary for this Go project.
# Usage: ./build_linux_x64.sh

set -euo pipefail

echo "Building linux/amd64 binary..."

export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

go build -v -o santak-ups-snmp-server.new .

echo "Built santak-ups-snmp-server.new"
