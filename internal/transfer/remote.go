package transfer

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

// Remote devolve o endpoint de um host acessado por SFTP.
func Remote(host string, client *sftp.Client) Endpoint {
	return &remoteEndpoint{host: host, client: client}
}

type remoteEndpoint struct {
	host   string
	client *sftp.Client
	// homeDir guarda o diretorio inicial ja descoberto (o "~" do shell).
	homeDir string
}

func (r *remoteEndpoint) Name() string { return r.host }

// caminhos remotos usam sempre "/", mesmo quando a maquina local e Windows
func (r *remoteEndpoint) Join(elem ...string) string { return path.Join(elem...) }

func (r *remoteEndpoint) Base(p string) string { return path.Base(p) }

func (r *remoteEndpoint) Resolve(p string) (string, error) {
	if p == "" {
		return r.home()
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := r.home()
		if err != nil {
			return "", err
		}
		return path.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	// "~usuario" depende do cadastro de contas do servidor, que o SFTP nao expoe
	if strings.HasPrefix(p, "~") {
		return "", fmt.Errorf("caminho remoto com ~usuario nao e suportado (%s: use o caminho absoluto)", r.host)
	}
	return p, nil
}

// home devolve o diretorio inicial do usuario no host (o "~" do shell).
func (r *remoteEndpoint) home() (string, error) {
	if r.homeDir != "" {
		return r.homeDir, nil
	}
	wd, err := r.client.Getwd()
	if err != nil {
		return "", fmt.Errorf("descobrir o diretorio inicial de %s: %w", r.host, err)
	}
	r.homeDir = wd
	return wd, nil
}

func (r *remoteEndpoint) HasTrailingSeparator(p string) bool { return strings.HasSuffix(p, "/") }

func (r *remoteEndpoint) Lstat(p string) (os.FileInfo, error) { return r.client.Lstat(p) }

func (r *remoteEndpoint) ReadDir(p string) ([]os.FileInfo, error) { return r.client.ReadDir(p) }

func (r *remoteEndpoint) Open(p string) (io.ReadCloser, error) { return r.client.Open(p) }

func (r *remoteEndpoint) Create(p string) (io.WriteCloser, error) { return r.client.Create(p) }

func (r *remoteEndpoint) MkdirAll(p string, mode os.FileMode) error { return r.client.MkdirAll(p) }

func (r *remoteEndpoint) Chmod(p string, mode os.FileMode) error { return r.client.Chmod(p, mode) }

func (r *remoteEndpoint) Chtimes(p string, atime, mtime time.Time) error {
	return r.client.Chtimes(p, atime, mtime)
}

func (r *remoteEndpoint) ReadLink(p string) (string, error) { return r.client.ReadLink(p) }

func (r *remoteEndpoint) Symlink(target, link string) error { return r.client.Symlink(target, link) }

func (r *remoteEndpoint) Remove(p string) error { return r.client.Remove(p) }
