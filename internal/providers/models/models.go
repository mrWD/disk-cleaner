// Package models — провайдер локальных AI-моделей: скачанных в LM Studio,
// Ollama, кэш Hugging Face, модели, которые скачали приложения (например,
// Lingary или Argos Translate), и хранилище llmstore.
//
// Модели — самые большие файлы на Mac разработчика: одна модель весит
// от сотен мегабайт до десятков гигабайт. Удалять их безопасно в том
// смысле, что их можно скачать снова, но это долго — поэтому риск у всех
// категорий «Caution».
package models

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/kit"
)

//go:embed translations
var translations embed.FS

const (
	catLMStudio    = "models.lmstudio"
	catOllama      = "models.ollama"
	catHuggingFace = "models.huggingface"
	catApps        = "models.apps"
	catLLMStore    = "models.llmstore"

	// Причины, почему модель нельзя выбрать (тексты в translations).
	reasonLoaded     = "models.loaded"      // модель сейчас загружена в память программы
	reasonAppRunning = "models.app_running" // приложение, скачавшее модель, запущено
	reasonLLMStore   = "models.llmstore"    // файлом управляет llmstore

	// minAppModel — меньше этого в папках моделей приложений не показываем:
	// там лежат служебные файлы вроде state.json.
	minAppModel = core.Bytes(1_000_000)
)

// Provider ищет модели в домашней папке.
type Provider struct {
	home      string
	processes func(ctx context.Context) []string // список процессов; в тестах — фейк
}

// New — провайдер для текущего пользователя.
func New() *Provider {
	return &Provider{home: kit.Home(), processes: kit.Processes}
}

func (p *Provider) ID() string { return "models" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catLMStudio, Risk: core.RiskCaution},
		{ID: catOllama, Risk: core.RiskCaution},
		{ID: catHuggingFace, Risk: core.RiskCaution},
		{ID: catApps, Risk: core.RiskCaution},
		{ID: catLLMStore, Risk: core.RiskCaution},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	processes := p.processes(ctx)

	var items []core.Item
	items = append(items, p.lmStudio(ctx)...)
	items = append(items, p.ollamaItems(processes)...)
	items = append(items, p.huggingFace(ctx)...)
	items = append(items, p.appModels(ctx, processes)...)
	items = append(items, p.llmStore()...)

	return items, nil
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	var actions []core.Action

	// Модели Ollama: манифест и только те блобы, которые не нужны другим
	// моделям (одинаковые слои у разных моделей хранятся один раз).
	ollama := map[string]ollamaModel{}
	for _, m := range p.ollamaModels() {
		ollama[m.manifest] = m
	}

	for _, it := range items {
		if it.CategoryID != catOllama {
			actions = append(actions, core.Action{RemovePath: it.ID})
			continue
		}
		m, ok := ollama[it.ID]
		if !ok {
			continue // модель уже удалили — делать нечего
		}
		actions = append(actions, core.Action{RemovePath: m.manifest})
		for _, blob := range m.uniqueBlobs {
			actions = append(actions, core.Action{RemovePath: blob})
		}
	}

	return actions, nil
}

// ---------- LM Studio ----------

// lmStudio — модели LM Studio: <папка загрузок>/<издатель>/<модель>.
// Папку загрузок можно поменять в настройках LM Studio — читаем её оттуда.
func (p *Provider) lmStudio(ctx context.Context) []core.Item {
	dir := filepath.Join(p.home, ".lmstudio", "models")

	var settings struct {
		DownloadsFolder string `json:"downloadsFolder"`
	}
	if data, err := os.ReadFile(filepath.Join(p.home, ".lmstudio", "settings.json")); err == nil {
		if json.Unmarshal(data, &settings) == nil && settings.DownloadsFolder != "" {
			dir = settings.DownloadsFolder
		}
	}

	var paths []string
	for _, publisher := range kit.Subfolders(dir) {
		paths = append(paths, kit.Subfolders(publisher)...)
	}

	return kit.PathItems(ctx, catLMStudio, paths, func(path string) string {
		rel, _ := filepath.Rel(dir, path) // "lmstudio-community/Qwen3-8B-GGUF"
		return rel
	})
}

// ---------- Ollama ----------

// ollamaModel — одна модель Ollama.
type ollamaModel struct {
	name        string   // "qwen2.5:0.5b"
	manifest    string   // путь к файлу-манифесту
	modelBlob   string   // путь к файлу с весами (по нему видно, загружена ли модель)
	uniqueBlobs []string // блобы, которые есть только у этой модели
	size        core.Bytes
}

// ollamaLayer — слой из манифеста Ollama.
type ollamaLayer struct {
	Digest    string `json:"digest"` // "sha256:74a4…"
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
}

