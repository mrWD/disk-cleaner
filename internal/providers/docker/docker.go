// Package docker is the provider for Docker: containers, images, volumes and the build cache.
//
// We learn everything with one command: docker system df -v --format '{{json .}}'.
// It returns JSON for all four kinds of objects at once, including how
// many containers use each image and each volume.
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

	// Keys of the reasons Docker is unavailable right now (texts are in translations).
	reasonNotInstalled = "docker.not_installed"
	reasonNotRunning   = "docker.not_running"

	// Who is using the object.
	reasonImageUsed      = "docker.image_used"
	reasonVolumeAttached = "docker.volume_attached"
)

// Provider asks Docker through its command line.
type Provider struct {
	run  shell.Runner                     // runs commands; a fake in tests
	look func(name string) (string, bool) // looks up a program; a fake in tests
}

// New is the provider that runs the real docker.
func New() *Provider {
	return &Provider{run: shell.Run, look: shell.LookPath}
}

func (p *Provider) ID() string { return "docker" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		// A stopped container may still hold data that was written inside it.
		{ID: catContainers, Risk: core.RiskCaution},
		{ID: catImages, Risk: core.RiskSafe},
		// Volumes often hold databases.
		{ID: catVolumes, Risk: core.RiskCaution},
		{ID: catBuildCache, Risk: core.RiskSafe},
	}
}

// text is a string in Docker's JSON. Docker returns some fields sometimes as a string
// ("0"), sometimes as a number (0); this type accepts both.
type text string

// UnmarshalJSON is called by the encoding/json package for fields of type text.
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

// dfOutput is the part of the docker system df -v answer that we need.
// Field names match the JSON keys, so the `json:"..."` tags are not needed.
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

	// If Docker "hangs", we do not wait forever.
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

// isNotRunning tells from the error text that Docker is simply turned off.
func isNotRunning(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "Is the docker daemon running") ||
		strings.Contains(msg, "docker.sock")
}

// toItems turns Docker's answer into items. A separate function without
// running commands, so it is easy to test on recorded JSON.
func toItems(df dfOutput) []core.Item {
	var items []core.Item

	for _, c := range df.Containers {
		it := core.Item{
			ID:         string(c.ID),
			CategoryID: catContainers,
			Name:       string(c.Names) + " · " + string(c.Image),
			Detail:     string(c.Status),
			// "2.5kB (virtual 120MB)": only the first part will be freed,
			// the "virtual" one is the image, which is deleted separately.
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

		// UniqueSize is what will be freed: layers shared with other
		// images stay on disk.
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
			it.Status = core.StatusInUse // the image is needed by a container
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
			it.Status = core.StatusInUse // the volume is attached to a container
			it.Busy = &core.Reason{Key: reasonVolumeAttached, Args: map[string]any{"Count": n}}
		}
		items = append(items, it)
	}

	// The build cache is hundreds of entries. We show everything that
	// is not in use right now as one item: that is exactly what docker builder prune deletes.
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

// Plan is the docker commands for each item.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	docker, ok := p.look("docker")
	if !ok {
		return nil, core.Unavailable(reasonNotInstalled)
	}

	// Containers first, then images and volumes: this way deleting a container
	// does not get in the way of deleting its image in the same plan.
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

// shortID is the first 12 characters of an ID, as Docker itself shows them.
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// sizeRe parses a size in Docker's style: "66.72MB", "0B", "7.19kB".
var sizeRe = regexp.MustCompile(`^([0-9.]+)\s*([kKMGTP]?i?B)$`)

// parseSize takes the first word from fields and turns it into bytes.
// Docker counts in decimal units, like we do. Anything unclear is 0.
func parseSize(fields []string) core.Bytes {
	if len(fields) == 0 {
		return 0
	}

	m := sizeRe.FindStringSubmatch(fields[0]) // [whole match, number, unit]
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
