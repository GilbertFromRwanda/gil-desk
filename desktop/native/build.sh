#!/usr/bin/env bash
# Builds the napi-rs native addon and stages it as index.node (planner F-18/F-19).
set -euo pipefail
cd "$(dirname "$0")"

cargo build

case "$(uname -s)" in
  Darwin*)            src="target/debug/libnexdesk_native.dylib" ;;
  MINGW*|MSYS*|CYGWIN*) src="target/debug/nexdesk_native.dll" ;;
  *)                   src="target/debug/libnexdesk_native.so" ;;
esac

cp -f "$src" index.node
echo "Built index.node. Run 'node smoke-test.js' to verify."
