package models

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
)

// write creates a file of a given size (and all folders leading to it).
func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = 1 // not zeros, so that APFS does not make the file "sparse"
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeText creates a text file.
func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const mb = 1_000_000

// fakeHome is a home folder that looks like this machine: models in all
// the places where LM Studio, Ollama, Hugging Face, Lingary,
// Argos Translate and llmstore put them.
func fakeHome(t *testing.T, processes ...string) *Provider {
	t.Helper()
	home := t.TempDir()
	p := &Provider{home: home, processes: func(context.Context) []string { return processes }}

	// LM Studio: the downloads folder is set in settings.json.
	lms := filepath.Join(home, "lmstudio-models")
	writeText(t, filepath.Join(home, ".lmstudio", "settings.json"), `{"downloadsFolder": "`+lms+`"}`)
	write(t, filepath.Join(lms, "lmstudio-community", "Qwen3-8B-GGUF", "Qwen3-8B-Q4_K_M.gguf"), 2*mb)

	// Ollama: two models share layer A (the weights), each has its own parameters layer.
	ollama := filepath.Join(home, ".ollama", "models")
	write(t, filepath.Join(ollama, "blobs", "sha256-aaa"), 3*mb)
	write(t, filepath.Join(ollama, "blobs", "sha256-bbb"), 10)
	write(t, filepath.Join(ollama, "blobs", "sha256-ccc"), 20)
	manifest := func(params string) string {
		return `{"config": {"digest": "sha256:cfg", "size": 1},
		         "layers": [{"digest": "sha256:aaa", "mediaType": "application/vnd.ollama.image.model", "size": 3000000},
		                    {"digest": "sha256:` + params + `", "mediaType": "application/vnd.ollama.image.params", "size": 10}]}`
	}
	writeText(t, filepath.Join(ollama, "manifests", "registry.ollama.ai", "library", "qwen2.5", "0.5b"), manifest("bbb"))
	writeText(t, filepath.Join(ollama, "manifests", "registry.ollama.ai", "library", "qwen2.5", "latest"), manifest("ccc"))

	// Hugging Face.
	write(t, filepath.Join(home, ".cache", "huggingface", "hub",
		"models--sentence-transformers--paraphrase-multilingual-MiniLM-L12-v2", "blobs", "x"), 2*mb)

	// Lingary: a model in the models folder, with a small state.json next to it.
	lingary := filepath.Join(home, "Library", "Application Support", "Lingary", "models")
	write(t, filepath.Join(lingary, "gguf", "Qwen3-8B-Q4_K_M.gguf"), 2*mb)
	writeText(t, filepath.Join(lingary, "state.json"), `{}`)

	// Argos Translate.
	write(t, filepath.Join(home, ".local", "share", "argos-translate", "packages", "translate-en_ru-1_9", "model", "model.bin"), 2*mb)

	// llmstore.
	sha := "74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db"
	write(t, filepath.Join(home, "Library", "Models", "blobs", "sha256", sha[:2], sha), mb)
	writeText(t, filepath.Join(home, "Library", "Models", "models.json"),
		`{"models": {"`+sha+`": {"name": "Qwen/Qwen2.5-0.5B-Instruct-GGUF", "filename": "q.gguf", "size": 491400032, "quant": "Q4_K_M"}}}`)

	return p
}

// byName returns an item by name (or an empty one).
func byName(items []core.Item, name string) core.Item {
	i := slices.IndexFunc(items, func(it core.Item) bool { return it.Name == name })
	if i < 0 {
		return core.Item{}
	}
	return items[i]
}

