#!/bin/sh
# Builds Disk Cleaner.app and, with the --install flag, installs it into /Applications.
#
# First it installs whatever is missing for the build:
#   1. Xcode Command Line Tools — the C compiler: Fyne builds part of itself from C;
#   2. Go — through Homebrew, or if that is missing, from the official archive
#      from go.dev into ~/sdk (no administrator password needed);
#   3. fyne — the tool that packs the built binary into an .app.
#
# Then:
#   4. builds the app (fyne package);
#   5. adds to Info.plist the text that macOS shows when the app asks for
#      access to Downloads (NSDownloadsFolderUsageDescription);
#   6. writes the checkout's path into the app (Resources/source-dir.txt),
#      so the app's update button knows where to run git pull and this script;
#   7. signs the app again (ad-hoc, without a developer certificate):
#      after Info.plist is edited, the old signature no longer matches.
#
# Run it from any folder:
#   ./scripts/build.sh                  — only build into dist/
#   ./scripts/build.sh --install        — build, install into /Applications and open it
#                                         (a running copy is closed, the old one is replaced)
#   ./scripts/build.sh --release 1.2.0  — a build to hand out (GitHub Actions runs it):
#                                         version 1.2.0, one binary for Apple Silicon and
#                                         Intel, left unsigned for packaging/sign_macos.sh,
#                                         and without the source folder (step 6): a
#                                         downloaded copy updates from GitHub Releases
set -e
cd "$(dirname "$0")/.."

MODE="build"
case "$1" in
--install) MODE="install" ;;
--release)
	MODE="release"
	VERSION="$2"
	[ -n "$VERSION" ] || { echo "Usage: $0 --release 1.2.0"; exit 1; }
	;;
esac

APP="Disk Cleaner.app"
PLIST="$APP/Contents/Info.plist"
TEXT="Disk Cleaner looks in Downloads for installers, app builds and archives you may no longer need. Nothing is removed without your confirmation, and files go to the Trash."
FYNE_VERSION="v1.7.4" # the version the project was tested with

# The Go version we need comes from go.mod (the line "go 1.27.3").
GO_VERSION="$(awk '$1 == "go" { print $2 }' go.mod)"

# Where Homebrew, Go and programs installed with go install usually live.
# They may be missing from PATH, for example if Homebrew was installed but
# not added to ~/.zprofile.
PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin:$HOME/sdk/go$GO_VERSION/bin"
export PATH

# If the environment has GOTOOLCHAIN=local, Go won't download the version it needs by itself.
export GOTOOLCHAIN=auto

step() { printf '\n==> %s\n' "$1"; }

# --- 1. Xcode Command Line Tools ---
if ! xcode-select -p >/dev/null 2>&1; then
	step "Installing Xcode Command Line Tools (a macOS window will open, click Install)"
	xcode-select --install 2>/dev/null || true
	echo "Waiting for the installation to finish. If you closed that window, press Ctrl+C and run this script again."
	# The installation runs in a separate macOS window; we wait until it finishes.
	until xcode-select -p >/dev/null 2>&1; do sleep 5; done
fi

# --- 2. Go ---

# go_ok tells whether there is a Go that can cope by itself. Since version 1.21, when Go sees
# a newer version than its own in go.mod, it downloads the needed one itself (GOTOOLCHAIN=auto).
# An older Go can't do that, so we treat it as missing.
go_ok() {
	command -v go >/dev/null 2>&1 || return 1
	v="$(go env GOVERSION 2>/dev/null | sed 's/^go//')" # "go1.27.3" → "1.27.3"
	major="${v%%.*}"
	rest="${v#*.}"
	minor="${rest%%.*}"
	[ "$major" -gt 1 ] 2>/dev/null || [ "$minor" -ge 21 ] 2>/dev/null
}

