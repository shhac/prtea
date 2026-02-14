#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "error: go is required to build release assets" >&2
  exit 1
fi

version=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
commit=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)

if [ -z "$version" ]; then
  printf '%s\n' "error: no git tag found. Run scripts/release.sh first." >&2
  exit 1
fi

outdir="release"
mkdir -p "$outdir"

ldflags="-s -w -X main.version=$version -X main.commit=$commit -X main.date=$date"

build() {
  os=$1
  arch=$2
  outfile="prtea-${os}-${arch}"
  printf '%s\n' "Building $outfile ($os/$arch)"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$outdir/$outfile" ./cmd/prtea
}

build "darwin" "arm64"
build "darwin" "amd64"
build "linux"  "arm64"
build "linux"  "amd64"

# Create tarballs
(
  cd "$outdir"
  for bin in prtea-darwin-arm64 prtea-darwin-amd64 prtea-linux-arm64 prtea-linux-amd64; do
    tar czf "${bin}.tar.gz" "$bin"
  done
)

# Generate checksums
(
  cd "$outdir"
  rm -f checksums-sha256.txt
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum *.tar.gz > checksums-sha256.txt
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 *.tar.gz | awk '{print $1 "  " $2}' > checksums-sha256.txt
  elif command -v openssl >/dev/null 2>&1; then
    for f in *.tar.gz; do
      h=$(openssl dgst -sha256 "$f" | awk '{print $2}')
      printf '%s  %s\n' "$h" "$f"
    done > checksums-sha256.txt
  else
    printf '%s\n' "error: need sha256sum, shasum, or openssl to generate checksums" >&2
    exit 1
  fi
)

printf '%s\n' "Done. Assets in $outdir/ ready for GitHub release v$version."
