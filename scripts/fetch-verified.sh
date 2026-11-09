#!/usr/bin/env bash
# fetch-verified.sh <https-url> <sha256> <output>
#
# The only way CI workflows download files (NFR-SEC-015 AC1). Fetches <https-url> into a
# temporary file next to <output>, checks it against <sha256> (64 lowercase hex, committed
# in the workflow), and only then renames it to <output>. On any failure the temporary
# file is removed and <output> is not created, so an unverified file never exists, no
# matter what shell control flow surrounds the call.
set -euo pipefail

die() {
  echo "fetch-verified: $*" >&2
  exit 1
}

[[ $# -eq 3 ]] || die "usage: fetch-verified.sh <https-url> <sha256> <output>"
url=$1
want=$2
out=$3

[[ $url == https://* ]] || die "refusing non-HTTPS URL: $url"
[[ $want =~ ^[0-9a-f]{64}$ ]] || die "expected a 64-character lowercase hex SHA-256, got: $want"
[[ -n $out && $out != */ ]] || die "output must be a file path"
[[ ! -e $out ]] || die "refusing to overwrite existing $out"

dir=$(dirname -- "$out")
tmp=$(mktemp "$dir/.fetch-verified.XXXXXX")
trap 'rm -f -- "$tmp"' EXIT

curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
  --tlsv1.2 --connect-timeout 20 --max-time 300 \
  --retry 8 --retry-max-time 240 --retry-connrefused \
  --output "$tmp" -- "$url"

if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum -- "$tmp" | cut -d' ' -f1)
else
  got=$(shasum -a 256 -- "$tmp" | cut -d' ' -f1)
fi
[[ $got == "$want" ]] || die "SHA-256 mismatch for $url: got $got, want $want"

mv -- "$tmp" "$out"
trap - EXIT
echo "fetch-verified: $out ($want)"
