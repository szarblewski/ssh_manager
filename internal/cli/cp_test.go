package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/szarblewski/ssh_manager/internal/config"
)

func TestParseCopyArgs(t *testing.T) {
	parsed, err := parseCopyArgs([]string{"-r", "origem", "--preserve", "destino", "-q"})
	if err != nil {
		t.Fatalf("parseCopyArgs: %v", err)
	}
	if !parsed.options.Recursive || !parsed.options.Preserve || !parsed.quiet {
		t.Fatalf("opcoes nao reconhecidas: %+v", parsed)
	}
	if len(parsed.targets) != 2 || parsed.targets[0] != "origem" || parsed.targets[1] != "destino" {
		t.Fatalf("caminhos = %v", parsed.targets)
	}

	// opcoes agrupadas e depois de "--"
	parsed, err = parseCopyArgs([]string{"a", "b", "--", "-r"})
	if err != nil {
		t.Fatalf("parseCopyArgs: %v", err)
	}
	if parsed.options.Recursive {
		t.Fatal("-r depois de -- deveria ser caminho")
	}
	if len(parsed.targets) != 3 {
		t.Fatalf("caminhos = %v", parsed.targets)
	}

	if _, err := parseCopyArgs([]string{"--recursive", "-X", "a", "b"}); err == nil {
		t.Fatal("opcao desconhecida deveria falhar")
	}
	if _, err := parseCopyArgs([]string{"-z", "a", "b"}); err == nil {
		t.Fatal("-z deveria falhar")
	}
}

