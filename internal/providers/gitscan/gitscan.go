// Package gitscan — общий поиск git-репозиториев и папок-«артефактов»
// (node_modules, Pods, build…) в них. Им пользуются два провайдера:
// worktrees (дополнительные рабочие копии) и projects (основные
// репозитории). Провайдеры не импортируют друг друга, а этот хелпер — могут.
//
// Главное правило для обоих: удалять можно ТОЛЬКО то, что git игнорирует
// (git check-ignore). Отслеживаемые файлы не трогаются никогда, даже если
// папка называется build.
package gitscan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"disk-cleaner/internal/shell"
)

// Git — путь к git. На Mac с Xcode он есть всегда.
const Git = "/usr/bin/git"

// MinSize — артефакты меньше этого размера не показываем: десятки
// крошечных папок только засоряют список, а места почти не освобождают.
const MinSize = 1_000_000

// artifactNames — папки, которые обычно создаются установкой или сборкой.
var artifactNames = map[string]bool{
	"node_modules": true, "Pods": true, "build": true, ".gradle": true,
	".cxx": true, ".expo": true, ".next": true, ".turbo": true,
	"dist": true, "DerivedData": true, ".venv": true, "venv": true,
	"target": true, "release": true, "out": true, // release/, out/ — выход electron-builder и других
}

// buildExts — расширения готовых сборок приложений: архивы Xcode,
// пакеты для iOS и Android, установщики macOS и Windows.
var buildExts = []string{
	".xcarchive", ".ipa", ".app", ".apk", ".aab", ".dmg", ".pkg",
	".exe", ".msi", ".appimage", ".deb",
}

// isBuild — похоже ли имя на готовую сборку.
func isBuild(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range buildExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// skipWhileSearching — куда не заходим, когда ищем репозитории:
// там их не бывает, а файлов — миллионы.
var skipWhileSearching = map[string]bool{
	"node_modules": true, "Pods": true, "build": true, ".gradle": true,
	"DerivedData": true, ".venv": true, "venv": true, "target": true,
	"dist": true, ".next": true, "vendor": true, ".git": true, "Library": true,
}

const (
	maxSearchDepth   = 6 // насколько глубоко от корня искать репозитории
	maxArtifactDepth = 4 // насколько глубоко внутри репозитория искать артефакты
)

// Kind — вид репозитория.
type Kind int

const (
	Main     Kind = iota // основной: .git — это папка
	Worktree             // дополнительный worktree: .git — файл "gitdir: …/worktrees/…"
)

// Repo — найденный репозиторий.
type Repo struct {
	Path string
	Kind Kind
}

// KindOf — является ли dir репозиторием, и каким. ok = false — не репозиторий
// (или подмодуль: у него .git тоже файл, но путь ведёт в .git/modules).
func KindOf(dir string) (Kind, bool) {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return 0, false
	}
	if info.IsDir() {
		return Main, true
	}

	data, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(string(data))
	if strings.HasPrefix(line, "gitdir:") && strings.Contains(line, "/worktrees/") {
		return Worktree, true
	}
	return 0, false
}

// FindRepos ищет репозитории внутри root. В основной репозиторий заходит
// дальше (внутри бывают .claude/worktrees и вложенные репозитории),
// в worktree — нет.
func FindRepos(root string) []Repo {
	var found []Repo

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if rel != "." && (skipWhileSearching[d.Name()] || strings.Count(rel, "/") >= maxSearchDepth) {
			return fs.SkipDir
		}

		kind, ok := KindOf(path)
		if !ok {
			return nil
		}
		found = append(found, Repo{Path: path, Kind: kind})
		if kind == Worktree {
			return fs.SkipDir // внутри worktree других не ищем
		}
		return nil
	})

	return found
}

// FindArtifacts — пути (относительно repo) папок с именами из artifactNames.
// Внутрь найденной не заходит: node_modules внутри node_modules не нужны.
// Во вложенные репозитории и worktree тоже не заходит: у них свой владелец.
func FindArtifacts(repo string) []string {
	var found []string

	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == repo {
			return nil
		}

		rel, _ := filepath.Rel(repo, path)
		switch {
		case d.Name() == ".git":
			return fs.SkipDir
		case artifactNames[d.Name()]:
			found = append(found, rel)
			return fs.SkipDir
		case strings.Count(rel, "/") >= maxArtifactDepth-1:
			return fs.SkipDir
		}
		if _, nested := KindOf(path); nested {
			return fs.SkipDir // другой репозиторий — не наш
		}
		return nil
	})

	return found
}

// FindBuilds — готовые сборки (.xcarchive, .apk, .dmg, .app…), которые
// лежат в репозитории вне папок-артефактов: например архив, сохранённый
// прямо в корень проекта. Пути — относительно repo. Внутрь найденной
// сборки (.app и .xcarchive — это папки) не заходит.
func FindBuilds(repo string) []string {
	var found []string

	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == repo {
			return nil
		}

		rel, _ := filepath.Rel(repo, path)
		name := d.Name()

		if isBuild(name) {
			found = append(found, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}

		switch {
		case name == ".git", artifactNames[name]:
			return fs.SkipDir // папки сборки и так показываются целиком
		case strings.Count(rel, "/") >= maxArtifactDepth-1:
			return fs.SkipDir
		}
		if _, nested := KindOf(path); nested {
			return fs.SkipDir
		}
		return nil
	})

	return found
}

// Untracked оставляет из candidates то, что git НЕ отслеживает (новые
// или игнорируемые файлы). git ls-files печатает отслеживаемые файлы;
// для папки — файлы внутри неё. Ошибка — считаем всё отслеживаемым
// и не предлагаем ничего: так безопаснее.
func Untracked(ctx context.Context, run shell.Runner, repo string, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}

	args := append([]string{"-C", repo, "ls-files", "--"}, candidates...)
	out, err := run(ctx, Git, args...)
	if err != nil {
		return nil
	}

	tracked := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, c := range candidates {
		isTracked := slices.ContainsFunc(tracked, func(t string) bool {
			return t == c || strings.HasPrefix(t, c+"/")
		})
		if !isTracked {
			result = append(result, c)
		}
	}
	return result
}

// Ignored оставляет из candidates только то, что git игнорирует.
// git check-ignore печатает игнорируемые пути и завершается с кодом 1,
// если таких нет. Любая ошибка — значит, не удаляем ничего: так безопаснее.
func Ignored(ctx context.Context, run shell.Runner, repo string, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}

	args := append([]string{"-C", repo, "check-ignore", "--"}, candidates...)
	out, _ := run(ctx, Git, args...)

	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSuffix(strings.TrimSpace(line), "/")
		if slices.Contains(candidates, line) { // только то, о чём спрашивали
			result = append(result, line)
		}
	}
	return result
}

// Branch — имя ветки ("detached", если HEAD не на ветке; "" — не удалось узнать).
func Branch(ctx context.Context, run shell.Runner, repo string) string {
	out, err := run(ctx, Git, "-C", repo, "rev-parse", "--abbrev-ref", "HEAD")
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		return ""
	}
	if name == "HEAD" {
		return "detached"
	}
	return name
}

// LastCommit — когда был последний коммит (нулевое время — коммитов нет).
func LastCommit(ctx context.Context, run shell.Runner, repo string) time.Time {
	out, err := run(ctx, Git, "-C", repo, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
