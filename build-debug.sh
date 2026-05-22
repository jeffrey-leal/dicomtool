#!/usr/bin/env bash
set -euo pipefail
export PATH="/c/Program Files/Go/bin:$PATH"

go build -o dicomtool.exe .
echo "Built dicomtool.exe (debug, Windows)"
