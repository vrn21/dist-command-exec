#!/bin/bash
# proto-gen.sh - Generate protobuf code using buf
set -e

cd "$(dirname "$0")/.."

echo "Generating protobuf code..."
cd proto && buf generate

echo "Done! Generated files in gen/go/"
