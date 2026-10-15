package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/secrets"
)

func TestMain(m *testing.M) {
	keyring.MockInit() // cofre em memoria: os testes nao tocam no Credential Manager
	os.Exit(m.Run())
}

// newTestApp monta o App com configuracao temporaria e conexao simulada.
func newTestApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.json")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	app := New(nil, out, errOut)
	app.ConfigPath = path
	app.NoColor = true
	app.Prompter = NewPrompter(nil, errOut)
	return app, out, errOut, path
}

// seed grava uma configuracao inicial.
func seed(t *testing.T, path string, hosts ...config.Host) {
	t.Helper()
	store := &config.Store{Path: path}
	cfg := config.New()
	cfg.Hosts = hosts
	if err := store.Save(cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func readConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := (&config.Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestListShowsGroupedTable(t *testing.T) {
	app, out, _, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root", Group: "cliente-a", Tags: []string{"prod"}},
		config.Host{ID: 2, Name: "beta", Host: "10.0.0.2", User: "deploy", Port: 2222},
	)

	if code := app.Run([]string{"list"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, app.Err)
	}
	text := out.String()
	if !strings.Contains(text, "[cliente-a]") || !strings.Contains(text, "["+config.NoGroupLabel+"]") {
		t.Fatalf("saida agrupada inesperada:\n%s", text)
	}
	if !strings.Contains(text, "alfa") || !strings.Contains(text, "2222") {
		t.Fatalf("hosts ausentes:\n%s", text)
	}
}

func TestListFlatAndFilters(t *testing.T) {
	app, out, _, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root", Group: "cliente-a", Tags: []string{"prod"}},
		config.Host{ID: 2, Name: "beta", Host: "10.0.0.2", User: "deploy", Group: "cliente-b", Tags: []string{"homolog"}},
	)

	app.Run([]string{"list", "--flat"})
	if !strings.Contains(out.String(), "GRUPO") {
		t.Fatalf("--flat deveria manter a coluna GRUPO:\n%s", out.String())
	}

	out.Reset()
	app.Run([]string{"list", "-g", "cliente-b"})
	if strings.Contains(out.String(), "alfa") || !strings.Contains(out.String(), "beta") {
		t.Fatalf("filtro por grupo falhou:\n%s", out.String())
	}

	out.Reset()
	app.Run([]string{"list", "-t", "prod"})
	if !strings.Contains(out.String(), "alfa") || strings.Contains(out.String(), "beta") {
		t.Fatalf("filtro por tag falhou:\n%s", out.String())
	}
}

func TestListAliasesAndEmptyStates(t *testing.T) {
	app, out, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	for _, alias := range []string{"list", "ls", "-l"} {
		out.Reset()
		if code := app.Run([]string{alias}); code != 0 {
			t.Fatalf("%s: codigo %d", alias, code)
		}
		if !strings.Contains(out.String(), "alfa") {
			t.Fatalf("%s nao listou o host: %s", alias, out.String())
		}
	}

	// filtro sem resultado
	if code := app.Run([]string{"list", "-g", "nao-existe"}); code != 1 {
		t.Fatalf("filtro vazio deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "nenhum host encontrado") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}

	// configuracao vazia (mensagem em stderr, stdout limpo)
	empty := filepath.Join(t.TempDir(), "hosts.json")
	app.ConfigPath = empty
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"list"}); code != 0 {
		t.Fatalf("config vazia deveria sair com 0, obtido %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout deveria ficar limpo: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "nenhum host cadastrado") {
		t.Fatalf("mensagem inesperada: %q", errOut.String())
	}
}

func TestCreateNonInteractive(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	if code := app.Run([]string{
		"create", "-n", "novo host", "-H", "10.1.1.1", "-u", "root",
		"-p", "2200", "-g", "lab", "-t", "prod, sap",
	}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "conexao 'novohost' criada") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}

	cfg := readConfig(t, path)
	if len(cfg.Hosts) != 1 {
		t.Fatalf("hosts = %+v", cfg.Hosts)
	}
	host := cfg.Hosts[0]
	if host.ID != 1 || host.Name != "novohost" || host.Port != 2200 || host.Group != "lab" {
		t.Fatalf("host gravado errado: %+v", host)
	}
	if strings.Join(host.Tags, ",") != "prod,sap" {
		t.Fatalf("tags = %v", host.Tags)
	}
}

func TestCreateRejectsDuplicatesAndBadPort(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	if code := app.Run([]string{"create", "-n", "ALFA", "-H", "10.0.0.9", "-u", "root"}); code != 1 {
		t.Fatalf("nome duplicado deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "ja existe uma conexao chamada") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}

	errOut.Reset()
	if code := app.Run([]string{"create", "-n", "beta", "-H", "10.0.0.9", "-u", "root", "-p", "99999"}); code != 1 {
		t.Fatalf("porta invalida deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "porta fora da faixa") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}

	// sem terminal e sem -n: erro claro
	errOut.Reset()
	if code := app.Run([]string{"create", "-H", "10.0.0.9"}); code != 1 {
		t.Fatalf("sem nome deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "sem terminal interativo") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestEditByFlags(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root", Group: "antigo", Tags: []string{"a"}})

	if code := app.Run([]string{"edit", "alfa", "-n", "alfa2", "-g", "novo", "-t", "b,c", "-p", "2222"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	host := readConfig(t, path).Hosts[0]
	if host.Name != "alfa2" || host.Group != "novo" || host.Port != 2222 {
		t.Fatalf("host nao atualizado: %+v", host)
	}
	if strings.Join(host.Tags, ",") != "b,c" {
		t.Fatalf("tags = %v", host.Tags)
	}
	if !strings.Contains(errOut.String(), "nome alterado de 'alfa' para 'alfa2'") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestEditKeepsUnsetFieldsAndValidates(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root", Port: 2200})

	app.Run([]string{"edit", "1", "-u", "deploy"})
	host := readConfig(t, path).Hosts[0]
	if host.User != "deploy" || host.Host != "10.0.0.1" || host.Port != 2200 || host.Name != "alfa" {
		t.Fatalf("campos nao informados mudaram: %+v", host)
	}

	errOut.Reset()
	if code := app.Run([]string{"edit", "alfa", "-p", "70000"}); code != 1 {
		t.Fatalf("porta invalida deveria falhar, codigo = %d", code)
	}

	errOut.Reset()
	if code := app.Run([]string{"edit"}); code != 1 {
		t.Fatalf("edit sem host deveria falhar, codigo = %d", code)
	}

	errOut.Reset()
	if code := app.Run([]string{"edit", "nao-existe", "-u", "x"}); code != 1 {
		t.Fatalf("host inexistente deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "nenhum host encontrado") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestDeleteWithConfirmationFlag(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"},
		config.Host{ID: 2, Name: "beta", Host: "10.0.0.2", User: "root"},
	)

	if code := app.Run([]string{"delete", "alfa", "-y"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	cfg := readConfig(t, path)
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Name != "beta" {
		t.Fatalf("hosts = %+v", cfg.Hosts)
	}

	// sem -y e sem terminal: erro explicativo
	errOut.Reset()
	if code := app.Run([]string{"delete", "beta"}); code != 1 {
		t.Fatalf("sem confirmacao deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "use: sshm delete") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}

	// ultimo host apagado avisa
	errOut.Reset()
	app.Run([]string{"delete", "beta", "-y"})
	if !strings.Contains(errOut.String(), "nao ha mais conexoes") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestPasswdStatusTable(t *testing.T) {
	app, out, _, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 1, Name: "com-texto", Host: "h", User: "u", Password: "x"},
		config.Host{ID: 2, Name: "com-cofre", Host: "h", User: "u", PasswordSecret: "com-cofre"},
		config.Host{ID: 3, Name: "sem-senha", Host: "h", User: "u"},
	)

	if code := app.Run([]string{"passwd"}); code != 0 {
		t.Fatalf("codigo = %d", code)
	}
	text := out.String()
	if !strings.Contains(text, "texto puro no arquivo") || !strings.Contains(text, "cofre") || !strings.Contains(text, "sem senha") {
		t.Fatalf("tabela inesperada:\n%s", text)
	}
	if !strings.Contains(text, "Guardar ou trocar: sshm passwd <nome>") {
		t.Fatalf("rodape ausente:\n%s", text)
	}
}

func TestPasswdRemoveClearsFileAndVault(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "h", User: "u", PasswordSecret: "alfa"})
	if err := app.Vault.Set("alfa", "segredo"); err != nil {
		t.Fatal(err)
	}

	if code := app.Run([]string{"passwd", "alfa", "--remove"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	host := readConfig(t, path).Hosts[0]
	if host.Password != "" || host.PasswordSecret != "" {
		t.Fatalf("senha deveria ter sido limpa: %+v", host)
	}
	if _, err := app.Vault.Get("alfa"); err == nil {
		t.Fatal("senha deveria ter sido removida do cofre")
	}
}

func TestPasswdWithoutTerminalFails(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "h", User: "u"})

	if code := app.Run([]string{"passwd", "alfa"}); code != 1 {
		t.Fatalf("sem terminal deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "sem terminal interativo") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestMigrateSecretsMovesPlaintext(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "h", User: "u", Password: "em-texto"})

	if code := app.Run([]string{"config", "migrate-secrets"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	host := readConfig(t, path).Hosts[0]
	if host.Password != "" || host.PasswordSecret != "alfa" {
		t.Fatalf("host nao migrado: %+v", host)
	}
	secret, err := app.Vault.Get("alfa")
	if err != nil || secret != "em-texto" {
		t.Fatalf("segredo no cofre = %q (%v)", secret, err)
	}
}

func TestConnectSelectedUsesPickerAndConnection(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"},
		config.Host{ID: 2, Name: "beta", Host: "10.0.0.2", User: "root"},
	)

	var connected config.Host
	app.connect = func(host config.Host, args []string) (int, error) {
		connected = host
		return 0, nil
	}
	app.pick = func(rows []config.Row, grouped bool) (int, error) {
		if len(rows) != 2 || !grouped {
			t.Fatalf("linhas/agrupamento inesperados: %d %v", len(rows), grouped)
		}
		return 2, nil
	}

	if code := app.Run([]string{"select"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	if connected.Name != "beta" {
		t.Fatalf("host conectado = %+v", connected)
	}
}

func TestConnectSelectedCancelled(t *testing.T) {
	app, _, _, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})
	app.pick = func([]config.Row, bool) (int, error) { return 0, nil }
	app.connect = func(config.Host, []string) (int, error) {
		t.Fatal("nao deveria conectar depois de cancelar")
		return 0, nil
	}
	if code := app.Run([]string{"menu"}); code != 0 {
		t.Fatalf("cancelar deveria sair com 0, codigo = %d", code)
	}
}

func TestBareCommandConnectsAndPropagatesExitCode(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	var gotArgs []string
	app.connect = func(host config.Host, args []string) (int, error) {
		gotArgs = args
		return 7, nil
	}

	if code := app.Run([]string{"alfa", "-L", "8080:localhost:80", "--", "echo", "oi"}); code != 7 {
		t.Fatalf("codigo de saida remoto = %d, esperado 7", code)
	}
	if len(gotArgs) != 5 || gotArgs[0] != "-L" || gotArgs[2] != "--" {
		t.Fatalf("args repassados = %v", gotArgs)
	}

	errOut.Reset()
	if code := app.Run([]string{"nao-existe"}); code != 1 {
		t.Fatalf("host inexistente = %d", code)
	}
	if !strings.Contains(errOut.String(), "nenhum host encontrado") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestVersionHelpAndConfigCommands(t *testing.T) {
	app, out, _, path := newTestApp(t)

	if code := app.Run([]string{"version"}); code != 0 {
		t.Fatal("version deveria sair com 0")
	}
	if !strings.Contains(out.String(), Version) {
		t.Fatalf("saida = %q", out.String())
	}

	out.Reset()
	if code := app.Run([]string{"--help"}); code != 0 {
		t.Fatal("--help deveria sair com 0")
	}
	if !strings.Contains(out.String(), "Uso: sshm") {
		t.Fatalf("ajuda inesperada: %q", out.String())
	}

	out.Reset()
	if code := app.Run([]string{"config", "path"}); code != 0 {
		t.Fatal("config path deveria sair com 0")
	}
	if strings.TrimSpace(out.String()) != path {
		t.Fatalf("config path = %q, esperado %q", strings.TrimSpace(out.String()), path)
	}

	out.Reset()
	if code := app.Run([]string{"config", "show"}); code != 0 {
		t.Fatal("config show deveria sair com 0")
	}
	if !strings.Contains(out.String(), "\"hosts\"") {
		t.Fatalf("config show inesperado: %q", out.String())
	}

	if code := app.Run([]string{"config", "validate"}); code != 0 {
		t.Fatalf("config validate deveria sair com 0, obtido %d", code)
	}

	if code := app.Run([]string{"config", "nao-existe"}); code != 1 {
		t.Fatalf("subcomando invalido = %d", code)
	}
}

func TestConfigValidateReportsProblems(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path,
		config.Host{ID: 1, Name: "alfa", Host: "h", User: "u"},
		config.Host{ID: 2, Name: "alfa", Host: "h", User: "u"},
	)

	if code := app.Run([]string{"config", "validate"}); code != 1 {
		t.Fatalf("nome repetido deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "nome repetido") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestConfigValidateRejectsBrokenJSON(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	if err := os.WriteFile(path, []byte("{ nao e json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run([]string{"config", "validate"}); code != 1 {
		t.Fatalf("JSON invalido deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "JSON invalido") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestGlobalFlags(t *testing.T) {
	app, _, errOut, _ := newTestApp(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "outro.json")
	seed(t, target, config.Host{ID: 1, Name: "alfa", Host: "h", User: "u"})

	if code := app.Run([]string{"--config", target, "list"}); code != 0 {
		t.Fatalf("--config antes do comando: codigo %d (%s)", code, errOut.String())
	}
	if app.ConfigPath != target {
		t.Fatalf("ConfigPath = %q", app.ConfigPath)
	}

	errOut.Reset()
	if code := app.Run([]string{"--picker", "invalido", "list"}); code != 1 {
		t.Fatalf("picker invalido deveria falhar, codigo = %d", code)
	}
	if !strings.Contains(errOut.String(), "seletor invalido") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestGlobalFlagsAcceptedAfterCommand(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})
	app.pick = func([]config.Row, bool) (int, error) { return 1, nil }
	app.connect = func(config.Host, []string) (int, error) { return 0, nil }

	if code := app.Run([]string{"select", "--picker", "number", "--no-color"}); code != 0 {
		t.Fatalf("codigo = %d (%s)", code, errOut.String())
	}
	if app.PickerMode != "number" || !app.NoColor {
		t.Fatalf("opcoes globais nao aplicadas: %q %v", app.PickerMode, app.NoColor)
	}
}

func TestColorModeFlag(t *testing.T) {
	app, out, errOut, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root", Group: "lab"})

	// com a saida em buffer (nao terminal) 'auto' nao colore, mas 'always' colore
	if code := app.Run([]string{"list"}); code != 0 {
		t.Fatalf("list: %d (%s)", code, errOut.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("sem terminal as cores deveriam ficar desligadas:\n%q", out.String())
	}

	out.Reset()
	if code := app.Run([]string{"list", "--color=always"}); code != 0 {
		t.Fatalf("list --color=always: %d", code)
	}
	colored := out.String()
	if !strings.Contains(colored, "\x1b[1;36m[lab]") {
		t.Fatalf("--color=always deveria destacar o grupo:\n%q", colored)
	}
	if stripANSIForTest(colored) == "" {
		t.Fatal("saida colorida nao deveria ficar vazia")
	}

	out.Reset()
	if code := app.Run([]string{"list", "--color", "never"}); code != 0 {
		t.Fatalf("list --color never: %d", code)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("--color never deveria desligar as cores:\n%q", out.String())
	}

	out.Reset()
	if code := app.Run([]string{"list", "--no-color"}); code != 0 {
		t.Fatalf("list --no-color: %d", code)
	}
	if app.ColorMode != "never" {
		t.Fatalf("--no-color deveria equivaler a --color never: %q", app.ColorMode)
	}

	errOut.Reset()
	if code := app.Run([]string{"list", "--color"}); code != 1 {
		t.Fatalf("--color sem valor deveria falhar: %d", code)
	}
}

// stripANSIForTest remove as sequencias de cor para comparar o conteudo.
func stripANSIForTest(text string) string {
	return regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`).ReplaceAllString(text, "")
}

func TestShortFlagsKeepBashMeaning(t *testing.T) {
	// -c continua sendo o atalho de create (e nao --config)
	app, out, errOut, path := newTestApp(t)
	if code := app.Run([]string{"-c", "-n", "novo", "-H", "10.0.0.9", "-u", "root"}); code != 0 {
		t.Fatalf("-c deveria criar: %d (%s)", code, errOut.String())
	}
	if app.ConfigPath != path {
		t.Fatalf("ConfigPath mudou para %q", app.ConfigPath)
	}

	// -l continua sendo o atalho de list
	out.Reset()
	if code := app.Run([]string{"-l"}); code != 0 || !strings.Contains(out.String(), "novo") {
		t.Fatalf("-l deveria listar: %d %q", code, out.String())
	}
}

func TestDoubleDashProtectsRemoteArguments(t *testing.T) {
	app, _, _, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "10.0.0.1", User: "root"})

	var got []string
	app.connect = func(_ config.Host, args []string) (int, error) {
		got = args
		return 0, nil
	}
	if code := app.Run([]string{"alfa", "--", "echo", "--picker", "--config"}); code != 0 {
		t.Fatalf("codigo = %d", code)
	}
	if strings.Join(got, " ") != "-- echo --picker --config" {
		t.Fatalf("args repassados = %v", got)
	}
}

func TestSecretsDisabledFallsBackToPlaintextWarning(t *testing.T) {
	app, _, _, path := newTestApp(t)
	seed(t, path, config.Host{ID: 1, Name: "alfa", Host: "h", User: "u", PasswordSecret: "alfa"})
	t.Setenv(secrets.EnvKeyring, "off")
	if code := app.Run([]string{"config", "migrate-secrets"}); code != 1 {
		t.Fatalf("cofre desligado deveria falhar, codigo = %d", code)
	}
}

// O hosts.json gravado pelo PowerShell/Notepad costuma vir com BOM (ou UTF-16);
// 'sshm config normalize' reescreve o arquivo em UTF-8 sem BOM sem perder dados.
func TestConfigNormalizeConvertsBOM(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	body := []byte("{\n  \"hosts\": [{\"id\": 1, \"name\": \"alfa\", \"host\": \"h\", \"user\": \"u\", \"tags\": [\"t\"]}]\n}\n")
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, body...), 0o600); err != nil {
		t.Fatal(err)
	}

	// antes: o arquivo tem BOM e nem lista nem valida reclamam
	if code := app.Run([]string{"list"}); code != 0 {
		t.Fatalf("list com BOM deveria funcionar, codigo = %d (%s)", code, errOut.String())
	}

	errOut.Reset()
	if code := app.Run([]string{"config", "normalize"}); code != 0 {
		t.Fatalf("normalize: codigo = %d (%s)", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "convertido") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("o BOM deveria ter sido removido")
	}
	cfg := readConfig(t, path)
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Name != "alfa" || len(cfg.Hosts[0].Tags) != 1 {
		t.Fatalf("dados perdidos na conversao: %+v", cfg.Hosts)
	}

	// rodar de novo e idempotente
	errOut.Reset()
	if code := app.Run([]string{"config", "normalize"}); code != 0 {
		t.Fatalf("normalize repetido: %d", code)
	}
	if !strings.Contains(errOut.String(), "ja estava") {
		t.Fatalf("mensagem inesperada: %s", errOut.String())
	}
}

func TestConfigNormalizeConvertsUTF16(t *testing.T) {
	app, _, errOut, path := newTestApp(t)
	utf16Body := encodeUTF16ForTest("{\"hosts\":[{\"id\":3,\"name\":\"gama\",\"host\":\"h\",\"user\":\"u\"}]}")
	if err := os.WriteFile(path, utf16Body, 0o600); err != nil {
		t.Fatal(err)
	}

	// so o normalize entende o arquivo antes da conversao
	if code := app.Run([]string{"list"}); code != 0 {
		t.Fatalf("list com UTF-16 deveria funcionar, codigo = %d (%s)", code, errOut.String())
	}
	if code := app.Run([]string{"config", "normalize"}); code != 0 {
		t.Fatalf("normalize: %d (%s)", code, errOut.String())
	}
	cfg := readConfig(t, path)
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Name != "gama" {
		t.Fatalf("hosts = %+v", cfg.Hosts)
	}
}

// encodeUTF16ForTest grava em UTF-16LE com BOM, como o 'Out-File' do PowerShell.
func encodeUTF16ForTest(text string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, r := range text {
		if r < 0x10000 {
			out = append(out, byte(r), byte(r>>8))
			continue
		}
		r -= 0x10000
		high := 0xD800 + (r >> 10)
		low := 0xDC00 + (r & 0x3FF)
		out = append(out, byte(high), byte(high>>8), byte(low), byte(low>>8))
	}
	return out
}
