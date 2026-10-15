// Package cli implementa a linha de comando do sshm.
//
// Os comandos, apelidos, filtros e mensagens seguem o sshm.sh: o que era script
// bash virou um binario unico, sem depender de jq, fzf, secret-tool ou sshpass.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/szarblewski/ssh_manager/internal/ansi"
	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/picker"
	"github.com/szarblewski/ssh_manager/internal/secrets"
	"github.com/szarblewski/ssh_manager/internal/sshx"
)

// Version e a versao do sshm (pode ser trocada no build: -ldflags "-X ...").
var Version = "2.0.0"

// ExitError carrega um codigo de saida especifico (comando remoto, cancelamento).
type ExitError struct {
	Code    int
	Message string
}

// Error implementa a interface error.
func (e *ExitError) Error() string { return e.Message }

// App reune as dependencias da linha de comando.
type App struct {
	ConfigPath string
	PickerMode picker.Mode
	ColorMode  string // auto | always | never
	NoColor    bool
	Debug      bool

	In  *os.File
	Out io.Writer
	Err io.Writer

	Vault    secrets.Vault
	Prompter *Prompter

	showHelp    bool
	showVersion bool

	// connect abre a sessao SSH e devolve o codigo de saida (injetavel em testes).
	connect func(host config.Host, args []string) (int, error)
	// pick escolhe um host na lista (injetavel em testes).
	pick func(rows []config.Row, grouped bool) (int, error)
}

// New monta o App com as dependencias padrao.
func New(in *os.File, out, errOut io.Writer) *App {
	app := &App{
		In:         in,
		Out:        out,
		Err:        errOut,
		PickerMode: picker.EnvModeOrDefault(),
		Vault:      secrets.System(),
	}
	app.Prompter = NewPrompter(in, errOut)
	app.connect = app.session
	app.pick = app.pickHost
	return app
}

// Main executa o sshm e devolve o codigo de saida do processo.
func Main(args []string, in *os.File, out, errOut io.Writer) int {
	// conhost classico precisa ser avisado para interpretar as cores ANSI
	if file, ok := out.(*os.File); ok {
		_ = ansi.EnableVirtualTerminal(file)
	}
	return New(in, out, errOut).Run(args)
}

// Run interpreta os argumentos e executa o comando.
func (a *App) Run(args []string) int {
	rest, err := a.parseGlobals(args)
	if err != nil {
		return a.report(err)
	}
	if a.showHelp {
		a.printUsage()
		return 0
	}
	if a.showVersion {
		fmt.Fprintf(a.Out, "sshm %s\n", Version)
		return 0
	}
	if len(rest) == 0 {
		return a.report(a.cmdConnectSelected(nil))
	}

	command, commandArgs := rest[0], rest[1:]
	switch command {
	case "list", "ls", "-l":
		return a.report(a.cmdList(commandArgs))
	case "select", "-s", "menu", "pick":
		return a.report(a.cmdConnectSelected(commandArgs))
	case "create", "new", "-c", "--create":
		return a.report(a.cmdCreate(commandArgs))
	case "edit", "editar", "update", "--edit":
		return a.report(a.cmdEdit(commandArgs))
	case "delete", "del", "rm", "remove", "apagar", "--delete":
		return a.report(a.cmdDelete(commandArgs))
	case "passwd", "password", "senha", "pass":
		return a.report(a.cmdPasswd(commandArgs))
	case "config":
		return a.report(a.cmdConfig(commandArgs))
	case "migrate-secrets", "migrate":
		return a.report(a.cmdMigrateSecrets())
	case "uninstall":
		return a.report(a.cmdUninstall(commandArgs))
	case "version", "-V", "--version":
		fmt.Fprintf(a.Out, "sshm %s\n", Version)
		return 0
	case "help", "-h", "--help":
		a.printUsage()
		return 0
	default:
		return a.report(a.cmdConnect(command, commandArgs))
	}
}

