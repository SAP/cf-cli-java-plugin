#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "Compiling WorkloadApp.java..."
mkdir -p out
javac --release 11 -d out WorkloadApp.java

echo "Packaging workload.jar..."
jar cfe ../workload.jar WorkloadApp -C out .

echo "Done: $(ls -lh ../workload.jar | awk '{print $5, $9}')"
