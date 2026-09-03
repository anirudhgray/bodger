package webui

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestChooseFS(t *testing.T) {
	dist := fstest.MapFS{"index.html": {Data: []byte("real build")}}
	placeholder := fstest.MapFS{"index.html": {Data: []byte("placeholder page")}}
	preBuildDist := fstest.MapFS{".gitkeep": {Data: []byte{}}}

	tests := []struct {
		name string
		dist fs.FS
		want string
	}{
		{"prefers dist once a real build has run", dist, "real build"},
		{"falls back to placeholder before any build has run", preBuildDist, "placeholder page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chooseFS(tt.dist, placeholder)
			b, err := fs.ReadFile(got, "index.html")
			if err != nil {
				t.Fatalf("ReadFile(index.html): %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("index.html = %q, want %q", b, tt.want)
			}
		})
	}
}

// TestDist proves the package's own go:embed directives actually compile
// and produce a filesystem with an index.html in it — the thing
// TestChooseFS can't cover, since it exercises chooseFS against fake
// filesystems rather than this package's real embedded state.
func TestDist(t *testing.T) {
	f := Dist()
	if _, err := fs.Stat(f, "index.html"); err != nil {
		t.Fatalf("Dist() has no index.html: %v", err)
	}
}
