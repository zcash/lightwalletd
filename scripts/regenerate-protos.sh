#!/usr/bin/env bash
#
# Regenerates all .pb.go files using the system protoc toolchain.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

echo "Regenerating proto files..."

PROTOS=(
    service.proto
    darkside.proto
    compact_formats.proto
)

(cd walletrpc && for proto in "${PROTOS[@]}"; do
    echo "  ${proto}"
    protoc \
        --go_out=. --go_opt=paths=source_relative \
        --go-grpc_out=. --go-grpc_opt=paths=source_relative \
        "${proto}"
done)

go mod tidy

echo ""
echo "Proto regeneration complete."
