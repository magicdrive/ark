package fsroot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ReadAs reads the object its walk examined, or nothing: the entry replaced
// after it was described — by another regular file, by a symlink (to a file
// of the tree, or outside it) — is refused, and so is the read check's
// refusal, before anything is opened.
func TestEntryReadAs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		swap  func(t *testing.T, dir string)
		check error
		want  string // content, or "" for a refusal
	}{
		{"unchanged", nil, nil, "listed"},
		{"another file", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "f.txt")
			if err := os.WriteFile(p+".new", []byte("other"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(p+".new", p); err != nil {
				t.Fatal(err)
			}
		}, nil, ""},
		{"symlink in the tree", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "f.txt")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("secret.txt", p); err != nil {
				t.Skip("symlinks unavailable:", err)
			}
		}, nil, ""},
		{"symlink outside", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "f.txt")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(filepath.Dir(dir), "outside.txt"), p); err != nil {
				t.Skip("symlinks unavailable:", err)
			}
		}, nil, ""},
		{"check refuses", nil, errors.New("refused"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := t.TempDir()
			dir := filepath.Join(ws, "root")
			for name, body := range map[string]string{"root/f.txt": "listed", "root/secret.txt": "SECRET", "outside.txt": "OUTSIDE"} {
				p := filepath.Join(ws, filepath.FromSlash(name))
				os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			tr, err := Pin(dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer tr.Close()
			read := false
			err = tr.Walk(".", func(e Entry) error {
				if e.Rel() != "f.txt" {
					return nil
				}
				_, r, ok, err := e.Describe()
				if err != nil || !ok {
					t.Fatalf("describe: %v %v", ok, err)
				}
				if tc.swap != nil {
					tc.swap(t, dir)
				}
				checked := false
				b, err := e.ReadAs(r.Identity(), func(Identity) error { checked = true; return tc.check })
				read = true
				switch {
				case !checked:
					t.Error("the read check was not asked")
				case tc.want == "" && err == nil:
					t.Errorf("read %q, want a refusal", b)
				case tc.want != "" && (err != nil || string(b) != tc.want):
					t.Errorf("read %q %v, want %q", b, err, tc.want)
				case tc.check != nil && !errors.Is(err, tc.check):
					t.Errorf("error %v, want the check's", err)
				}
				return nil
			})
			if err != nil || !read {
				t.Fatalf("walk: %v (read %v)", err, read)
			}
		})
	}
}

// ReadResolved reaches the file through the directories its resolution
// passed, identity by identity: a directory on the way replaced by another
// directory — even one holding the very same file (a hard link) — fails the
// read, as Open does.
func TestReadResolved_DirectoryReplaced(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "root")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "sub", "f.txt")
	if err := os.WriteFile(f, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := Pin(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	r, err := tr.Resolve("sub/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := tr.ReadResolved(r); err != nil || string(b) != "content" {
		t.Fatalf("unchanged: %q %v", b, err)
	}
	if err := os.Rename(filepath.Join(dir, "sub"), filepath.Join(ws, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(ws, "old", "f.txt"), f); err != nil {
		t.Skip("hard links unavailable:", err)
	}
	if b, err := tr.ReadResolved(r); err == nil {
		t.Errorf("read %q through a replaced directory", b)
	}
	if fh, err := tr.Open(r); err == nil {
		fh.Close()
		t.Error("Open reached the file through a replaced directory")
	}
}
