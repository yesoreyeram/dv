#!/usr/bin/env bash
# NFR-MAINT-007: mise.toml, go.mod, package.json, the Dockerfile and CI agree on toolchain
# versions, and Node is on an LTS (even) major. The Dockerfile may run ahead (Dependabot
# bumps it), never behind. Exits non-zero listing every mismatch.
set -euo pipefail
cd "$(dirname "$0")/.."

tool() { sed -nE "s/^$1[[:space:]]*=[[:space:]]*\"([^\"]+)\".*/\1/p" mise.toml; }
fail=0
check() { # name want got
  if [[ "$2" != "$3" ]]; then echo "toolchain mismatch: $1: mise.toml=$2 other=$3"; fail=1; fi
}

go_mise=$(tool go)
node_mise=$(tool node)
pnpm_mise=$(tool pnpm)
lint_mise=$(tool golangci-lint)

check "go (go.mod)" "$go_mise" "$(sed -nE 's/^go ([0-9.]+)$/\1/p' go.mod)"
# The image may build with a newer Go than go.mod (Dependabot bumps it on its own), never older.
docker_go=$(sed -nE 's/^FROM golang:([0-9]+\.[0-9]+)-alpine.*/\1/p' deploy/Dockerfile)
if [[ "$(printf '%s\n%s\n' "${go_mise%.*}" "$docker_go" | sort -V | head -n1)" != "${go_mise%.*}" ]]; then
  echo "toolchain mismatch: Dockerfile golang:${docker_go} is older than go ${go_mise}"; fail=1
fi
check "pnpm (package.json)" "$pnpm_mise" "$(sed -nE 's/.*"packageManager": "pnpm@([^"]+)".*/\1/p' package.json)"
check "pnpm (Dockerfile)" "$pnpm_mise" "$(sed -nE 's/^RUN npm install -g (--ignore-scripts )?pnpm@([0-9][^[:space:]]*)$/\2/p' deploy/Dockerfile)"
docker_node=$(sed -nE 's/^FROM node:([0-9]+)-alpine.*/\1/p' deploy/Dockerfile)
if (( docker_node % 2 != 0 || docker_node < ${node_mise%%.*} )); then
  echo "toolchain mismatch: Dockerfile node:${docker_node} must be an LTS (even) major >= ${node_mise%%.*}"; fail=1
fi
while read -r v; do check "node major (ci.yml)" "${node_mise%%.*}" "$v"; done \
  < <(sed -nE 's/^[[:space:]]*node-version: ([0-9]+)$/\1/p' .github/workflows/ci.yml)
check "golangci-lint (ci.yml)" "v$lint_mise" "$(sed -nE 's/^[[:space:]]*version: (v[0-9.]+)$/\1/p' .github/workflows/ci.yml | head -n1)"

# NFR-SEC-015 AC6: govulncheck scans the go.mod toolchain (no silent toolchain switch) and
# prints the Go version it scanned.
while IFS= read -r l; do
  [[ "$l" =~ GOTOOLCHAIN=local[[:space:]]+go[[:space:]]+run[[:space:]]+golang\.org/x/vuln/cmd/govulncheck@ ]] ||
    { echo "toolchain mismatch: govulncheck must run with GOTOOLCHAIN=local: ${l#"${l%%[![:space:]]*}"}"; fail=1; }
  [[ "$l" =~ [[:space:]]-show[[:space:]]+([a-z,]*,)?version ]] ||
    { echo "toolchain mismatch: govulncheck must print the scanned Go version (-show version): ${l#"${l%%[![:space:]]*}"}"; fail=1; }
done < <(grep -hE 'govulncheck@' .github/workflows/*.yml | grep -vE '^[[:space:]]*#')

if (( ${node_mise%%.*} % 2 != 0 )); then echo "node ${node_mise} is not an LTS (even) major"; fail=1; fi
if (( fail == 0 )); then echo "toolchain pins consistent: go ${go_mise}, node ${node_mise}, pnpm ${pnpm_mise}, golangci-lint ${lint_mise}"; fi
exit "$fail"
