package core

import "context"

// A scan can take a minute. To show what it's doing meanwhile, the window
// puts a Reporter into the scan's context, and the code that measures folders
// (diskutil.DirSize) reports to it: which folder it's looking at, and how big
// it turned out to be.
//
// The context is the right place for this: it already travels through every
// scan for cancellation, so no Provider has to change its methods, and code
// that doesn't report simply doesn't. A context without a Reporter makes the
// Report functions do nothing.

// Reporter receives a scan's progress. Its methods may be called from several
// goroutines at once (folders are measured in parallel), so they must be safe for that.
type Reporter interface {
	Looking(path string)              // started measuring this folder
	Measured(path string, size Bytes) // finished: this is its size
}

// reporterKey is the key for the Reporter in a context. An unexported type
// means no other package can put something else under the same key.
type reporterKey struct{}

// WithReporter returns a copy of ctx that carries r.
func WithReporter(ctx context.Context, r Reporter) context.Context {
	return context.WithValue(ctx, reporterKey{}, r)
}

// ReportLooking tells the context's Reporter, if any, which folder is being measured.
func ReportLooking(ctx context.Context, path string) {
	if r, ok := ctx.Value(reporterKey{}).(Reporter); ok {
		r.Looking(path)
	}
}

// ReportMeasured tells the context's Reporter, if any, a folder's size.
func ReportMeasured(ctx context.Context, path string, size Bytes) {
	if r, ok := ctx.Value(reporterKey{}).(Reporter); ok {
		r.Measured(path, size)
	}
}
