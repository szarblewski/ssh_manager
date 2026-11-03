package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("gravar %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler %s: %v", path, err)
	}
	return string(data)
}

func TestCopyFileIntoExistingDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "app.conf")
	writeFile(t, src, "listen 8080")

	dstDir := filepath.Join(root, "backup")
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Copy(Local(), src, Local(), dstDir, Options{})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if res.Files != 1 || res.Bytes != int64(len("listen 8080")) {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if got := readFile(t, filepath.Join(dstDir, "app.conf")); got != "listen 8080" {
		t.Fatalf("conteudo copiado = %q", got)
	}
}

func TestCopyFileWithNewName(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "app.conf")
	writeFile(t, src, "x")
	dst := filepath.Join(root, "app.conf.bak")

	if _, err := Copy(Local(), src, Local(), dst, Options{}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := readFile(t, dst); got != "x" {
		t.Fatalf("conteudo = %q", got)
	}
}

func TestCopyOverwritesExistingFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "novo.txt")
	dst := filepath.Join(root, "antigo.txt")
	writeFile(t, src, "novo conteudo")
	writeFile(t, dst, "conteudo antigo e maior")

	if _, err := Copy(Local(), src, Local(), dst, Options{}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := readFile(t, dst); got != "novo conteudo" {
		t.Fatalf("conteudo = %q", got)
	}
}

func TestCopyDirectoryRequiresRecursive(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "site")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(srcDir, "index.html"), "<h1>oi</h1>")

	_, err := Copy(Local(), srcDir, Local(), filepath.Join(root, "destino"), Options{})
	if err == nil || !strings.Contains(err.Error(), "use -r") {
		t.Fatalf("esperava erro pedindo -r, obtido %v", err)
	}
}

func TestCopyDirectoryRecursive(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "site")
	if err := os.MkdirAll(filepath.Join(srcDir, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(srcDir, "index.html"), "home")
	writeFile(t, filepath.Join(srcDir, "css", "app.css"), "body{}")

	var logged []string
	res, err := Copy(Local(), srcDir, Local(), filepath.Join(root, "destino"), Options{
		Recursive: true,
		Logf:      func(format string, args ...any) { logged = append(logged, format) },
	})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if res.Dirs != 2 || res.Files != 2 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if got := readFile(t, filepath.Join(root, "destino", "css", "app.css")); got != "body{}" {
		t.Fatalf("conteudo = %q", got)
	}
	if len(logged) == 0 {
		t.Fatal("Logf deveria relatar a copia")
	}
}

func TestCopyToTrailingSeparatorCreatesDirectoryEntry(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "nota.txt")
	writeFile(t, src, "texto")

	dstDir := filepath.Join(root, "nova")
	if _, err := Copy(Local(), src, Local(), dstDir+string(filepath.Separator), Options{}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := readFile(t, filepath.Join(dstDir, "nota.txt")); got != "texto" {
		t.Fatalf("conteudo = %q", got)
	}
}

func TestPreserveKeepsModeAndTime(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "exec.sh")
	writeFile(t, src, "#!/bin/sh\n")
	if err := os.Chmod(src, 0o750); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "copia.sh")
	if _, err := Copy(Local(), src, Local(), dst, Options{Preserve: true}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.ModTime(); !got.Equal(old) {
		t.Fatalf("data = %v, esperado %v", got, old)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o750 {
			t.Fatalf("permissao = %v, esperada 0750", got)
		}
	}
}

func TestSymlinkIsCopiedAsLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "original.txt")
	writeFile(t, target, "conteudo")
	link := filepath.Join(root, "atalho.txt")
	if err := os.Symlink("original.txt", link); err != nil {
		t.Skipf("symlink indisponivel: %v", err)
	}

	dst := filepath.Join(root, "atalho-copia.txt")
	res, err := Copy(Local(), link, Local(), dst, Options{})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if res.Links != 1 || res.Files != 0 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	got, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("ler link copiado: %v", err)
	}
	if got != "original.txt" {
		t.Fatalf("destino do link = %q", got)
	}
}

func TestCopyReportsMissingSource(t *testing.T) {
	root := t.TempDir()
	_, err := Copy(Local(), filepath.Join(root, "nao-existe"), Local(), root, Options{})
	if err == nil || !strings.Contains(err.Error(), "origem") {
		t.Fatalf("esperava erro de origem, obtido %v", err)
	}
}

// TestRemoteResolveExpandsHome cobre o "~" do shell no lado remoto: o SFTP nao
// conhece o atalho, entao ele e trocado pelo diretorio inicial do usuario.
func TestRemoteResolveExpandsHome(t *testing.T) {
	// homeDir preenchido evita a conexao (Resolve usa o valor ja descoberto)
	remote := &remoteEndpoint{host: "alfa", homeDir: "/home/deploy"}

	cases := map[string]string{
		"":             "/home/deploy",
		"~":            "/home/deploy",
		"~/":           "/home/deploy",
		"~/logs/app":   "/home/deploy/logs/app",
		"/etc/hosts":   "/etc/hosts",
		"relativo.txt": "relativo.txt",
	}
	for in, want := range cases {
		got, err := remote.Resolve(in)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("Resolve(%q) = %q, esperado %q", in, got, want)
		}
	}

	if _, err := remote.Resolve("~outro/app"); err == nil {
		t.Fatal("~usuario deveria falhar com mensagem clara")
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		512:     "512 B",
		1024:    "1.0 KiB",
		1536:    "1.5 KiB",
		1 << 20: "1.0 MiB",
	}
	for in, want := range cases {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, esperado %q", in, got, want)
		}
	}
}

func TestResultAdd(t *testing.T) {
	var res Result
	res.Add(Result{Files: 2, Bytes: 10})
	res.Add(Result{Files: 1, Dirs: 1, Links: 1, Bytes: 5})
	if res.Files != 3 || res.Dirs != 1 || res.Links != 1 || res.Bytes != 15 {
		t.Fatalf("soma inesperada: %+v", res)
	}
}

func TestProgressReportsBytesAndTotal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "grande.bin")
	content := bytes.Repeat([]byte("abcd"), 20000)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	var updates []Progress
	_, err := Copy(Local(), src, Local(), filepath.Join(root, "copia.bin"), Options{
		Progress: func(update Progress) { updates = append(updates, update) },
	})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(updates) < 2 {
		t.Fatalf("esperava varias atualizacoes, obtido %d", len(updates))
	}
	for i := 1; i < len(updates); i++ {
		if updates[i].Done < updates[i-1].Done {
			t.Fatalf("o progresso regrediu: %+v", updates)
		}
	}
	last := updates[len(updates)-1]
	if last.Done != int64(len(content)) || last.Total != int64(len(content)) {
		t.Fatalf("ultimo progresso = %+v", last)
	}
	if !strings.Contains(last.File, "grande.bin") {
		t.Fatalf("origem inesperada no progresso: %q", last.File)
	}
}
