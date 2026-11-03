// Package transfer copia arquivos entre a maquina local e os hosts
// cadastrados no sshm (e entre dois hosts), no estilo do scp.
//
// A copia e feita em fluxo (nenhum arquivo temporario): quando os dois lados
// sao remotos, os bytes passam pela maquina local, como em
// "ssh hostA -- cat arquivo | ssh hostB -- \"cat > arquivo\"".
package transfer

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Endpoint e um dos lados da copia: a maquina local ou um host remoto (SFTP).
type Endpoint interface {
	// Name identifica o lado nas mensagens (vazio = maquina local).
	Name() string
	// Join monta um caminho com o separador do lado.
	Join(elem ...string) string
	// Base devolve o nome final de um caminho.
	Base(p string) string
	// Resolve completa um caminho vazio com o diretorio atual.
	Resolve(p string) (string, error)
	// HasTrailingSeparator diz se o caminho termina com o separador do lado,
	// o que indica que o destino e um diretorio.
	HasTrailingSeparator(p string) bool

	Lstat(p string) (os.FileInfo, error)
	ReadDir(p string) ([]os.FileInfo, error)
	Open(p string) (io.ReadCloser, error)
	Create(p string) (io.WriteCloser, error)
	MkdirAll(p string, mode os.FileMode) error
	Chmod(p string, mode os.FileMode) error
	Chtimes(p string, atime, mtime time.Time) error
	ReadLink(p string) (string, error)
	Symlink(target, link string) error
	Remove(p string) error
}

// Options controla a copia.
type Options struct {
	// Recursive copia diretorios (como scp -r).
	Recursive bool
	// Preserve mantem permissao e data de modificacao (como scp -p).
	Preserve bool
	// Logf recebe uma linha por arquivo copiado (nil = silencioso).
	Logf func(format string, args ...any)
	// Progress recebe o andamento de cada arquivo (nil = sem barra).
	Progress func(update Progress)
}

// Progress descreve o andamento da copia de um arquivo.
type Progress struct {
	// File identifica a origem (com o host, quando remota).
	File string
	// Done sao os bytes ja copiados deste arquivo.
	Done int64
	// Total e o tamanho do arquivo (0 quando desconhecido).
	Total int64
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// warnf relata um problema que nao interrompe a copia.
func (o Options) warnf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf("aviso: "+format, args...)
	}
}

// Result resume o que foi copiado.
type Result struct {
	Files int
	Dirs  int
	Links int
	Bytes int64
}

// Add acumula outro resultado.
func (r *Result) Add(other Result) {
	r.Files += other.Files
	r.Dirs += other.Dirs
	r.Links += other.Links
	r.Bytes += other.Bytes
}

// Copy copia srcPath (no endpoint src) para dstPath (no endpoint dst).
//
// Como no scp, quando o destino e um diretorio existente (ou termina com
// separador) o arquivo entra dentro dele com o nome da origem.
func Copy(src Endpoint, srcPath string, dst Endpoint, dstPath string, opts Options) (Result, error) {
	var res Result

	resolvedSrc, err := src.Resolve(srcPath)
	if err != nil {
		return res, err
	}
	resolvedDst, err := dst.Resolve(dstPath)
	if err != nil {
		return res, err
	}

	info, err := src.Lstat(resolvedSrc)
	if err != nil {
		return res, fmt.Errorf("origem %s: %w", label(src, resolvedSrc), err)
	}

	target := resolvedDst
	if dst.HasTrailingSeparator(dstPath) {
		// separador no fim = "quero copiar para dentro deste diretorio"
		if err := dst.MkdirAll(resolvedDst, 0o755); err != nil {
			return res, fmt.Errorf("criar o diretorio %s: %w", label(dst, resolvedDst), err)
		}
		target = dst.Join(resolvedDst, src.Base(resolvedSrc))
	} else if dstInfo, err := dst.Lstat(resolvedDst); err == nil && dstInfo.IsDir() {
		target = dst.Join(resolvedDst, src.Base(resolvedSrc))
	}

	if err := copyNode(src, resolvedSrc, info, dst, target, opts, &res); err != nil {
		return res, err
	}
	return res, nil
}

func copyNode(src Endpoint, srcPath string, info os.FileInfo, dst Endpoint, dstPath string, opts Options, res *Result) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return copySymlink(src, srcPath, dst, dstPath, opts, res)
	case info.IsDir():
		if !opts.Recursive {
			return fmt.Errorf("%s e um diretorio (use -r para copiar recursivamente)", label(src, srcPath))
		}
		return copyDir(src, srcPath, info, dst, dstPath, opts, res)
	case info.Mode().IsRegular():
		return copyFile(src, srcPath, info, dst, dstPath, opts, res)
	default:
		return fmt.Errorf("%s: tipo de arquivo nao suportado (%s)", label(src, srcPath), info.Mode().Type())
	}
}