if ! go_ok; then
	if command -v brew >/dev/null 2>&1; then
		step "Installing Go with Homebrew"
		if brew list go >/dev/null 2>&1; then
			brew upgrade go
		else
			brew install go
		fi
	else
		# No Homebrew: take the official Go archive and put it into ~/sdk.
		case "$(uname -m)" in
		arm64) ARCH=arm64 ;;
		*) ARCH=amd64 ;;
		esac
		step "Downloading Go $GO_VERSION from go.dev into ~/sdk/go$GO_VERSION"
		mkdir -p "$HOME/sdk"
		TMP="$(mktemp -d)"
		curl -fL "https://go.dev/dl/go$GO_VERSION.darwin-$ARCH.tar.gz" | tar -xz -C "$TMP"
		rm -rf "$HOME/sdk/go$GO_VERSION"
		mv "$TMP/go" "$HOME/sdk/go$GO_VERSION"
		rm -rf "$TMP"
		echo "Go is in ~/sdk/go$GO_VERSION/bin. To use it in your terminal, add this line to ~/.zprofile:"
		echo "  export PATH=\"\$HOME/sdk/go$GO_VERSION/bin:\$PATH\""
	fi
	go_ok || { echo "Go could not be installed. Install it from https://go.dev/dl/ and run this script again."; exit 1; }
fi

# --- 3. fyne ---
GOBIN_DIR="$(go env GOBIN)"
[ -n "$GOBIN_DIR" ] || GOBIN_DIR="$(go env GOPATH)/bin"
FYNE="$(command -v fyne || true)"
[ -n "$FYNE" ] || FYNE="$GOBIN_DIR/fyne"
if [ ! -x "$FYNE" ]; then
	step "Installing the fyne packaging tool $FYNE_VERSION"
	go install "fyne.io/tools/cmd/fyne@$FYNE_VERSION"
	FYNE="$GOBIN_DIR/fyne"
fi

# --- 4–7. Build ---
step "Building $APP (the first build takes a few minutes)"
if [ "$MODE" = "release" ]; then
	# The build number only has to grow; on GitHub Actions it's the run number.
	"$FYNE" package -os darwin -release --app-version "$VERSION" --app-build "${GITHUB_RUN_NUMBER:-1}"

	# fyne builds for this Mac's processor. Build the other one too and join
	# them into one universal binary, so the download runs on any Mac.
	BIN="$APP/Contents/MacOS/disk-cleaner"
	case "$(uname -m)" in
	arm64) OTHER=amd64 ;;
	*) OTHER=arm64 ;;
	esac
	step "Adding the $OTHER binary"
	TMP_BIN="$(mktemp)"
	CGO_ENABLED=1 GOARCH="$OTHER" go build -trimpath -ldflags="-s -w" -o "$TMP_BIN" .
	lipo -create "$BIN" "$TMP_BIN" -output "$BIN.universal"
	mv "$BIN.universal" "$BIN"
	rm -f "$TMP_BIN"
else
	"$FYNE" package -os darwin -release
fi

/usr/libexec/PlistBuddy -c "Delete :NSDownloadsFolderUsageDescription" "$PLIST" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Add :NSDownloadsFolderUsageDescription string $TEXT" "$PLIST"

if [ "$MODE" != "release" ]; then
	# Remember which checkout the app was built from: the app's update button
	# runs git pull and this script again in that folder.
	pwd > "$APP/Contents/Resources/source-dir.txt"

	codesign --force --deep --sign - "$APP"
fi

mkdir -p dist
rm -rf "dist/$APP"
mv "$APP" dist/
echo "Built: dist/$APP"

if [ "$MODE" = "install" ]; then
	# Close the running copy so we can replace it, and install the new one.
	# We move rather than copy: otherwise a second copy stays in dist/,
	# and Spotlight and Launchpad show two identical Disk Cleaners.
	step "Installing to /Applications"
	osascript -e 'quit app "Disk Cleaner"' 2>/dev/null || true
	rm -rf "/Applications/$APP"
	mv "dist/$APP" "/Applications/$APP"
	echo "Installed: /Applications/$APP"

	# Right after the old copy is closed, macOS sometimes answers with error
	# -600/-609; then we just try again a bit later.
	sleep 1
	open "/Applications/$APP" 2>/dev/null || { sleep 2; open "/Applications/$APP"; }
fi
