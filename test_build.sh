#!/bin/bash
set -e

echo "=== Running unit tests ==="
go test ./... -v

echo ""
echo "=== Building binary ==="
go build -o intuneme .

echo ""
echo "=== Verifying commands ==="
./intuneme --help
./intuneme init --help
./intuneme start --help
./intuneme stop --help
./intuneme status --help
./intuneme destroy --help

echo ""
echo "=== All checks passed ==="
