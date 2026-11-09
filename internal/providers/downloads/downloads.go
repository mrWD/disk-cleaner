// Package downloads — провайдер папки «Загрузки»: установщики (.dmg, .pkg),
// сборки приложений (.aab, .apk, .ipa, .xcarchive, .app) и архивы
// (.zip, .xip, .tar.gz…).
//
// Это файлы пользователя, поэтому правила строже, чем у кэшей:
//   - ничего не удаляется навсегда — файлы переносятся в Корзину, и любую
//     ошибку можно исправить, вернув файл;
//   - смонтированный образ диска (.dmg, открытый в Finder) выбрать нельзя;
//   - чтобы читать «Загрузки», macOS один раз спросит разрешение. Если его
//     не дать, раздел спокойно скажет, где это разрешить.
package downloads

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/shell"
)

//go:embed translations
var translations embed.FS

const (
	catInstallers = "downloads.installers"
	catBuilds     = "downloads.builds"
	catArchives   = "downloads.archives"

	reasonNoAccess = "downloads.no_access" // macOS не дала доступ к «Загрузкам»
	reasonMounted  = "downloads.mounted"   // образ диска сейчас подключён

	minSize = core.Bytes(1_000_000) // мелочь не показываем
)

// kinds — какие окончания имён в какую категорию попадают.
// Для архивов важно проверять составные окончания (.tar.gz) раньше простых.
var kinds = []struct {
	category string
	exts     []string
}{
	{catInstallers, []string{".dmg", ".pkg", ".mpkg", ".iso"}},
	{catBuilds, []string{".aab", ".apk", ".ipa", ".xcarchive", ".app", ".appimage", ".exe", ".msi"}},
	{catArchives, []string{".tar.gz", ".tar.xz", ".tar.bz2", ".zip", ".xip", ".tgz", ".tar", ".gz", ".7z", ".rar"}},
}

// Provider ищет файлы в «Загрузках».
type Provider struct {
	dir string       // папка «Загрузки»
	run shell.Runner // запуск hdiutil; в тестах — фейк
}

// New — провайдер для «Загрузок» текущего пользователя.
func New() *Provider {
	return &Provider{dir: filepath.Join(kit.Home(), "Downloads"), run: shell.Run}
}

func (p *Provider) ID() string { return "downloads" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catInstallers, Risk: core.RiskSafe},  // программа уже установлена или скачивается снова
		{ID: catBuilds, Risk: core.RiskCaution},   // сборку могут ещё не загрузить в магазин
		{ID: catArchives, Risk: core.RiskCaution}, // внутри может быть что-то единственное
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	entries, err := os.ReadDir(p.dir)
	if errors.Is(err, fs.ErrPermission) {
		// macOS не дала доступ: это не сбой, а вопрос разрешения.
		return nil, core.Unavailable(reasonNoAccess)
	}
	if err != nil {
		return nil, err
	}

	mounted := p.mountedImages(ctx)

	var items []core.Item
	var paths []string
	for _, e := range entries {
		cat := categoryOf(e.Name())
		if cat == "" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Папкой бывают только сборки .app и .xcarchive; папка «my.zip» — не архив.
		if e.IsDir() && cat != catBuilds {
			continue
		}

		path := filepath.Join(p.dir, e.Name())
		it := core.Item{ID: path, CategoryID: cat, Name: e.Name()}
		if info, err := e.Info(); err == nil {
			it.Detail = info.ModTime().Format("2006-01-02") // когда скачан или изменён
		}
		if mounted[path] {
			it.Status = core.StatusInUse
			it.Busy = &core.Reason{Key: reasonMounted}
		}
		items = append(items, it)
		paths = append(paths, path)
	}

	sizes := diskutil.SizeAll(ctx, paths, kit.Workers())
	for i := range items {
		items[i].Size = sizes[i]
	}
	items = slices.DeleteFunc(items, func(it core.Item) bool { return it.Size < minSize })
	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })

	return items, nil
}

// Plan — в Корзину, а не навсегда.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	actions := make([]core.Action, 0, len(items))
	for _, it := range items {
		actions = append(actions, core.Action{RemovePath: it.ID, Trash: true})
	}
	return actions, nil
}

// categoryOf — категория по окончанию имени ("" — не наш файл).
func categoryOf(name string) string {
	lower := strings.ToLower(name)
	for _, k := range kinds {
		for _, ext := range k.exts {
			if strings.HasSuffix(lower, ext) {
				return k.category
			}
		}
	}
	return ""
}

// mountedImages — какие образы дисков сейчас подключены. hdiutil info
// печатает для каждого строку "image-path : /Users/…/file.dmg".
func (p *Provider) mountedImages(ctx context.Context) map[string]bool {
	out, err := p.run(ctx, "/usr/bin/hdiutil", "info")
	if err != nil {
		return nil
	}

	mounted := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(key) == "image-path" {
			mounted[strings.TrimSpace(value)] = true
		}
	}
	return mounted
}
