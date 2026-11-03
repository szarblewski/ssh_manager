package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/sshx"
	"github.com/szarblewski/ssh_manager/internal/transfer"
)

// cmdCopy copia arquivos entre a maquina local e os hosts cadastrados
// (ou entre dois hosts), no estilo do scp.
//
// A origem e o destino sao caminhos locais ou "host:/caminho", onde "host" e o
// nome (ou o id) de um cadastro do sshm.
func (a *App) cmdCopy(args []string) error {
	parsed, err := parseCopyArgs(args)
	if err != nil {
		return err
	}
	if len(parsed.targets) < 2 {
		return errors.New("uso: sshm cp [-r] [-p] <origem>... <destino>")
	}

	_, cfg, err := a.openConfig()
	if err != nil {
		return err
	}

	targets := make([]cpTarget, 0, len(parsed.targets))
	for _, spec := range parsed.targets {
		target, err := parseCopyTarget(cfg, spec)
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}
	sources, destination := targets[:len(targets)-1], targets[len(targets)-1]

	sessions := newCopySessions(a)
	defer sessions.Close()

	dstEndpoint, err := sessions.endpoint(destination)
	if err != nil {
		return err
	}
	if len(sources) > 1 {
		info, err := dstEndpoint.Lstat(destination.path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%s precisa ser um diretorio para receber %d origens", destination, len(sources))
		}
	}

	progress := newCopyProgress(a.Err, parsed.quiet)
	defer progress.Finish()

	opts := parsed.options
	opts.Progress = progress.Update
	if !parsed.quiet {
		opts.Logf = func(format string, args ...any) {
			progress.Clear()
			a.info(format, args...)
		}
	}

	var total transfer.Result
	for _, source := range sources {
		srcEndpoint, err := sessions.endpoint(source)
		if err != nil {
			return err
		}
		result, err := transfer.Copy(srcEndpoint, source.path, dstEndpoint, destination.path, opts)
		total.Add(result)
		if err != nil {
			return err
		}
	}

	if opts.Logf != nil {
		opts.Logf("concluido: %d arquivo(s), %d diretorio(s), %d link(s), %s",
			total.Files, total.Dirs, total.Links, transfer.HumanSize(total.Bytes))
	}
	return nil
}

// cpArgs e o resultado do parser de 'sshm cp'.
type cpArgs struct {
	options transfer.Options
	quiet   bool
	targets []string
}

// parseCopyArgs separa as opcoes (-r, -p, -q) dos caminhos.
//
// As opcoes podem vir em qualquer posicao (antes ou depois dos caminhos) e
// agrupadas (-rp); depois de "--" tudo e caminho.
func parseCopyArgs(argv []string) (cpArgs, error) {
	var out cpArgs
	endOfFlags := false

	for _, arg := range argv {
		switch {
		case endOfFlags || arg == "" || arg == "-" || !strings.HasPrefix(arg, "-"):
			out.targets = append(out.targets, arg)
		case arg == "--":
			endOfFlags = true
		case strings.HasPrefix(arg, "--"):
			switch arg {
			case "--recursive":
				out.options.Recursive = true
			case "--preserve":
				out.options.Preserve = true
			case "--quiet":
				out.quiet = true
			default:
				return out, fmt.Errorf("opcao desconhecida para cp: %s (use -r, -p ou -q)", arg)
			}
		default:
			for _, flag := range arg[1:] {
				switch flag {
				case 'r':
					out.options.Recursive = true
				case 'p':
					out.options.Preserve = true
				case 'q':
					out.quiet = true
				default:
					return out, fmt.Errorf("opcao desconhecida para cp: -%c (use -r, -p ou -q)", flag)
				}
			}
		}
	}
	return out, nil
}

// cpTarget e um dos lados da copia: um caminho local ou "host:/caminho".
type cpTarget struct {
	remote bool
	host   config.Host
	path   string
	spec   string
}

func (t cpTarget) String() string {
	if !t.remote {
		return t.path
	}
	return t.host.Name + ":" + t.path
}

