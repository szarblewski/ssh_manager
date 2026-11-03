package transfer

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// Local devolve o endpoint da maquina local.
func Local() Endpoint { return localEndpoint{} }

type localEndpoint struct{}

func (localEndpoint) Name() string { return "" }

func (localEndpoint) Join(elem ...string) string { return filepath.Join(elem...) }

func (localEndpoint) Base(p string) string { return filepath.Base(p) }

func (localEndpoint) Resolve(p string) (string, error) {
	if p == "" {
		return os.Getwd()
	}
	return config.ExpandPath(p), nil
}

func (localEndpoint) HasTrailingSeparator(p string) bool {
	return strings.HasSuffix(p, "/") || strings.HasSuffix(p, string(filepath.Separator))
}

func (localEndpoint) Lstat(p string) (os.FileInfo, error) { return os.Lstat(p) }

func (localEndpoint) ReadDir(p string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (localEndpoint) Open(p string) (io.ReadCloser, error) { return os.Open(p) }

func (localEndpoint) Create(p string) (io.WriteCloser, error) {
	return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
}

func (localEndpoint) MkdirAll(p string, mode os.FileMode) error { return os.MkdirAll(p, mode) }

func (localEndpoint) Chmod(p string, mode os.FileMode) error { return os.Chmod(p, mode) }

func (localEndpoint) Chtimes(p string, atime, mtime time.Time) error {
	return os.Chtimes(p, atime, mtime)
}

func (localEndpoint) ReadLink(p string) (string, error) { return os.Readlink(p) }

func (localEndpoint) Symlink(target, link string) error { return os.Symlink(target, link) }

func (localEndpoint) Remove(p string) error { return os.Remove(p) }
