// Package docker — провайдер Docker: контейнеры, образы, тома и кэш сборки.
//
// Всё узнаём одной командой: docker system df -v --format '{{json .}}'.
// Она отдаёт JSON сразу по всем четырём видам объектов, включая то,
// сколько контейнеров использует каждый образ и каждый том.
package docker

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/shell"
)

//go:embed translations
var translations embed.FS

const (
	catContainers = "docker.containers"
	catImages     = "docker.images"
	catVolumes    = "docker.volumes"
	catBuildCache = "docker.build_cache"

	// Ключи причин, почему Docker сейчас недоступен (тексты в translations).
	reasonNotInstalled = "docker.not_installed"
	reasonNotRunning   = "docker.not_running"

	// Кем занят объект.
	reasonImageUsed      = "docker.image_used"
	reasonVolumeAttached = "docker.volume_attached"
)

// Provider спрашивает Docker через его командную строку.
type Provider struct {
	run  shell.Runner                     // запуск команд; в тестах — фейк
	look func(name string) (string, bool) // поиск программы; в тестах — фейк
}

// New — провайдер, который запускает настоящий docker.
func New() *Provider {
	return &Provider{run: shell.Run, look: shell.LookPath}
}

func (p *Provider) ID() string { return "docker" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		// В остановленном контейнере могут остаться данные, записанные внутрь него.
		{ID: catContainers, Risk: core.RiskCaution},
		{ID: catImages, Risk: core.RiskSafe},
		// В томах часто живут базы данных.
		{ID: catVolumes, Risk: core.RiskCaution},
		{ID: catBuildCache, Risk: core.RiskSafe},
	}
}

// text — строка в JSON Docker. Некоторые поля Docker отдаёт то строкой
// ("0"), то числом (0); этот тип принимает оба варианта.
type text string

// UnmarshalJSON вызывается пакетом encoding/json для полей типа text.
func (t *text) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = text(s)
		return nil
	}

	s := strings.TrimSpace(string(b))
	if s == "null" {
		s = ""
	}
	*t = text(s)
	return nil
}

// dfOutput — нужная нам часть ответа docker system df -v.
// Имена полей совпадают с ключами JSON, поэтому теги `json:"..."` не нужны.
type dfOutput struct {
	Containers []struct {
		ID, Names, Image, State, Status, Size text
	}
	Images []struct {
		ID, Repository, Tag, Size, UniqueSize, Containers, CreatedSince text
	}
	Volumes []struct {
		Name, Links, Size, Labels text
	}
	BuildCache []struct {
		ID, InUse, Size text
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	docker, ok := p.look("docker")
	if !ok {
		return nil, core.Unavailable(reasonNotInstalled)
	}

	// Если Docker «завис», не ждём вечно.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	out, err := p.run(ctx, docker, "system", "df", "-v", "--format", "{{json .}}")
	if err != nil {
		if isNotRunning(err) {
			return nil, core.Unavailable(reasonNotRunning)
		}
		return nil, err
	}

	var df dfOutput
	if err := json.Unmarshal(out, &df); err != nil {
		return nil, err
	}

	return toItems(df), nil
}

// isNotRunning — по тексту ошибки понимаем, что Docker просто выключен.
func isNotRunning(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "Is the docker daemon running") ||
		strings.Contains(msg, "docker.sock")
}