// ollamaModels читает манифесты Ollama: каждая модель — файл
// manifests/<реестр>/<пространство>/<модель>/<тег> со списком слоёв.
// Слои лежат в blobs/sha256-<хэш>, и разные модели могут делить один слой.
func (p *Provider) ollamaModels() []ollamaModel {
	root := filepath.Join(p.home, ".ollama", "models")
	manifests := filepath.Join(root, "manifests")
	blobPath := func(digest string) string {
		return filepath.Join(root, "blobs", strings.Replace(digest, ":", "-", 1))
	}

	type parsed struct {
		path   string
		name   string
		layers []ollamaLayer
	}
	var all []parsed
	refs := map[string]int{} // хэш слоя → сколько моделей его используют

	_ = filepath.WalkDir(manifests, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var m struct {
			Config ollamaLayer   `json:"config"`
			Layers []ollamaLayer `json:"layers"`
		}
		if json.Unmarshal(data, &m) != nil {
			return nil
		}

		layers := append([]ollamaLayer{m.Config}, m.Layers...)
		for _, l := range layers {
			refs[l.Digest]++
		}
		rel, _ := filepath.Rel(manifests, path)
		all = append(all, parsed{path: path, name: ollamaName(rel), layers: layers})
		return nil
	})

	var out []ollamaModel
	for _, m := range all {
		om := ollamaModel{name: m.name, manifest: m.path}
		for _, l := range m.layers {
			if l.MediaType == "application/vnd.ollama.image.model" {
				om.modelBlob = blobPath(l.Digest)
			}
			if refs[l.Digest] == 1 { // слой только у этой модели — его можно удалить
				if _, err := os.Stat(blobPath(l.Digest)); err == nil {
					om.uniqueBlobs = append(om.uniqueBlobs, blobPath(l.Digest))
					om.size += core.Bytes(l.Size)
				}
			}
		}
		out = append(out, om)
	}
	return out
}

// ollamaName: "registry.ollama.ai/library/qwen2.5/0.5b" → "qwen2.5:0.5b",
// модели из других реестров — с адресом: "hf.co/bartowski/model:Q4_K_M".
func ollamaName(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return rel
	}
	model, tag := strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
	model = strings.TrimPrefix(model, "registry.ollama.ai/library/")
	return model + ":" + tag
}

// ollamaItems — объекты для моделей Ollama.
func (p *Provider) ollamaItems(processes []string) []core.Item {
	var items []core.Item
	for _, m := range p.ollamaModels() {
		it := core.Item{
			ID:         m.manifest,
			CategoryID: catOllama,
			Name:       m.name,
			Detail:     kit.Tilde(filepath.Join(p.home, ".ollama", "models")),
			Size:       m.size,
		}
		// Загруженную модель держит процесс «ollama runner --model <блоб>».
		if m.modelBlob != "" && kit.AnyProcess(processes, m.modelBlob) {
			it.Status = core.StatusInUse
			it.Busy = &core.Reason{Key: reasonLoaded, Args: map[string]any{"App": "Ollama"}}
		}
		items = append(items, it)
	}
	return items
}

// ---------- Hugging Face ----------

// huggingFace — кэш библиотек Hugging Face (transformers,
// sentence-transformers, diffusers): hub/models--<организация>--<модель>.
func (p *Provider) huggingFace(ctx context.Context) []core.Item {
	hub := filepath.Join(p.home, ".cache", "huggingface", "hub")

	var paths []string
	for _, dir := range kit.Subfolders(hub) {
		base := filepath.Base(dir)
		if strings.HasPrefix(base, "models--") || strings.HasPrefix(base, "datasets--") || strings.HasPrefix(base, "spaces--") {
			paths = append(paths, dir)
		}
	}

	return kit.PathItems(ctx, catHuggingFace, paths, func(path string) string {
		// "models--sentence-transformers--paraphrase-…" → "sentence-transformers/paraphrase-…"
		base := filepath.Base(path)
		kind, name, _ := strings.Cut(base, "--")
		name = strings.ReplaceAll(name, "--", "/")
		if kind != "models" {
			return kind + ": " + name
		}
		return name
	})
}

// ---------- Модели приложений ----------

// appModelDir — папка с моделями, которую создало приложение.
type appModelDir struct {
	app string // "Lingary"
	dir string
}

// skipAppSupport — папки в Application Support, куда не заглядываем:
// данные Claude, системные папки Apple (часть из них macOS закрывает).
var skipAppSupport = map[string]bool{
	"Claude": true, "AddressBook": true, "CallHistoryDB": true, "CallHistoryTransactions": true,
	"FaceTime": true, "Knowledge": true, "MobileSync": true, "CloudDocs": true,
}