// parseGlobals consome as opcoes que valem para qualquer comando.
//
// Elas podem vir antes ou depois do comando ("sshm select --picker number"),
// mas nunca depois de "--": dali em diante tudo pertence ao comando remoto.
func (a *App) parseGlobals(args []string) ([]string, error) {
	// estado de cada execucao (Run pode ser chamado mais de uma vez)
	a.showHelp = false
	a.showVersion = false

	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}

		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				return nil, errors.New("--config exige um arquivo")
			}
			i++
			a.ConfigPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			a.ConfigPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--picker":
			if i+1 >= len(args) {
				return nil, errors.New("--picker exige um valor (tui, number ou fzf)")
			}
			i++
			a.PickerMode = picker.Mode(args[i])
		case strings.HasPrefix(arg, "--picker="):
			a.PickerMode = picker.Mode(strings.TrimPrefix(arg, "--picker="))
		case arg == "--no-color":
			a.NoColor = true
			a.ColorMode = "never"
		case arg == "--color":
			if i+1 >= len(args) {
				return nil, errors.New("--color exige um valor (auto, always ou never)")
			}
			i++
			a.ColorMode = args[i]
		case strings.HasPrefix(arg, "--color="):
			a.ColorMode = strings.TrimPrefix(arg, "--color=")
		case arg == "--debug":
			a.Debug = true
		case arg == "--help" || arg == "-h":
			a.showHelp = true
		case arg == "--version" || arg == "-V":
			a.showVersion = true
		default:
			rest = append(rest, arg)
		}
	}
	if !picker.ValidMode(a.PickerMode) {
		return nil, fmt.Errorf("seletor invalido: %q (use tui, number ou fzf)", a.PickerMode)
	}
	return rest, nil
}

// report imprime o erro (e a mensagem de um ExitError) e devolve o codigo.
func (a *App) report(err error) int {
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		if exit.Message != "" {
			fmt.Fprintf(a.Err, "sshm: %s\n", exit.Message)
		}
		return exit.Code
	}
	fmt.Fprintf(a.Err, "sshm: %v\n", err)
	return 1
}

// openConfig carrega o hosts.json, criando um arquivo vazio quando nao existe.
func (a *App) openConfig() (*config.Store, *config.Config, error) {
	store, cfg, created, err := config.Open(a.ConfigPath)
	if err != nil {
		return nil, nil, err
	}
	if created {
		fmt.Fprintf(a.Err, "sshm: nenhum cadastro encontrado; criado %s (adicione conexoes com 'sshm create')\n", store.Path)
	}
	return store, cfg, nil
}

// pickOptions monta as opcoes do seletor.
func (a *App) pickOptions() picker.Options {
	opts := picker.DefaultOptions()
	opts.Mode = a.PickerMode
	opts.InFile = a.In
	opts.Out = a.Out
	opts.Err = a.Err
	opts.Style = a.style()
	if a.In != nil {
		opts.In = a.In
	}
	return opts
}

// pickHost pergunta qual host usar (usada pelo comando select).
func (a *App) pickHost(rows []config.Row, grouped bool) (int, error) {
	opts := a.pickOptions()
	opts.Grouped = grouped
	return picker.Pick(rows, opts)
}