func TestScanFindsModelsEverywhere(t *testing.T) {
	items, err := fakeHome(t).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	providertest.CheckCategories(t, New(), items)

	want := map[string]string{
		"lmstudio-community/Qwen3-8B-GGUF": catLMStudio,
		"qwen2.5:0.5b":                     catOllama,
		"qwen2.5:latest":                   catOllama,
		"sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2": catHuggingFace,
		"Lingary · gguf/Qwen3-8B-Q4_K_M.gguf":                         catApps,
		"Argos Translate · translate-en_ru-1_9/model/model.bin":       catApps,
		"Qwen/Qwen2.5-0.5B-Instruct-GGUF · Q4_K_M":                    catLLMStore,
	}
	for name, cat := range want {
		if got := byName(items, name); got.CategoryID != cat {
			t.Errorf("%q: category = %q, want %q", name, got.CategoryID, cat)
		}
	}

	t.Run("small service files of apps are not shown", func(t *testing.T) {
		if byName(items, "Lingary · state.json").ID != "" {
			t.Error("state.json should be hidden")
		}
	})

	t.Run("llmstore models are shown but protected", func(t *testing.T) {
		ok, why := byName(items, "Qwen/Qwen2.5-0.5B-Instruct-GGUF · Q4_K_M").CanSelect()
		if ok || why.Key != reasonLLMStore {
			t.Errorf("llmstore model: ok = %v, why = %+v", ok, why)
		}
	})
}

func TestOllama(t *testing.T) {
	t.Run("deleting a model keeps layers that another model still needs", func(t *testing.T) {
		// Given: qwen2.5:0.5b and qwen2.5:latest share the weights (sha256-aaa)
		p := fakeHome(t)
		items, _ := p.Scan(context.Background())
		small := byName(items, "qwen2.5:0.5b")

		// Then: the size is only its own layer, without the shared weights
		if small.Size != 10 {
			t.Errorf("size = %s, want 10 B (shared weights are not freed)", small.Size)
		}

		// When: the deletion plan
		actions, _ := p.Plan([]core.Item{small})

		// Then: the manifest and its own layer, but not the shared weights
		var paths []string
		for _, a := range actions {
			paths = append(paths, filepath.Base(a.RemovePath))
		}
		if !slices.Equal(paths, []string{"0.5b", "sha256-bbb"}) {
			t.Errorf("plan removes %v, want [0.5b sha256-bbb]", paths)
		}
	})

	t.Run("a model loaded into memory cannot be selected", func(t *testing.T) {
		// Given: an ollama runner process holds the weights
		p := fakeHome(t)
		blob := filepath.Join(p.home, ".ollama", "models", "blobs", "sha256-aaa")
		p.processes = func(context.Context) []string {
			return []string{"/opt/homebrew/bin/ollama runner --model " + blob + " --port 54321"}
		}

		items, _ := p.Scan(context.Background())

		ok, why := byName(items, "qwen2.5:0.5b").CanSelect()
		if ok || why.Key != reasonLoaded {
			t.Errorf("loaded model: ok = %v, why = %+v", ok, why)
		}
	})
}

func TestAppModels(t *testing.T) {
	t.Run("models of a running app cannot be selected", func(t *testing.T) {
		p := fakeHome(t, "/Applications/Lingary.app/Contents/MacOS/Lingary")

		items, _ := p.Scan(context.Background())

		ok, why := byName(items, "Lingary · gguf/Qwen3-8B-Q4_K_M.gguf").CanSelect()
		if ok || why.Key != reasonAppRunning || why.Args["App"] != "Lingary" {
			t.Errorf("Lingary model: ok = %v, why = %+v", ok, why)
		}
		// This does not affect the models of other apps.
		if ok, _ := byName(items, "Argos Translate · translate-en_ru-1_9/model/model.bin").CanSelect(); !ok {
			t.Error("Argos model should stay selectable")
		}
	})
}

func TestNoModelsMeansNothingToShow(t *testing.T) {
	p := &Provider{home: t.TempDir(), processes: func(context.Context) []string { return nil }}
	items, err := p.Scan(context.Background())
	if err != nil || len(items) != 0 {
		t.Errorf("items = %v, err = %v", items, err)
	}
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(), reasonLoaded, reasonAppRunning, reasonLLMStore)
}
