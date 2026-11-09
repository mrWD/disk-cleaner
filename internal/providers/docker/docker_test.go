package docker

import (
	"context"
	"errors"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
)

// sampleDF — ответ docker system df -v в том виде, в каком его отдаёт
// Docker Desktop: числа в полях — строками, размеры — с единицами.
const sampleDF = `{
  "Containers": [
    {"ID": "a1b2c3d4e5f6", "Names": "redis", "Image": "redis:8", "State": "running", "Status": "Up 2 hours", "Size": "98MB (virtual 140MB)"},
    {"ID": "f6e5d4c3b2a1", "Names": "old-api", "Image": "node:22", "State": "exited", "Status": "Exited (0) 9 days ago", "Size": "2.5kB (virtual 1.1GB)"}
  ],
  "Images": [
    {"ID": "sha256:1111111111111111", "Repository": "redis", "Tag": "8", "Size": "140MB", "UniqueSize": "140MB", "Containers": "1", "CreatedSince": "3 weeks ago"},
    {"ID": "sha256:2222222222222222", "Repository": "postgres", "Tag": "17", "Size": "525.5MB", "UniqueSize": "312MB", "Containers": "0", "CreatedSince": "2 months ago"},
    {"ID": "sha256:3333333333333333", "Repository": "<none>", "Tag": "<none>", "Size": "80MB", "UniqueSize": "80MB", "Containers": 0, "CreatedSince": "1 month ago"}
  ],
  "Volumes": [
    {"Name": "03304a0e2b951bfec6030af9acb4eb9f", "Labels": "com.docker.volume.anonymous=", "Links": "0", "Size": "66.72MB"},
    {"Name": "pgdata", "Labels": "", "Links": "1", "Size": "1.2GB"}
  ],
  "BuildCache": [
    {"ID": "abc*", "InUse": "true", "Size": "7.19kB"},
    {"ID": "def*", "InUse": "false", "Size": "650MB"}
  ]
}`

// fakeDocker — провайдер, который вместо docker возвращает заданный ответ.
func fakeDocker(out string, runErr error) *Provider {
	return &Provider{
		look: func(string) (string, bool) { return "/usr/local/bin/docker", true },
		run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte(out), runErr
		},
	}
}

// byName — найти объект по имени (или пустой, если нет).
func byName(items []core.Item, name string) core.Item {
	i := slices.IndexFunc(items, func(it core.Item) bool { return it.Name == name })
	if i < 0 {
		return core.Item{}
	}
	return items[i]
}

func TestScan(t *testing.T) {
	ctx := context.Background()

	items, err := fakeDocker(sampleDF, nil).Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	providertest.CheckCategories(t, New(), items)

	t.Run("running container cannot be selected, stopped one can", func(t *testing.T) {
		if byName(items, "redis · redis:8").Status != core.StatusRunning {
			t.Error("redis should be Running")
		}
		if ok, _ := byName(items, "old-api · node:22").CanSelect(); !ok {
			t.Error("stopped old-api should be selectable")
		}
	})

	t.Run("image used by a container is in use", func(t *testing.T) {
		if byName(items, "redis:8").Status != core.StatusInUse {
			t.Error("redis:8 image should be InUse")
		}
		if ok, _ := byName(items, "postgres:17").CanSelect(); !ok {
			t.Error("postgres:17 is not used and should be selectable")
		}
	})

	t.Run("image size is what deleting frees, not the full size", func(t *testing.T) {
		if got := byName(items, "postgres:17").Size; got != 312_000_000 {
			t.Errorf("postgres:17 size = %s, want 312.0 MB", got)
		}
	})

	t.Run("untagged image and anonymous volume get readable names", func(t *testing.T) {
		if byName(items, "untagged · 333333333333").ID == "" {
			t.Error("untagged image not found by its short name")
		}
		if byName(items, "anonymous · 03304a0e2b95").ID == "" {
			t.Error("anonymous volume not found by its short name")
		}
	})

	t.Run("attached volume is in use", func(t *testing.T) {
		if byName(items, "pgdata").Status != core.StatusInUse {
			t.Error("pgdata is attached and should be InUse")
		}
	})

	t.Run("only unused build cache is counted", func(t *testing.T) {
		if got := byName(items, "Build cache").Size; got != 650_000_000 {
			t.Errorf("build cache = %s, want 650.0 MB", got)
		}
	})
}

func TestScanWhenDockerIsUnavailable(t *testing.T) {
	ctx := context.Background()

	t.Run("not installed", func(t *testing.T) {
		p := &Provider{look: func(string) (string, bool) { return "", false }}
		_, err := p.Scan(ctx)

		var u *core.UnavailableError
		if !errors.As(err, &u) || u.Reason.Key != reasonNotInstalled {
			t.Errorf("err = %v, want unavailable(not installed)", err)
		}
	})

	t.Run("not running", func(t *testing.T) {
		daemonDown := errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
		_, err := fakeDocker("", daemonDown).Scan(ctx)

		var u *core.UnavailableError
		if !errors.As(err, &u) || u.Reason.Key != reasonNotRunning {
			t.Errorf("err = %v, want unavailable(not running)", err)
		}
	})
}

func TestPlan(t *testing.T) {
	t.Run("containers are removed before images, each with its own command", func(t *testing.T) {
		p := fakeDocker("", nil)
		items := []core.Item{
			{ID: "sha256:2222", CategoryID: catImages},
			{ID: "f6e5d4c3b2a1", CategoryID: catContainers},
			{ID: "build-cache", CategoryID: catBuildCache},
		}

		actions, err := p.Plan(items)
		if err != nil {
			t.Fatal(err)
		}

		var got []string
		for _, a := range actions {
			got = append(got, a.String())
		}
		want := []string{
			"/usr/local/bin/docker rm f6e5d4c3b2a1",
			"/usr/local/bin/docker rmi sha256:2222",
			"/usr/local/bin/docker builder prune -f",
		}
		if !slices.Equal(got, want) {
			t.Errorf("plan =\n%v\nwant\n%v", got, want)
		}
	})
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(), reasonNotInstalled, reasonNotRunning, reasonImageUsed, reasonVolumeAttached)
}