func (a *App) printUsage() {
	fmt.Fprintf(a.Out, `Uso: sshm                              (selecao interativa)
     sshm select [-g <grupo>] [-t <tag>] [--flat]   (atalhos: -s, menu)
     sshm menu [--flat]                     (lista navegavel: setas/j k, Enter conecta, q sai)
     sshm create [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
                 [-g grupo] [-t tags] [-P]   (atalhos: new, --create, -c)
     sshm passwd [<id|nome>] [--remove] [--plain]
                                            (senha guardada; atalhos: senha, pass)
     sshm edit <id|nome> [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
                         [-g grupo] [-t tags]   (atalhos: editar, update)
     sshm delete <id|nome> [-y]              (atalhos: del, rm, remove, apagar)
     sshm <numero|nome> [opcoes do ssh] [-- comando]
     sshm list [-g <grupo>] [-t <tag>] [--flat]
     sshm ls | -l
     sshm config path|show|init|validate|normalize|migrate-secrets
     sshm version
     sshm uninstall [--purge-config]

Opcoes gerais:
     --config <arquivo>   usa outro hosts.json
     --picker tui|number|fzf  forca o seletor (equivale a SSHM_PICKER)
     --color auto|always|never  cores na saida (padrao: auto)
     --no-color           equivale a --color never
     --debug              mostra detalhes de diagnostico

Opcoes aceitas depois do host (como no ssh):
     -L [bind:]porta:host:porta   encaminhamento local
     -J [usuario@]host[:porta]    bastiao (ProxyJump)
     -o Chave=Valor                StrictHostKeyChecking, UserKnownHostsFile,
                                   IdentityFile, Port, User, ProxyJump,
                                   ConnectTimeout, ServerAliveInterval,
                                   ServerAliveCountMax, BatchMode, LogLevel
     -i chave   -p porta   -l usuario   -N   -T   -t   -v
     -- comando remoto   (sem '--', o primeiro argumento livre tambem vale)

Listagem e selecao separam os hosts por grupo (alfabetica, sem grupo no fim);
--flat volta a tabela unica com a coluna GRUPO.

Config atual: %s
Procura nesta ordem: --config, $%s, %s,
%s, hosts.json na pasta atual.
SSHM_PICKER=tui|number|fzf forca o seletor da selecao interativa.
SSHM_STRICT_HOSTKEY=1 pede confirmacao manual da chave de host na 1a conexao.
SSHM_KEYRING=off desliga o cofre do sistema (senhas ficam em texto puro).
`, a.configPathForDisplay(), config.EnvConfig,
		config.UserConfigDir()+string(os.PathSeparator)+config.FileName,
		config.LegacyConfigDir()+string(os.PathSeparator)+config.FileName)
}

func (a *App) configPathForDisplay() string {
	if path, ok := config.Resolve(a.ConfigPath); ok {
		return path
	}
	return config.CreatePath(a.ConfigPath) + " (ainda nao existe)"
}

// session executa o comando remoto no host indicado.
func (a *App) session(host config.Host, extra []string) (int, error) {
	parsed, err := sshx.ParseArgs(extra)
	if err != nil {
		return 0, err
	}
	for _, warning := range parsed.Warnings {
		a.info("%s", warning)
	}

	password, warnings := secrets.Lookup(a.Vault, host)
	for _, warning := range warnings {
		a.info("%s", warning)
	}

	opts := parsed.Options
	opts.Password = password
	opts.Interactive = sshx.IsTerminal(a.In)
	if !opts.StrictHostKeySet {
		opts.StrictHostKey = strictHostKeyEnv()
	}

	spec := sshx.Spec{
		Name:         host.Name,
		Host:         host.Host,
		User:         host.User,
		Port:         host.PortOrDefault(),
		IdentityFile: host.IdentityFile,
		ProxyJump:    host.ProxyJump,
	}

	dialer := sshx.NewDialer(spec, opts, a.Prompter, a.Err)
	defer dialer.Close()

	if host.Group != "" {
		a.info("conectando em %s:%d [grupo: %s]...", host.Target(), host.PortOrDefault(), host.Group)
	} else {
		a.info("conectando em %s:%d...", host.Target(), host.PortOrDefault())
	}
	if password != "" {
		a.info("usando a senha guardada de '%s' (se estiver errada: sshm passwd '%s')", host.Name, host.Name)
	}

	client, err := dialer.Dial(a.context())
	if err != nil {
		return 255, err
	}
	defer client.Close()

	stop := func() {}
	if len(opts.LocalForwards) > 0 || opts.NoRemoteCommand {
		if stop, err = dialer.StartLocalForwards(client); err != nil {
			return 255, err
		}
		defer stop()
	}
	if opts.NoRemoteCommand {
		dialer.WaitWithoutShell(client)
		return 0, nil
	}

	return dialer.Run(client, parsed.Command, a.In, a.Out, a.Err)
}
