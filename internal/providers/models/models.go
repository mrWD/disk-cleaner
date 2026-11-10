// Package models is the provider for local AI models: the ones downloaded in LM Studio,
// Ollama, the Hugging Face cache, models that apps downloaded (for example
// Lingary or Argos Translate), and the llmstore storage.
//
// Models are the biggest files on a developer's Mac: one model weighs
// from hundreds of megabytes to tens of gigabytes. Deleting them is safe in the
// sense that you can download them again, but that takes long, so all
// categories have the "Caution" risk.
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

	// Reasons a model cannot be selected (texts are in translations).
	reasonLoaded     = "models.loaded"      // the model is loaded into the program's memory right now
	reasonAppRunning = "models.app_running" // the app that downloaded the model is running
	reasonLLMStore   = "models.llmstore"    // the file is managed by llmstore

	// minAppModel: anything smaller is not shown in the app model folders,
	// because those hold service files like state.json.
	minAppModel = core.Bytes(1_000_000)
)

// Provider looks for models in the home folder.
type Provider struct {
	home      string
	processes func(ctx context.Context) []string // list of processes; a fake in tests
}

// New is the provider for the current user.
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

	// Ollama models: the manifest and only those blobs that no other
	// model needs (identical layers of different models are stored once).
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
			continue // the model is already deleted, nothing to do
		}
		actions = append(actions, core.Action{RemovePath: m.manifest})
		for _, blob := range m.uniqueBlobs {
			actions = append(actions, core.Action{RemovePath: blob})
		}
	}

	return actions, nil
}

// ---------- LM Studio ----------

// lmStudio is LM Studio models: <downloads folder>/<publisher>/<model>.
// The downloads folder can be changed in LM Studio settings, so we read it from there.
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

// ollamaModel is one Ollama model.
type ollamaModel struct {
	name        string   // "qwen2.5:0.5b"
	manifest    string   // path to the manifest file
	modelBlob   string   // path to the weights file (it shows whether the model is loaded)
	uniqueBlobs []string // blobs that only this model has
	size        core.Bytes
}

// ollamaLayer is a layer from an Ollama manifest.
type ollamaLayer struct {
	Digest    string `json:"digest"` // "sha256:74a4…"
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
}

// ollamaModels reads Ollama manifests: each model is a file
// manifests/<registry>/<namespace>/<model>/<tag> with a list of layers.
// Layers are in blobs/sha256-<hash>, and different models can share one layer.
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
	refs := map[string]int{} // layer hash → how many models use it

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
			if refs[l.Digest] == 1 { // the layer is only in this model, so it can be deleted
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
// models from other registries keep the address: "hf.co/bartowski/model:Q4_K_M".
func ollamaName(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return rel
	}
	model, tag := strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
	model = strings.TrimPrefix(model, "registry.ollama.ai/library/")
	return model + ":" + tag
}

// ollamaItems builds the items for Ollama models.
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
		// A loaded model is held by the process "ollama runner --model <blob>".
		if m.modelBlob != "" && kit.AnyProcess(processes, m.modelBlob) {
			it.Status = core.StatusInUse
			it.Busy = &core.Reason{Key: reasonLoaded, Args: map[string]any{"App": "Ollama"}}
		}
		items = append(items, it)
	}
	return items
}

// ---------- Hugging Face ----------

// huggingFace is the cache of Hugging Face libraries (transformers,
// sentence-transformers, diffusers): hub/models--<organization>--<model>.
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

// ---------- App models ----------

// appModelDir is a folder with models that an app created.
type appModelDir struct {
	app string // "Lingary"
	dir string
}

// skipAppSupport: folders in Application Support that we do not look into:
// Claude data and Apple system folders (macOS locks some of them).
var skipAppSupport = map[string]bool{
	"Claude": true, "AddressBook": true, "CallHistoryDB": true, "CallHistoryTransactions": true,
	"FaceTime": true, "Knowledge": true, "MobileSync": true, "CloudDocs": true,
}

// appModelDirs finds where apps put models:
//   - ~/Library/Application Support/<App>/…/models (no deeper than three levels);
//   - ~/.local/share/argos-translate/packages: Argos translation models;
//   - ~/.cache/<name with "model">: model caches of individual programs.
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

// findModelsDirs finds folders named models inside root, no deeper than depth.
// We do not go further into a folder we found.
func findModelsDirs(root string, depth int) []string {
	if depth == 0 {
		return nil
	}

	var out []string
	for _, dir := range kit.Subfolders(root) {
		if strings.EqualFold(filepath.Base(dir), "models") { // case-insensitive
			out = append(out, dir)
			continue
		}
		out = append(out, findModelsDirs(dir, depth-1)...)
	}
	return out
}

// appModels gives one item per model (folder or file) in the app
// model folders.
func (p *Provider) appModels(ctx context.Context, processes []string) []core.Item {
	var items []core.Item

	for _, d := range p.appModelDirs() {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}

		var paths []string
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") { // .partial and other service files are skipped
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
				// A running app may hold a model open.
				it.Status = core.StatusInUse
				it.Busy = &core.Reason{Key: reasonAppRunning, Args: map[string]any{"App": d.app}}
			}
			items = append(items, it)
		}
	}

	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) }) // the big ones on top
	return items
}

// leafName is a readable name: if a folder has exactly one entry, we step
// into it, so that the model file itself is visible: "gguf/Qwen3-8B-Q4_K_M.gguf".
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

// llmStore is the models in the llmstore storage (~/Library/Models).
//
// llmstore keeps one copy of the weights, and LM Studio, Ollama and apps
// get APFS clones of it: the files look separate, but the blocks on disk
// are shared. Deleting a file in the storage frees almost nothing while
// the clones are alive, so we show the models but protect them: they have
// to be deleted through llmstore itself.
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
