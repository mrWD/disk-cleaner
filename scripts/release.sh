#!/bin/sh
# Starts a release: checks the checkout and pushes a tag like v1.2.0.
# GitHub Actions does the rest (.github/workflows/release.yml): tests,
# builds, signs, notarises and publishes it on the Releases page.
#
#   ./scripts/release.sh 1.2.0
set -e
cd "$(dirname "$0")/.."

VERSION="$1"
case "$VERSION" in
[0-9]*.[0-9]*.[0-9]*) ;;
*) echo "Usage: $0 1.2.0   (three numbers: major.minor.patch)"; exit 1 ;;
esac

# A release is made from what's on GitHub's main, nothing else: no local
# changes, no commits that aren't pushed.
[ "$(git rev-parse --abbrev-ref HEAD)" = "main" ] || { echo "Switch to main first."; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "Commit or put aside your changes first."; exit 1; }
git fetch -q origin main
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || { echo "main and origin/main differ: push or pull first."; exit 1; }
if git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null; then
	echo "v$VERSION already exists."
	exit 1
fi

git tag -a "v$VERSION" -m "Disk Cleaner $VERSION"
git push origin "v$VERSION"
echo "Pushed v$VERSION. Follow the build: gh run watch  (or the Actions tab on GitHub)"