// appModelDirs ищет, куда приложения складывают модели:
//   - ~/Library/Application Support/<App>/…/models (не глубже трёх уровней);
//   - ~/.local/share/argos-translate/packages — модели перевода Argos;
//   - ~/.cache/<имя с «model»> — кэши моделей своих программ.
func (p *Provider) appModelDirs() []appModelDir {
	var out []appModelDir

	support := filepath.Join(p.home, "Library", "Application Support")
	for _, appDir := range kit.Subfolders(support) {
		app := filepath.Base(appDir)
		if skipAppSupport[app] || strings.HasPrefix(app, "com.apple.") {
			continue
		}
		for _, dir := range findModelsDirs(appDir, 3) {
			out = append(out, appModelDir{app: app, dir: dir})
		}
	}

	argos := filepath.Join(p.home, ".local", "share", "argos-translate", "packages")
	if info, err := os.Stat(argos); err == nil && info.IsDir() {
		out = append(out, appModelDir{app: "Argos Translate", dir: argos})
	}

	for _, dir := range kit.Subfolders(filepath.Join(p.home, ".cache")) {
		if base := filepath.Base(dir); strings.Contains(strings.ToLower(base), "model") {
			out = append(out, appModelDir{app: base, dir: dir})
		}
	}

	return out
}

// findModelsDirs — папки с именем models внутри root, не глубже depth.
// Найденную папку не обходим дальше.
func findModelsDirs(root string, depth int) []string {
	if depth == 0 {
		return nil
	}

	var out []string
	for _, dir := range kit.Subfolders(root) {
		if strings.EqualFold(filepath.Base(dir), "models") { // без учёта регистра
			out = append(out, dir)
			continue
		}
		out = append(out, findModelsDirs(dir, depth-1)...)
	}
	return out
}

// appModels — по объекту на каждую модель (папку или файл) в папках
// моделей приложений.
func (p *Provider) appModels(ctx context.Context, processes []string) []core.Item {
	var items []core.Item

	for _, d := range p.appModelDirs() {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}

		var paths []string
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") { // .partial и прочие служебные — мимо
				paths = append(paths, filepath.Join(d.dir, e.Name()))
			}
		}

		sizes := diskutil.SizeAll(ctx, paths, kit.Workers())
		running := kit.AnyProcess(processes, "/"+d.app+".app/")

		for i, path := range paths {
			if sizes[i] < minAppModel {
				continue
			}
			it := core.Item{
				ID:         path,
				CategoryID: catApps,
				Name:       d.app + " · " + leafName(path),
				Detail:     kit.Tilde(path),
				Size:       sizes[i],
			}
			if running {
				// Работающее приложение может держать модель открытой.
				it.Status = core.StatusInUse
				it.Busy = &core.Reason{Key: reasonAppRunning, Args: map[string]any{"App": d.app}}
			}
			items = append(items, it)
		}
	}

	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) }) // большие сверху
	return items
}

// leafName — понятное имя: если в папке ровно один элемент, спускаемся
// к нему, чтобы было видно сам файл модели: "gguf/Qwen3-8B-Q4_K_M.gguf".
func leafName(path string) string {
	name := filepath.Base(path)
	for range 3 {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 1 {
			break
		}
		path = filepath.Join(path, entries[0].Name())
		name += "/" + entries[0].Name()
	}
	return name
}

// ---------- llmstore ----------

// llmStore — модели в хранилище llmstore (~/Library/Models).
//
// llmstore хранит одну копию весов, а LM Studio, Ollama и приложения
// получают её клоны APFS: файлы выглядят отдельными, но блоки на диске
// общие. Удаление файла в хранилище почти ничего не освобождает, пока
// живы клоны, — поэтому показываем модели, но защищаем: удалять их
// нужно через сам llmstore.
func (p *Provider) llmStore() []core.Item {
	root := filepath.Join(p.home, "Library", "Models")

	data, err := os.ReadFile(filepath.Join(root, "models.json"))
	if err != nil {
		return nil
	}
	var store struct {
		Models map[string]struct {
			Name     string `json:"name"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
			Quant    string `json:"quant"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &store) != nil {
		return nil
	}

	var items []core.Item
	for sha, m := range store.Models {
		if len(sha) < 2 {
			continue
		}
		blob := filepath.Join(root, "blobs", "sha256", sha[:2], sha)
		if _, err := os.Stat(blob); err != nil {
			continue
		}

		name := m.Name
		if m.Quant != "" {
			name += " · " + m.Quant
		}
		items = append(items, core.Item{
			ID:         blob,
			CategoryID: catLLMStore,
			Name:       name,
			Detail:     m.Filename,
			Size:       core.Bytes(m.Size),
			Protected:  &core.Reason{Key: reasonLLMStore},
		})
	}

	slices.SortFunc(items, func(a, b core.Item) int { return strings.Compare(a.Name, b.Name) })
	return items
}
