#!/usr/bin/env bash
#
# scripts/install-tbls.sh - install a pinned tbls release binary
# (https://github.com/k1LoW/tbls) into bin/tbls, for `make erd`/
# `make check-erd` (issue #248).
#
# tbls has no asdf/mise plugin, so it can't be pinned in .tool-versions
# the way golangci-lint is. Its own project publishes prebuilt release
# binaries for darwin/linux, amd64/arm64 - this downloads one of those
# rather than `go install`ing from source, which pulls in tbls' full set
# of database-driver dependencies (postgres, mysql, bigquery, ...) and
# takes minutes; a real user only ever needs the sqlite driver it already
# bundles into its release binaries. Bump TBLS_VERSION deliberately, the
# same way CI's golangci-lint version is bumped deliberately rather than
# tracking latest.
#
# Usage: scripts/install-tbls.sh <output-path>

set -euo pipefail

TBLS_VERSION="1.96.0"

out="${1:?usage: install-tbls.sh <output-path>}"

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "install-tbls.sh: unsupported OS '$os' - see https://github.com/k1LoW/tbls/releases" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "install-tbls.sh: unsupported arch '$arch' - see https://github.com/k1LoW/tbls/releases" >&2
    exit 1
    ;;
esac

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

base_url="https://github.com/k1LoW/tbls/releases/download/v${TBLS_VERSION}"

if [ "$os" = "darwin" ]; then
  asset="tbls_v${TBLS_VERSION}_darwin_${arch}.zip"
  curl -sSL -o "$workdir/tbls.zip" "$base_url/$asset"
  unzip -q "$workdir/tbls.zip" -d "$workdir"
else
  asset="tbls_v${TBLS_VERSION}_linux_${arch}.tar.gz"
  curl -sSL -o "$workdir/tbls.tar.gz" "$base_url/$asset"
  tar -xzf "$workdir/tbls.tar.gz" -C "$workdir"
fi

mkdir -p "$(dirname "$out")"
mv "$workdir/tbls" "$out"
chmod +x "$out"

echo "installed tbls v${TBLS_VERSION} -> $out"
