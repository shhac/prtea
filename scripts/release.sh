#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if [ $# -eq 0 ]; then
  printf '%s\n' "Usage: release.sh <patch|minor|major>" >&2
  exit 1
fi

bump_type="$1"

# Get current version from latest git tag
current=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//') || true
if [ -z "$current" ]; then
  current="0.0.0"
fi
IFS='.' read -r major minor patch <<EOF
$current
EOF

case "$bump_type" in
  patch) patch=$((patch + 1)) ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  major) major=$((major + 1)); minor=0; patch=0 ;;
  *) printf '%s\n' "error: invalid bump type '$bump_type'. Use patch, minor, or major." >&2; exit 1 ;;
esac

new_version="$major.$minor.$patch"
tag="v$new_version"

printf '%s\n' "Bumping $current -> $new_version"

git tag "$tag"
git push origin "$tag"

printf '%s\n' "Tagged $tag. Now run: sh scripts/build-release-assets.sh"
