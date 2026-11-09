#!/bin/sh
# Собирает Disk Cleaner.app в папку dist/.
#
# Кроме обычной сборки Fyne делает две вещи:
#   1. добавляет в Info.plist текст, который macOS показывает, когда
#      приложение просит доступ к «Загрузкам» (NSDownloadsFolderUsageDescription);
#   2. заново подписывает приложение (ad-hoc, без сертификата разработчика):
#      после правки Info.plist старая подпись перестаёт сходиться.
#
# Запуск из корня проекта:
#   ./scripts/build.sh            — только собрать в dist/
#   ./scripts/build.sh --install  — собрать и установить в /Applications
#                                   (запущенная копия закроется, старая заменится)
set -e
cd "$(dirname "$0")/.."

FYNE="$(command -v fyne || echo "$HOME/go/bin/fyne")"
APP="Disk Cleaner.app"
PLIST="$APP/Contents/Info.plist"
TEXT="Disk Cleaner looks in Downloads for installers, app builds and archives you may no longer need. Nothing is removed without your confirmation, and files go to the Trash."

"$FYNE" package -os darwin -release

/usr/libexec/PlistBuddy -c "Delete :NSDownloadsFolderUsageDescription" "$PLIST" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Add :NSDownloadsFolderUsageDescription string $TEXT" "$PLIST"

codesign --force --deep --sign - "$APP"

mkdir -p dist
rm -rf "dist/$APP"
mv "$APP" dist/
echo "Готово: dist/$APP"

if [ "$1" = "--install" ]; then
	# Закрыть запущенную копию, чтобы заменить её, и поставить новую.
	# Переносим, а не копируем: иначе в dist/ остаётся вторая копия,
	# и Spotlight с Launchpad показывают два одинаковых Disk Cleaner.
	osascript -e 'quit app "Disk Cleaner"' 2>/dev/null || true
	rm -rf "/Applications/$APP"
	mv "dist/$APP" "/Applications/$APP"
	echo "Установлено: /Applications/$APP"
fi
