#!/usr/bin/env bash
set -euo pipefail

mkdir -p .build

go mod tidy
go test ./...

for dir in ./cmd/*; do
    [[ -d "$dir" ]] || continue

    name="$(basename "$dir")"
    echo "Building $name..."

    go build -o ".build/$name" "$dir"
done
