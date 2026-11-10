package core

import (
	"context"
	"testing"
)

// recorder remembers what was reported.
type recorder struct {
	looking  []string
	measured Bytes
}

func (r *recorder) Looking(path string)              { r.looking = append(r.looking, path) }
func (r *recorder) Measured(path string, size Bytes) { r.measured += size }

func TestReporter(t *testing.T) {
	t.Run("reports reach the reporter in the context", func(t *testing.T) {
		r := &recorder{}
		ctx := WithReporter(context.Background(), r)

		ReportLooking(ctx, "/a")
		ReportMeasured(ctx, "/a", 100)

		if len(r.looking) != 1 || r.looking[0] != "/a" || r.measured != 100 {
			t.Errorf("recorded %+v", r)
		}
	})

	t.Run("without a reporter nothing happens", func(t *testing.T) {
		ReportLooking(context.Background(), "/a") // must not panic
		ReportMeasured(context.Background(), "/a", 100)
	})
}
