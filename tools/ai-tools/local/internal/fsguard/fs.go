// Package fsguard bounds read-only access to explicitly selected directories.
package fsguard

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Display(p string) string {
	h, _ := os.UserHomeDir()
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}
func Root(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		return "", errors.New("请选择存在的目录")
	}
	home, _ := os.UserHomeDir()
	home, _ = filepath.EvalSymlinks(home)
	relHome, _ := filepath.Rel(strings.ToLower(p), strings.ToLower(home))
	if p == filepath.Dir(p) || strings.EqualFold(p, home) || (relHome != "." && filepath.IsLocal(relHome)) {
		return "", errors.New("请选具体记录目录，不能扫描家目录或磁盘根目录")
	}
	if userDirectory(p) {
		return "", errors.New("请选择用户目录内的具体记录目录，不能扫描整个用户目录")
	}
	for _, broad := range []string{"/Users", "/home", "/Volumes", `C:\Users`} {
		if strings.EqualFold(p, broad) {
			return "", errors.New("请选择具体记录目录，不能扫描多用户或卷目录")
		}
	}
	for _, bad := range []string{"/System", "/Library", "/etc", "/proc", "/dev", "/sys", "/bin", "/sbin", "/usr", "/private/etc", `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`} {
		a, b := strings.ToLower(p), strings.ToLower(bad)
		if a == b || strings.HasPrefix(a, b+string(filepath.Separator)) {
			return "", errors.New("不能扫描系统目录")
		}
	}
	return p, nil
}

// Open rejects links in every component; os.Root also prevents escape during a rename race.
func Open(root *os.Root, rel string, max int64) (*os.File, error) {
	if !filepath.IsLocal(rel) {
		return nil, errors.New("路径越界")
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	cur := ""
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		st, err := root.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 || st.Mode()&os.ModeIrregular != 0 {
			return nil, errors.New("跳过链接或特殊文件")
		}
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("不是普通文件")
	}
	if st.Size() > max {
		f.Close()
		return nil, errors.New("文件超过大小上限")
	}
	return f, nil
}
func Read(path string, max int64) ([]byte, string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "unreadable"
	}
	// Open from the volume root so parent symlinks are checked as well.
	base := filepath.VolumeName(abs) + string(filepath.Separator)
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, "unreadable"
	}
	defer r.Close()
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return nil, "unreadable"
	}
	f, err := Open(r, rel, max)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "absent"
		}
		if strings.Contains(err.Error(), "链接") {
			return nil, "skipped_symlink"
		}
		if strings.Contains(err.Error(), "大小") {
			return nil, "too_large"
		}
		return nil, "unreadable"
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, "unreadable"
	}
	if int64(len(b)) > max {
		return nil, "too_large"
	}
	return b, "found"
}

// ReadUnder resolves the selected base once, while retaining link checks below it.
func ReadUnder(base, rel string, max int64) ([]byte, string) {
	if !filepath.IsLocal(rel) {
		return nil, "unreadable"
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "absent"
		}
		return nil, "unreadable"
	}
	return Read(filepath.Join(resolved, rel), max)
}

func userDirectory(p string) bool {
	parent := filepath.Dir(p)
	for _, users := range []string{"/Users", "/home", filepath.Join(filepath.VolumeName(p)+string(filepath.Separator), "Users")} {
		if strings.EqualFold(parent, filepath.Clean(users)) {
			return true
		}
	}
	return false
}