func TestParseCopyTargetRecognizesHosts(t *testing.T) {
	cfg := config.New()
	cfg.Hosts = []config.Host{
		{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"},
		{ID: 2, Name: "beta", Host: "10.0.0.2", User: "root"},
	}

	remote, err := parseCopyTarget(cfg, "alfa:/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatalf("parseCopyTarget: %v", err)
	}
	if !remote.remote || remote.host.Name != "alfa" || remote.path != "/etc/nginx/nginx.conf" {
		t.Fatalf("alvo remoto inesperado: %+v", remote)
	}

	byID, err := parseCopyTarget(cfg, "2:")
	if err != nil {
		t.Fatalf("parseCopyTarget: %v", err)
	}
	if !byID.remote || byID.host.Name != "beta" || byID.path != "" {
		t.Fatalf("alvo por id inesperado: %+v", byID)
	}

	local, err := parseCopyTarget(cfg, "./pasta")
	if err != nil {
		t.Fatalf("parseCopyTarget: %v", err)
	}
	if local.remote {
		t.Fatalf("./pasta deveria ser local: %+v", local)
	}

	// "C:\..." e caminho local, nunca host "C"
	drive, err := parseCopyTarget(cfg, `C:\Users\alguem\hosts.json`)
	if err != nil {
		t.Fatalf("parseCopyTarget: %v", err)
	}
	if drive.remote {
		t.Fatalf("caminho com letra de unidade deveria ser local: %+v", drive)
	}

	if _, err := parseCopyTarget(cfg, "gama:/tmp"); err == nil ||
		!strings.Contains(err.Error(), "nao esta cadastrado") {
		t.Fatalf("host desconhecido deveria falhar, obtido %v", err)
	}
}

func TestParseCopyTargetRejectsAmbiguousName(t *testing.T) {
	cfg := config.New()
	cfg.Hosts = []config.Host{
		{ID: 1, Name: "alfa", Host: "10.0.0.1"},
		{ID: 2, Name: "ALFA", Host: "10.0.0.2"},
	}
	if _, err := parseCopyTarget(cfg, "alfa:/tmp"); err == nil ||
		!strings.Contains(err.Error(), "mais de um host") {
		t.Fatalf("nome ambiguo deveria falhar, obtido %v", err)
	}
}

func TestCopyLocalToLocal(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	root := t.TempDir()
	src := filepath.Join(root, "notas.txt")
	if err := os.WriteFile(src, []byte("conteudo"), 0o644); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(root, "backup")
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if code := app.Run([]string{"cp", src, dstDir}); code != 0 {
		t.Fatalf("cp local falhou: codigo %d (%s)", code, errOut)
	}
	data, err := os.ReadFile(filepath.Join(dstDir, "notas.txt"))
	if err != nil || string(data) != "conteudo" {
		t.Fatalf("copia local = %q, %v", data, err)
	}
	if !strings.Contains(errOut.String(), "concluido") {
		t.Fatalf("faltou o resumo da copia: %s", errOut)
	}

	// -q silencia o resumo
	errOut.Reset()
	if code := app.Run([]string{"cp", "-q", src, filepath.Join(dstDir, "copia.txt")}); code != 0 {
		t.Fatalf("-q: codigo %d (%s)", code, errOut)
	}
	if strings.Contains(errOut.String(), "concluido") {
		t.Fatalf("-q deveria silenciar: %s", errOut)
	}
}

func TestCopyUsageErrors(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	if code := app.Run([]string{"cp", "so-um"}); code != 1 {
		t.Fatalf("codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "uso: sshm cp") {
		t.Fatalf("mensagem de uso ausente: %s", errOut)
	}

	errOut.Reset()
	if code := app.Run([]string{"cp", "gama:/tmp", "."}); code != 1 {
		t.Fatalf("codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "nao esta cadastrado") {
		t.Fatalf("mensagem inesperada: %s", errOut)
	}
}

func TestCopyMultipleSourcesNeedsDirectory(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	root := t.TempDir()
	first := filepath.Join(root, "a.txt")
	second := filepath.Join(root, "b.txt")
	for _, name := range []string{first, second} {
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// destino e um arquivo (nao diretorio): recusa
	fileDst := filepath.Join(root, "destino.txt")
	if err := os.WriteFile(fileDst, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := app.Run([]string{"cp", first, second, fileDst}); code != 1 {
		t.Fatalf("codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "precisa ser um diretorio") {
		t.Fatalf("mensagem inesperada: %s", errOut)
	}

	// com um diretorio, as duas origens entram nele
	dstDir := filepath.Join(root, "destino")
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := app.Run([]string{"cp", first, second, dstDir}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(dstDir, name)); err != nil {
			t.Fatalf("%s nao foi copiado: %v", name, err)
		}
	}
}

func TestCopyRemoteTargetReportsConnectionFailure(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	// porta 1 nao tem servico: a falha acontece logo apos o dial
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "127.0.0.1", Port: 1, User: "root"})

	if code := app.Run([]string{"cp", "alfa:/etc/hosts", t.TempDir()}); code == 0 {
		t.Fatalf("deveria falhar ao conectar: %s", errOut)
	}
	if !strings.Contains(errOut.String(), "conectando em") {
		t.Fatalf("faltou a mensagem de conexao: %s", errOut)
	}
}

func TestHelpMentionsCopy(t *testing.T) {
	app, out, _, _ := newTestApp(t)
	if code := app.Run([]string{"--help"}); code != 0 {
		t.Fatalf("codigo = %d", code)
	}
	if !strings.Contains(out.String(), "sshm cp") {
		t.Fatalf("ajuda sem o comando cp:\n%s", out.String())
	}
}

func TestRemoteSpecPrefix(t *testing.T) {
	cfg := config.New()
	cfg.Hosts = []config.Host{
		{ID: 19, Name: "db-social", Host: "10.0.0.1", User: "root"},
		{ID: 3, Name: "srv-sap", Host: "10.0.0.2", User: "root"},
	}

	if prefix, rest, ok := remoteSpecPrefix(cfg, "19:/mnt/scripts/x.sh"); !ok || prefix != "19" || rest != "/mnt/scripts/x.sh" {
		t.Fatalf("spec por id = %q %q %v", prefix, rest, ok)
	}
	if prefix, rest, ok := remoteSpecPrefix(cfg, "srv-sap:~/"); !ok || prefix != "srv-sap" || rest != "~/" {
		t.Fatalf("spec por nome = %q %q %v", prefix, rest, ok)
	}
	for _, spec := range []string{"19", "gama:/tmp", `C:\pasta`, "/etc/hosts"} {
		if _, _, ok := remoteSpecPrefix(cfg, spec); ok {
			t.Fatalf("%q nao deveria ser alvo remoto", spec)
		}
	}
}

func TestLooksLikeCopy(t *testing.T) {
	app, _, _, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 19, Name: "db-social", Host: "10.0.0.1", User: "root"},
		config.Host{ID: 3, Name: "srv-sap", Host: "10.0.0.2", User: "root"},
	)

	cases := []struct {
		args []string
		want bool
		why  string
	}{
		{[]string{"19:/mnt/scripts/liberar-ssh.sh", "3:~/"}, true, "host a host"},
		{[]string{"./site.tar.gz", "19:/tmp/"}, true, "envia"},
		{[]string{"19:/etc/nginx.conf", "."}, true, "baixa"},
		{[]string{"19:/mnt/x.sh"}, false, "um argumento so e conexao"},
		{[]string{"19", "uptime"}, false, "comando remoto"},
		{[]string{"19", "-L", "8080:localhost:80"}, false, "opcoes do ssh"},
		{[]string{"19:/x", "-r", "3:/y"}, false, "opcao no meio"},
		{[]string{"db-social"}, false, "conexao"},
	}
	for _, c := range cases {
		if got := app.looksLikeCopy(c.args); got != c.want {
			t.Fatalf("%v (%s) = %v, esperado %v", c.args, c.why, got, c.want)
		}
	}
}

func TestCopyWithoutCommandRunsCopy(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	// porta 1 nao tem servico: a falha acontece logo apos o dial
	seed(t, path,
		config.Host{ID: 19, Name: "db-social", Host: "127.0.0.1", Port: 1, User: "root"},
		config.Host{ID: 3, Name: "srv-sap", Host: "127.0.0.1", Port: 1, User: "root"},
	)

	if code := app.Run([]string{"19:/mnt/scripts/liberar-ssh.sh", "3:~/"}); code == 0 {
		t.Fatalf("deveria falhar ao conectar: %s", errOut)
	}
	if !strings.Contains(errOut.String(), "conectando em") {
		t.Fatalf("nao entrou no caminho da copia: %s", errOut)
	}
	if strings.Contains(errOut.String(), "nenhum host encontrado") {
		t.Fatalf("ainda tratou o alvo como host: %s", errOut)
	}
}

func TestConnectHintsCopyForHostPath(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 19, Name: "db-social", Host: "10.0.0.1", User: "root"})

	if code := app.Run([]string{"19:/mnt/scripts/liberar-ssh.sh"}); code != 1 {
		t.Fatalf("codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "sshm cp") {
		t.Fatalf("faltou a sugestao de cp: %s", errOut)
	}
}
