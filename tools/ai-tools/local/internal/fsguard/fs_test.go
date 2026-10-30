package fsguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBoundedRead(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	regular := filepath.Join(dir, "settings.json")
	os.WriteFile(regular, []byte("{}"), 0600)
	for _, tc := range []struct {
		path string
		max  int64
		want string
	}{{regular, 10, "found"}, {regular, 1, "too_large"}, {filepath.Join(dir, "absent"), 10, "absent"}} {
		_, status := Read(tc.path, tc.max)
		if status != tc.want {
			t.Fatalf("%s %s", tc.path, status)
		}
	}
	link := filepath.Join(dir, "link")
	if os.Symlink(regular, link) == nil {
		if _, status := Read(link, 10); status != "skipped_symlink" {
			t.Fatal(status)
		}
	}
	parent := filepath.Join(dir, "linked-dir")
	if os.Symlink(dir, parent) == nil {
		if _, status := Read(filepath.Join(parent, "settings.json"), 10); status != "skipped_symlink" {
			t.Fatal(status)
		}
	}
	r, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	for _, p := range []string{"../settings.json", regular} {
		if f, e := Open(r, p, 10); e == nil {
			f.Close()
			t.Fatal("escaped root")
		}
	}
}

func TestUserDirectoryBoundary(t *testing.T) {
	for _, base := range []string{"/Users", "/home", filepath.VolumeName(os.TempDir()) + string(filepath.Separator) + "Users"} {
		p := filepath.Join(base, "other")
		if !userDirectory(p) {
			t.Fatal("accepted entire user directory", p)
		}
		if userDirectory(filepath.Join(p, "records")) {
			t.Fatal("rejected specific records directory")
		}
	}
}
