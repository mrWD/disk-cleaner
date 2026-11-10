#!/bin/bash
# Signs and notarises dist/Disk Cleaner.app and packs it for handing out:
#   dist/Disk-Cleaner.dmg — what people download from the website;
#   dist/Disk-Cleaner.zip — what the app's updater downloads (a .zip is
#                           simpler to unpack in code than a disk image).
#
# Three levels of signing, and only the last one works for other people:
#   1. ad-hoc (scripts/build.sh) — runs on the Mac it was built on;
#   2. Apple Development — runs on your team's devices;
#   3. Developer ID Application plus notarisation — runs for anybody, and
#      Gatekeeper opens it without "the developer cannot be verified".
#
# Usage (after ./scripts/build.sh --release 1.2.0):
#   packaging/sign_macos.sh                                   # finds the Developer ID itself
#   NOTARY_PROFILE=disk-cleaner packaging/sign_macos.sh       # on your Mac: a notarytool keychain profile
#   NOTARY_APPLE_ID=… NOTARY_TEAM_ID=… NOTARY_PASSWORD=… …   # on GitHub Actions: from the secrets
set -euo pipefail
cd "$(dirname "$0")/.."

APP="dist/Disk Cleaner.app"
DMG="dist/Disk-Cleaner.dmg"
ZIP="dist/Disk-Cleaner.zip"

IDENTITY="${1:-}"
if [ -z "$IDENTITY" ]; then
	IDENTITY="$(security find-identity -v -p codesigning |
		sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -1)"
fi
if [ -z "$IDENTITY" ]; then
	echo "No Developer ID Application certificate in the keychain. What there is:"
	security find-identity -v -p codesigning | sed 's/^/  /'
	exit 1
fi
[ -d "$APP" ] || { echo "No $APP: build it first with ./scripts/build.sh --release 1.2.0"; exit 1; }

# --options runtime turns on the hardened runtime, which notarisation
# requires. Disk Cleaner needs no exceptions from it (no JIT, no plug-ins),
# so there is no entitlements file. --timestamp: Apple's timestamp server
# vouches for when it was signed, which notarisation also requires.
echo "→ Signing as $IDENTITY"
codesign --force --timestamp --options runtime --sign "$IDENTITY" "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"

# Access to the notary service: a keychain profile on your own Mac, or the
# details in environment variables on a build machine.
NOTARY_ARGS=()
if [ -n "${NOTARY_PROFILE:-}" ]; then
	NOTARY_ARGS=(--keychain-profile "$NOTARY_PROFILE")
elif [ -n "${NOTARY_APPLE_ID:-}" ] && [ -n "${NOTARY_TEAM_ID:-}" ] && [ -n "${NOTARY_PASSWORD:-}" ]; then
	NOTARY_ARGS=(--apple-id "$NOTARY_APPLE_ID" --team-id "$NOTARY_TEAM_ID" --password "$NOTARY_PASSWORD")
fi

notarize() {
	echo "→ Notarising $1 (usually a few minutes)"
	xcrun notarytool submit "$1" "${NOTARY_ARGS[@]}" --wait
}

# The order matters: the app is stapled BEFORE it goes into the .zip and the
# .dmg. Otherwise they'd carry an app without its notarisation ticket: with
# internet Gatekeeper would ask Apple and let it through, offline it wouldn't.
# notarytool takes an archive, not a folder, hence the first .zip.
if [ ${#NOTARY_ARGS[@]} -gt 0 ]; then
	ditto -c -k --keepParent "$APP" "$ZIP"
	notarize "$ZIP"
	xcrun stapler staple "$APP"
else
	echo "→ No notarisation details: the build is signed but not notarised"
fi

rm -f "$ZIP"
ditto -c -k --keepParent "$APP" "$ZIP"

# The disk image: the app and a link to Applications, so it can be dragged across.
echo "→ Making $DMG"
stage="$(mktemp -d)"
cp -R "$APP" "$stage/"
ln -s /Applications "$stage/Applications"
rm -f "$DMG"
hdiutil create -volname "Disk Cleaner" -srcfolder "$stage" -ov -format UDZO "$DMG"
rm -rf "$stage"
codesign --force --timestamp --sign "$IDENTITY" "$DMG"

if [ ${#NOTARY_ARGS[@]} -gt 0 ]; then
	notarize "$DMG"
	xcrun stapler staple "$DMG"
fi

ls -lh "$DMG" "$ZIP"