func copyFile(src Endpoint, srcPath string, info os.FileInfo, dst Endpoint, dstPath string, opts Options, res *Result) error {
	in, err := src.Open(srcPath)
	if err != nil {
		return fmt.Errorf("abrir %s: %w", label(src, srcPath), err)
	}
	defer in.Close()

	out, err := dst.Create(dstPath)
	if err != nil {
		return fmt.Errorf("criar %s: %w", label(dst, dstPath), err)
	}

	written, copyErr := copyWithProgress(in, out, label(src, srcPath), info.Size(), opts.Progress)
	closeErr := out.Close()
	switch {
	case copyErr != nil:
		return fmt.Errorf("copiar %s: %w", label(src, srcPath), copyErr)
	case closeErr != nil:
		return fmt.Errorf("gravar %s: %w", label(dst, dstPath), closeErr)
	}

	if opts.Preserve {
		preserveMetadata(dst, dstPath, info, opts)
	}

	res.Files++
	res.Bytes += written
	opts.logf("%s -> %s (%s)", label(src, srcPath), label(dst, dstPath), HumanSize(written))
	return nil
}

// copyWithProgress copia em blocos para poder relatar o andamento.
//
// Nao usa io.Copy de proposito: com um destino que implementa io.ReaderFrom
// (como o *sftp.File), o io.Copy copiaria tudo de uma vez e nao haveria como
// desenhar a barra.
func copyWithProgress(in io.Reader, out io.Writer, file string, total int64, progress func(Progress)) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64

	report := func() {
		if progress != nil {
			progress(Progress{File: file, Done: written, Total: total})
		}
	}
	report()

	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return written, writeErr
			}
			written += int64(n)
			report()
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func copyDir(src Endpoint, srcPath string, info os.FileInfo, dst Endpoint, dstPath string, opts Options, res *Result) error {
	mode := os.FileMode(0o755)
	if opts.Preserve {
		mode = info.Mode().Perm()
	}
	if err := dst.MkdirAll(dstPath, mode); err != nil {
		return fmt.Errorf("criar o diretorio %s: %w", label(dst, dstPath), err)
	}
	res.Dirs++
	opts.logf("diretorio %s -> %s", label(src, srcPath), label(dst, dstPath))

	entries, err := src.ReadDir(srcPath)
	if err != nil {
		return fmt.Errorf("listar %s: %w", label(src, srcPath), err)
	}
	for _, entry := range entries {
		childSrc := src.Join(srcPath, entry.Name())
		childDst := dst.Join(dstPath, entry.Name())
		if err := copyNode(src, childSrc, entry, dst, childDst, opts, res); err != nil {
			return err
		}
	}

	if opts.Preserve {
		preserveMetadata(dst, dstPath, info, opts)
	}
	return nil
}

func copySymlink(src Endpoint, srcPath string, dst Endpoint, dstPath string, opts Options, res *Result) error {
	target, err := src.ReadLink(srcPath)
	if err != nil {
		return fmt.Errorf("ler o link %s: %w", label(src, srcPath), err)
	}
	// o destino pode ter um arquivo ou link anterior: remove para recriar
	_ = dst.Remove(dstPath)
	if err := dst.Symlink(target, dstPath); err != nil {
		return fmt.Errorf("criar o link %s: %w", label(dst, dstPath), err)
	}
	res.Links++
	opts.logf("link %s -> %s (aponta para %s)", label(src, srcPath), label(dst, dstPath), target)
	return nil
}

// preserveMetadata aplica permissao e data de modificacao sem falhar a copia:
// um servidor sem permissao para mudar o dono do arquivo, por exemplo, apenas
// gera aviso.
func preserveMetadata(dst Endpoint, dstPath string, info os.FileInfo, opts Options) {
	if err := dst.Chmod(dstPath, info.Mode().Perm()); err != nil {
		opts.warnf("nao foi possivel ajustar a permissao de %s: %v", label(dst, dstPath), err)
	}
	if err := dst.Chtimes(dstPath, info.ModTime(), info.ModTime()); err != nil {
		opts.warnf("nao foi possivel ajustar a data de %s: %v", label(dst, dstPath), err)
	}
}

func label(endpoint Endpoint, path string) string {
	if endpoint.Name() == "" {
		return path
	}
	return endpoint.Name() + ":" + path
}

// HumanSize formata bytes para leitura humana (512 B, 1.2 KiB, 3.4 MiB).
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
