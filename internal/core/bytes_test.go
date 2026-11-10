package core

import "testing"

// Tests in Go are functions named TestXxx in *_test.go files.
// They are run by go test and don't end up in the built program.
func TestBytesString(t *testing.T) {
	// A table of cases: behaviour name, input, expected result.
	// struct { ... } right in place is an anonymous struct, like an object in JS.
	cases := []struct {
		name string
		in   Bytes
		want string
	}{
		{"zero", 0, "0 B"},
		{"less than a kilobyte stays in bytes", 999, "999 B"},
		{"exactly one kilobyte", 1000, "1.0 kB"},
		{"megabytes with one decimal", 1_500_000, "1.5 MB"}, // _ in numbers is for readability
		{"typical free space", 104_300_000_000, "104.3 GB"},
	}

	// range walks the slice: _ is the index (not needed), c is the current case.
	for _, c := range cases {
		// t.Run is a subtest with its own name. The names describe behaviour,
		// like scenarios in BDD, and show up in the go test -v output.
		t.Run(c.name, func(t *testing.T) {
			got := c.in.String()

			if got != c.want {
				// %q prints the string in quotes, so stray spaces are visible.
				t.Errorf("Bytes(%d).String() = %q, want %q", int64(c.in), got, c.want)
			}
		})
	}
}
