#!/bin/sh
# Собирает Disk Cleaner.app и, с флагом --install, ставит его в /Applications.
#
# Сначала сам ставит то, чего не хватает для сборки:
#   1. Xcode Command Line Tools — компилятор C: Fyne собирает часть себя из C;
#   2. Go — через Homebrew, а если его нет, официальным архивом с go.dev
#      в ~/sdk (пароль администратора не нужен);
#   3. fyne — программа, которая упаковывает собранный файл в .app.
#
# Потом:
#   4. собирает приложение (fyne package);
#   5. добавляет в Info.plist текст, который macOS показывает, когда
#      приложение просит доступ к «Загрузкам» (NSDownloadsFolderUsageDescription);
#   6. заново подписывает приложение (ad-hoc, без сертификата разработчика):
#      после правки Info.plist старая подпись перестаёт сходиться.
#
# Запуск из любой папки:
#   ./scripts/build.sh            — только собрать в dist/
#   ./scripts/build.sh --install  — собрать, поставить в /Applications и открыть
#                                   (запущенная копия закроется, старая заменится)
set -e
cd "$(dirname "$0")/.."

APP="Disk Cleaner.app"
PLIST="$APP/Contents/Info.plist"
TEXT="Disk Cleaner looks in Downloads for installers, app builds and archives you may no longer need. Nothing is removed without your confirmation, and files go to the Trash."
FYNE_VERSION="v1.7.4" # версия, с которой проект проверен

# Нужная версия Go — из go.mod (строка "go 1.27.3").
GO_VERSION="$(awk '$1 == "go" { print $2 }' go.mod)"

# Где обычно лежат Homebrew, Go и программы, поставленные через go install.
# В PATH их может не быть, например, если Homebrew поставили, но не
# добавили в ~/.zprofile.
PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin:$HOME/sdk/go$GO_VERSION/bin"
export PATH

# Если в окружении стоит GOTOOLCHAIN=local, Go не скачает нужную версию сам.
export GOTOOLCHAIN=auto

step() { printf '\n==> %s\n' "$1"; }

# --- 1. Xcode Command Line Tools ---
if ! xcode-select -p >/dev/null 2>&1; then
	step "Installing Xcode Command Line Tools (a macOS window will open, click Install)"
	xcode-select --install 2>/dev/null || true
	echo "Waiting for the installation to finish. If you closed that window, press Ctrl+C and run this script again."
	# Установка идёт в отдельном окне macOS — ждём, пока она закончится.
	until xcode-select -p >/dev/null 2>&1; do sleep 5; done
fi

# --- 2. Go ---

# go_ok — есть ли Go, который справится сам. С версии 1.21 Go, увидев
# в go.mod версию новее себя, сам скачивает нужную (GOTOOLCHAIN=auto).
# Более старый так не умеет — его считаем как отсутствующий.
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
		# Homebrew нет — берём официальный архив Go и кладём его в ~/sdk.
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

# --- 4–6. Сборка ---
step "Building $APP (the first build takes a few minutes)"
"$FYNE" package -os darwin -release

/usr/libexec/PlistBuddy -c "Delete :NSDownloadsFolderUsageDescription" "$PLIST" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Add :NSDownloadsFolderUsageDescription string $TEXT" "$PLIST"

codesign --force --deep --sign - "$APP"

mkdir -p dist
rm -rf "dist/$APP"
mv "$APP" dist/
echo "Built: dist/$APP"

if [ "$1" = "--install" ]; then
	# Закрыть запущенную копию, чтобы заменить её, и поставить новую.
	# Переносим, а не копируем: иначе в dist/ остаётся вторая копия,
	# и Spotlight с Launchpad показывают два одинаковых Disk Cleaner.
	step "Installing to /Applications"
	osascript -e 'quit app "Disk Cleaner"' 2>/dev/null || true
	rm -rf "/Applications/$APP"
	mv "dist/$APP" "/Applications/$APP"
	echo "Installed: /Applications/$APP"

	# Сразу после закрытия старой копии macOS иногда отвечает ошибкой
	# -600/-609 — тогда просто пробуем ещё раз чуть позже.
	sleep 1
	open "/Applications/$APP" 2>/dev/null || { sleep 2; open "/Applications/$APP"; }
fi
