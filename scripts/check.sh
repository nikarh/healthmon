#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

files=$(gofmt -l "$ROOT_DIR")
if [ -n "$files" ]; then
  echo "$files"
  exit 1
fi

(cd "$ROOT_DIR/web" && npm run lint)
(cd "$ROOT_DIR/web" && npm run format:check)
(cd "$ROOT_DIR/web" && npm run build)

go vet ./...

go test ./...

go build -o /tmp/healthmon ./cmd/healthmon

go build -o /tmp/healthmon-dev -tags dev ./cmd/healthmon
