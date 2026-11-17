package pkgformat

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const execManifest = `{"schemaVersion":2,"id":"io.terra.exec","name":"E","version":"1.0.0","kind":"service","entrypoints":{"process":{"linux-amd64":"payload/run"}}}`

// A package built on a host with no exec bit (Windows reads every file 0666)
// must still record entrypoints and bin/ as executable, and everything else 0644.
func TestWritePackageMarksEntrypointsExecutable(t *testing.T) {
	root := t.TempDir()
	for name, mode := range map[string]os.FileMode{"payload/run": 0o666, "bin/linux-amd64/tool": 0o666, "data.txt": 0o666} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "e.tmod")
	if err := WritePackage(root, []byte(execManifest), dest); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	want := map[string]os.FileMode{"payload/run": 0o755, "bin/linux-amd64/tool": 0o755, "data.txt": 0o644}
	for _, f := range zr.File {
		if w, ok := want[f.Name]; ok && f.Mode().Perm() != w {
			t.Errorf("%s: mode %o, want %o", f.Name, f.Mode().Perm(), w)
		}
	}
}

// Packages already published with 0666 entrypoints get the exec bit on extract.
func TestExtractPackageFixesLegacyEntrypointMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no exec bit on Windows")
	}
	dest := craftZip(t, func(w *zip.Writer) {
		for _, e := range []struct{ name, body string }{{ManifestFileName, execManifest}, {"payload/run", "x"}} {
			h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
			h.SetMode(0o664)
			entry, _ := w.CreateHeader(h)
			entry.Write([]byte(e.body))
		}
	})
	out := filepath.Join(t.TempDir(), "out")
	if err := ExtractPackage(dest, out, DefaultArchiveLimits()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(out, "payload", "run"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("entrypoint not executable: %v %v", info, err)
	}
}