// toItems превращает ответ Docker в объекты. Отдельная функция без
// запуска команд — её удобно тестировать на записанном JSON.
func toItems(df dfOutput) []core.Item {
	var items []core.Item

	for _, c := range df.Containers {
		it := core.Item{
			ID:         string(c.ID),
			CategoryID: catContainers,
			Name:       string(c.Names) + " · " + string(c.Image),
			Detail:     string(c.Status),
			// "2.5kB (virtual 120MB)": освободится только первая часть,
			// «виртуальная» — это образ, он удаляется отдельно.
			Size: parseSize(strings.Fields(string(c.Size))),
		}
		switch c.State {
		case "running", "paused", "restarting":
			it.Status = core.StatusRunning
		}
		items = append(items, it)
	}

	for _, img := range df.Images {
		name := string(img.Repository) + ":" + string(img.Tag)
		if img.Repository == "" || img.Repository == "<none>" {
			name = "untagged · " + shortID(string(img.ID))
		}

		// UniqueSize — то, что освободится: слои, общие с другими
		// образами, останутся на диске.
		size := parseSize([]string{string(img.UniqueSize)})
		if size == 0 {
			size = parseSize([]string{string(img.Size)})
		}

		it := core.Item{
			ID:         string(img.ID),
			CategoryID: catImages,
			Name:       name,
			Detail:     string(img.CreatedSince),
			Size:       size,
		}
		if n, _ := strconv.Atoi(string(img.Containers)); n > 0 {
			it.Status = core.StatusInUse // образ нужен контейнеру
			it.Busy = &core.Reason{Key: reasonImageUsed, Args: map[string]any{"Count": n}}
		}
		items = append(items, it)
	}

	for _, v := range df.Volumes {
		name := string(v.Name)
		if strings.Contains(string(v.Labels), "com.docker.volume.anonymous") {
			name = "anonymous · " + shortID(name)
		}

		it := core.Item{
			ID:         string(v.Name),
			CategoryID: catVolumes,
			Name:       name,
			Size:       parseSize([]string{string(v.Size)}),
		}
		if n, _ := strconv.Atoi(string(v.Links)); n > 0 {
			it.Status = core.StatusInUse // том подключён к контейнеру
			it.Busy = &core.Reason{Key: reasonVolumeAttached, Args: map[string]any{"Count": n}}
		}
		items = append(items, it)
	}

	// Кэш сборки — сотни записей. Показываем одним объектом всё, что
	// не используется прямо сейчас: именно это удалит docker builder prune.
	var cache core.Bytes
	for _, b := range df.BuildCache {
		if b.InUse != "true" {
			cache += parseSize([]string{string(b.Size)})
		}
	}
	if cache > 0 {
		items = append(items, core.Item{
			ID:         "build-cache",
			CategoryID: catBuildCache,
			Name:       "Build cache",
			Size:       cache,
		})
	}

	return items
}

// Plan — команды docker для каждого объекта.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	docker, ok := p.look("docker")
	if !ok {
		return nil, core.Unavailable(reasonNotInstalled)
	}

	// Сначала контейнеры, потом образы и тома: так удаление контейнера
	// не помешает удалить его образ в том же плане.
	order := []string{catContainers, catImages, catVolumes, catBuildCache}

	var actions []core.Action
	for _, cat := range order {
		for _, it := range items {
			if it.CategoryID != cat {
				continue
			}
			var cmd []string
			switch cat {
			case catContainers:
				cmd = []string{docker, "rm", it.ID}
			case catImages:
				cmd = []string{docker, "rmi", it.ID}
			case catVolumes:
				cmd = []string{docker, "volume", "rm", it.ID}
			case catBuildCache:
				cmd = []string{docker, "builder", "prune", "-f"}
			}
			actions = append(actions, core.Action{Command: cmd})
		}
	}

	return actions, nil
}

// shortID — первые 12 символов ID, как их показывает сам Docker.
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// sizeRe разбирает размер в стиле Docker: "66.72MB", "0B", "7.19kB".
var sizeRe = regexp.MustCompile(`^([0-9.]+)\s*([kKMGTP]?i?B)$`)

// parseSize берёт первое слово из fields и превращает его в байты.
// Docker считает в десятичных единицах, как и мы. Непонятное — 0.
func parseSize(fields []string) core.Bytes {
	if len(fields) == 0 {
		return 0
	}

	m := sizeRe.FindStringSubmatch(fields[0]) // [всё совпадение, число, единица]
	if m == nil {
		return 0
	}

	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}

	multipliers := map[string]float64{
		"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}

	return core.Bytes(value * multipliers[m[2]])
}
