#!/usr/bin/env bash
# Entry point in the clean golang container: no source, only `go get` from the public proxy.
set -uo pipefail
printf '{"go":"%s","os":"%s"}\n' "$(go version)" "$(. /etc/os-release; echo "$PRETTY_NAME")" > /results/env.json
mkdir -p /work/runner && cp /harness/runner/main.go /work/runner/ && cd /work/runner \
  && go mod init example.com/runner >/dev/null 2>&1 && go run .