// parseCopyTarget decide se spec aponta para um host cadastrado ou para um
// caminho local.
//
// Um caminho local com ":" (C:\pasta, ou um arquivo cujo nome tem ":") nao vira
// host: so e remoto quando a parte antes do ":" e um cadastro do sshm.
func parseCopyTarget(cfg *config.Config, spec string) (cpTarget, error) {
	if strings.TrimSpace(spec) == "" {
		return cpTarget{}, errors.New("caminho vazio na copia")
	}

	prefix, rest, hasColon := strings.Cut(spec, ":")
	if !hasColon || prefix == "" || looksLikeDrivePath(prefix, rest) {
		return cpTarget{path: config.ExpandPath(spec), spec: spec}, nil
	}

	switch matches := cfg.FindAll(prefix); len(matches) {
	case 1:
		return cpTarget{remote: true, host: cfg.Hosts[matches[0]], path: rest, spec: spec}, nil
	case 0:
		if _, err := os.Stat(config.ExpandPath(spec)); err == nil {
			return cpTarget{path: config.ExpandPath(spec), spec: spec}, nil
		}
		return cpTarget{}, fmt.Errorf("host %q nao esta cadastrado (use 'sshm list' ou 'sshm create')", prefix)
	default:
		return cpTarget{}, fmt.Errorf("o nome %q casa com mais de um host; use o id no lugar", prefix)
	}
}

// remoteSpecPrefix separa "host:/caminho" quando "host" e um cadastro do sshm.
//
// Devolve ok=false para caminhos locais, para um host desconhecido e para
// "C:\pasta" (que tem ":" mas nao e host).
func remoteSpecPrefix(cfg *config.Config, spec string) (prefix, rest string, ok bool) {
	prefix, rest, hasColon := strings.Cut(spec, ":")
	if !hasColon || prefix == "" || looksLikeDrivePath(prefix, rest) {
		return "", "", false
	}
	if len(cfg.FindAll(prefix)) != 1 {
		return "", "", false
	}
	return prefix, rest, true
}

// looksLikeCopy reconhece a copia sem o nome do comando ("sshm a:/x b:/y"),
// igual ao scp: pelo menos dois argumentos, nenhum deles opcao, o primeiro nao
// e um host e algum deles aponta para um host cadastrado.
func (a *App) looksLikeCopy(args []string) bool {
	if len(args) < 2 {
		return false
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return false
		}
	}
	_, cfg, err := a.openConfig()
	if err != nil {
		return false
	}
	if len(cfg.FindAll(args[0])) > 0 {
		return false // "sshm host comando" e conexao, nao copia
	}
	for _, arg := range args {
		if _, _, ok := remoteSpecPrefix(cfg, arg); ok {
			return true
		}
	}
	return false
}

// looksLikeDrivePath reconhece "C:\..." e "C:/..." (senao o ":" seria lido como
// separador de host).
func looksLikeDrivePath(prefix, rest string) bool {
	if len(prefix) != 1 {
		return false
	}
	c := prefix[0]
	if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
		return false
	}
	return strings.HasPrefix(rest, `\`) || strings.HasPrefix(rest, "/")
}

// copySessions reaproveita as conexoes: uma por host, aberta sob demanda.
type copySessions struct {
	app    *App
	local  transfer.Endpoint
	remote map[string]*copySession
	opened []*copySession
}

type copySession struct {
	dialer *sshx.Dialer
	client *ssh.Client
	sftp   *sftp.Client
}

func newCopySessions(a *App) *copySessions {
	return &copySessions{
		app:    a,
		local:  transfer.Local(),
		remote: map[string]*copySession{},
	}
}

// endpoint devolve o lado da copia, conectando no host na primeira vez.
func (s *copySessions) endpoint(target cpTarget) (transfer.Endpoint, error) {
	if !target.remote {
		return s.local, nil
	}
	session, ok := s.remote[target.host.Name]
	if !ok {
		opened, err := s.open(target.host)
		if err != nil {
			return nil, err
		}
		session = opened
		s.remote[target.host.Name] = session
		s.opened = append(s.opened, session)
	}
	return transfer.Remote(target.host.Name, session.sftp), nil
}

func (s *copySessions) open(host config.Host) (*copySession, error) {
	dialer, password := s.app.hostDialer(host, sshx.Options{})
	s.app.info("conectando em %s:%d...", host.Target(), host.PortOrDefault())
	if password != "" {
		s.app.info("usando a senha guardada de '%s' (se estiver errada: sshm passwd '%s')", host.Name, host.Name)
	}

	client, err := dialer.Dial(s.app.context())
	if err != nil {
		dialer.Close()
		return nil, err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		dialer.Close()
		return nil, fmt.Errorf("%s nao aceitou o subsistema sftp: %w", host.Name, err)
	}
	return &copySession{dialer: dialer, client: client, sftp: sftpClient}, nil
}

// Close encerra as sessoes abertas (sftp, cliente e agente).
func (s *copySessions) Close() {
	for _, session := range s.opened {
		_ = session.sftp.Close()
		_ = session.client.Close()
		_ = session.dialer.Close()
	}
	s.opened = nil
	s.remote = map[string]*copySession{}
}
