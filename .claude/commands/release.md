---
description: Build, release, and publish to Homebrew
argument-hint: <patch|minor|major>
---

# Release

Perform a full release: version bump, build, GitHub release, and Homebrew tap update.

## Arguments

- `$ARGUMENTS` — version bump type: `patch`, `minor`, or `major`

## Instructions

You are performing a release of the `prtea` CLI. Follow these steps exactly.

### Pre-flight

1. Confirm the working tree is clean (`git st`). If not, stop and ask.
2. Run `go build ./...` and `go test ./...`. If either fails, stop and fix.
3. Show the user what version bump will happen (current version from latest git tag + bump type).

### Step 1: Version bump, tag, and push

```bash
sh scripts/release.sh <bump-type>
```

The release script reads the latest git tag, bumps the version, creates a new tag, and pushes it. Capture the new version number from the output for subsequent steps. Verify the tag was pushed successfully before continuing.

### Step 2: Build release binaries

Clean up any leftover artifacts from previous builds, then build:

```bash
rm -f release/prtea-* release/checksums-sha256.txt
sh scripts/build-release-assets.sh
```

This creates binaries and tarballs in `release/` for all platforms (darwin-arm64, darwin-amd64, linux-arm64, linux-amd64) plus `checksums-sha256.txt`. Verify all 4 tarballs and the checksum file exist in `release/` before continuing.

### Step 3: Create GitHub release

Generate release notes from commits since the previous tag:

```bash
# Get the previous tag (empty if first release)
prev_tag=$(git tag --sort=-v:refname | head -2 | tail -1)
# Get commit subjects between tags
git log --pretty=format:"- %s" "${prev_tag}..v<NEW_VERSION>" --no-merges | grep -v "^- v[0-9]"
```

For the first release (no previous tag), use all commits:

```bash
git log --pretty=format:"- %s" "v<NEW_VERSION>" --no-merges | grep -v "^- v[0-9]"
```

Use those to write concise release notes, then create the release:

```bash
gh release create v<NEW_VERSION> release/*.tar.gz release/checksums-sha256.txt \
  --title "v<NEW_VERSION>" \
  --notes "<release notes>"
```

Verify the release was created: `gh release view v<NEW_VERSION>`. If the upload timed out, retry with `gh release upload v<NEW_VERSION> <missing-files>`.

### Step 4: Update Homebrew tap

The Homebrew formula lives in the `homebrew-tap` sibling repo (`../homebrew-tap` relative to this repo's root).

First, check if the sibling repo exists:

```bash
ls ../homebrew-tap/Formula/prtea.rb
```

**If it doesn't exist:** Create a new formula at `../homebrew-tap/Formula/prtea.rb` following the pattern of other formulas in that repo. Use the template below.

**If it exists:** Read the SHA256 checksums from `release/checksums-sha256.txt` and update the formula:

1. Read `../homebrew-tap/Formula/prtea.rb`
2. Update:
   - `version` to the new version (bare number, no `v` prefix, e.g., `"1.0.0"`)
   - All `url` lines to use `v<NEW_VERSION>`
   - All `sha256` values from the checksums (match darwin-arm64, darwin-amd64, linux-arm64, linux-amd64 tarballs)
   - The `assert_match` version string in the test block
3. Write the updated formula
4. Commit and push:
   ```bash
   cd ../homebrew-tap
   git add Formula/prtea.rb
   git commit -m "prtea <NEW_VERSION>"
   git push
   ```

#### Formula template (for first release)

```ruby
class Prtea < Formula
  desc "A TUI for reviewing GitHub pull requests"
  homepage "https://github.com/shhac/prtea"
  version "<NEW_VERSION>"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/shhac/prtea/releases/download/v<NEW_VERSION>/prtea-darwin-arm64.tar.gz"
      sha256 "<SHA256>"
    end
    on_intel do
      url "https://github.com/shhac/prtea/releases/download/v<NEW_VERSION>/prtea-darwin-amd64.tar.gz"
      sha256 "<SHA256>"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/shhac/prtea/releases/download/v<NEW_VERSION>/prtea-linux-arm64.tar.gz"
      sha256 "<SHA256>"
    end
    on_intel do
      url "https://github.com/shhac/prtea/releases/download/v<NEW_VERSION>/prtea-linux-amd64.tar.gz"
      sha256 "<SHA256>"
    end
  end

  def install
    bin.install Dir["prtea-*"].first => "prtea"
  end

  test do
    assert_match "<NEW_VERSION>", shell_output("#{bin}/prtea --version")
  end
end
```

### Step 5: Report

Show the user:

- New version number
- GitHub release URL
- Homebrew tap commit (if applicable)
- `brew upgrade shhac/tap/prtea` command for users
