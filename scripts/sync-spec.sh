#!/usr/bin/env sh
# Copy the Orbits API spec (orbits-backend's libs/contracts/openapi.yaml) into testdata/openapi.yaml, or check
# that the copy is current.
#
#   scripts/sync-spec.sh <path-or-url>           write testdata/openapi.yaml from a file or URL
#   scripts/sync-spec.sh --check <path-or-url>   exit 1 if testdata/openapi.yaml differs from it
#
# Without an argument the source is $ORBITS_SPEC_URL. After a sync, regenerate the types with
# `go run ./scripts/gentypes` and run `go test ./...`.
#
# The copy is the source byte for byte. The repository's wording test (hygiene_test.go) checks it like every
# other file.
set -eu

CHECK=0
if [ "${1:-}" = "--check" ]; then
  CHECK=1
  shift
fi
SRC="${1:-${ORBITS_SPEC_URL:-}}"
if [ -z "$SRC" ]; then
  echo "usage: scripts/sync-spec.sh [--check] <path-or-url>  (or set ORBITS_SPEC_URL)" >&2
  exit 2
fi
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/testdata/openapi.yaml"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

case "$SRC" in
  http://* | https://*)
    curl --fail --silent --show-error --location --retry 3 -o "$TMP" "$SRC"
    ;;
  *)
    cp "$SRC" "$TMP"
    ;;
esac

if [ "$CHECK" = 1 ]; then
  if cmp -s "$TMP" "$DEST"; then
    echo "testdata/openapi.yaml matches $SRC"
    exit 0
  fi
  echo "testdata/openapi.yaml differs from $SRC: run scripts/sync-spec.sh $SRC, then go run ./scripts/gentypes" >&2
  diff -u "$DEST" "$TMP" | head -40 >&2 || true
  exit 1
fi
cp "$TMP" "$DEST"
echo "Wrote testdata/openapi.yaml from $SRC"
