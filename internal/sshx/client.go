package sshx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// DefaultConnectTimeout limita a espera pelo handshake quando o host nao diz nada.
const DefaultConnectTimeout = 20 * time.Second

// Spec descreve a conexao de um host cadastrado.
type Spec struct {
	Name           string
	Host           string
	User           string
	Port           int
	IdentityFile   string
	ProxyJump      string
	ConnectTimeout int
}

// Addr devolve "host:porta" ja com a porta padrao aplicada.
func (s Spec) Addr() string {
	port := s.Port
	if port <= 0 {
		port = config.DefaultPort
	}
	return net.JoinHostPort(s.Host, strconv.Itoa(port))
}

// Dialer conecta no host aplicando as opcoes extras do comando.
type Dialer struct {
	Spec   Spec
	Opts   Options
	Prompt Prompter
	Stderr io.Writer

	agentConn net.Conn
}

// NewDialer monta o conector com os valores padrao de usuario e porta.
func NewDialer(spec Spec, opts Options, prompt Prompter, stderr io.Writer) *Dialer {
	if spec.Port <= 0 {
		spec.Port = config.DefaultPort
	}
	if spec.User == "" {
		spec.User = currentUser()
	}
	if opts.User != "" {
		spec.User = opts.User
	}
	if opts.Port > 0 {
		spec.Port = opts.Port
	}
	if len(opts.ProxyJumps) == 0 && spec.ProxyJump != "" {
		opts.ProxyJumps = splitJumps(spec.ProxyJump)
	}
	if prompt == nil {
		prompt = NopPrompter{}
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &Dialer{Spec: spec, Opts: opts, Prompt: prompt, Stderr: stderr}
}

// Close libera recursos (conexao com o agente).
func (d *Dialer) Close() error {
	if d.agentConn != nil {
		err := d.agentConn.Close()
		d.agentConn = nil
		return err
	}
	return nil
}

// Target devolve "usuario@host:porta" para as mensagens.
func (d *Dialer) Target() string {
	return fmt.Sprintf("%s@%s", d.Spec.User, d.Spec.Addr())
}

func (d *Dialer) logf(format string, args ...any) {
	if d.Stderr == nil {
		return
	}
	fmt.Fprintf(d.Stderr, "sshm: "+format+"\n", args...)
}

// debugf so escreve com -v ligado.
func (d *Dialer) debugf(format string, args ...any) {
	if d.Opts.Verbosity > 0 {
		d.logf(format, args...)
	}
}

func (d *Dialer) timeout() time.Duration {
	if d.Opts.ConnectTimeout > 0 {
		return d.Opts.ConnectTimeout
	}
	if d.Spec.ConnectTimeout > 0 {
		return time.Duration(d.Spec.ConnectTimeout) * time.Second
	}
	return DefaultConnectTimeout
}

// Dial abre a conexao SSH (passando pelos bastiões, quando houver).
func (d *Dialer) Dial(ctx context.Context) (*ssh.Client, error) {
	cfg, err := d.clientConfig()
	if err != nil {
		return nil, err
	}
	target := d.Spec.Addr()

	conn, err := d.dialTransport(ctx, target)
	if err != nil {
		return nil, err
	}

	if err := conn.SetDeadline(time.Now().Add(d.timeout())); err != nil {
		d.debugf("depuracao: nao foi possivel definir o prazo da conexao: %v", err)
	}
	clientConn, channels, requests, err := ssh.NewClientConn(conn, target, cfg)
	if err != nil {
		conn.Close()
		return nil, normalizeHandshakeError(err, target)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		d.debugf("depuracao: nao foi possivel limpar o prazo da conexao: %v", err)
	}

	client := ssh.NewClient(clientConn, channels, requests)
	d.startKeepAlive(client)
	return client, nil
}

func (d *Dialer) clientConfig() (*ssh.ClientConfig, error) {
	auths, err := d.authMethods()
	if err != nil {
		return nil, err
	}
	hostKey, err := knownHostsCallback(d.Opts.KnownHostsFiles, d.Opts.StrictHostKey, d.Prompt)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            d.Spec.User,
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         d.timeout(),
	}
	// O x/crypto/ssh nao expoe mais o log interno (o campo Config.Log foi
	// removido), entao -v mostra o que o sshm sabe: destino, chaves e metodos.
	if d.Opts.Verbosity > 0 {
		d.debugf("depuracao: conectando em %s como %q com %d metodo(s) de autenticacao",
			d.Spec.Addr(), d.Spec.User, len(auths))
	}
	return cfg, nil
}

// dialTransport devolve a conexao TCP ate o destino, atravessando os bastiões.
func (d *Dialer) dialTransport(ctx context.Context, target string) (net.Conn, error) {
	if len(d.Opts.ProxyJumps) == 0 {
		return d.dialTCP(ctx, target)
	}

	var jump *ssh.Client
	for _, jumpSpec := range d.Opts.ProxyJumps {
		addr, jumpUser, err := parseJumpSpec(jumpSpec, d.Spec)
		if err != nil {
			return nil, err
		}

		var conn net.Conn
		if jump == nil {
			conn, err = d.dialTCP(ctx, addr)
		} else {
			conn, err = jump.Dial("tcp", addr)
		}
		if err != nil {
			return nil, fmt.Errorf("falha ao conectar no bastiao %s: %w", addr, err)
		}

		jumpSpecCopy := d.Spec
		jumpSpecCopy.Host, _ = splitHostPort(addr)
		jumpSpecCopy.User = jumpUser
		if jumpUser == "" {
			jumpSpecCopy.User = d.Spec.User
		}
		jumpOpts := d.Opts
		jumpOpts.ProxyJumps = nil // a cadeia ja esta sendo percorrida aqui
		jumpDialer := &Dialer{
			Spec:      jumpSpecCopy,
			Opts:      jumpOpts,
			Prompt:    d.Prompt,
			Stderr:    d.Stderr,
			agentConn: d.agentConn,
		}
		cfg, err := jumpDialer.clientConfig()
		if err != nil {
			return nil, err
		}
		conn.SetDeadline(time.Now().Add(d.timeout()))
		clientConn, channels, requests, err := ssh.NewClientConn(conn, addr, cfg)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("falha ao autenticar no bastiao %s: %w", addr, normalizeHandshakeError(err, addr))
		}
		conn.SetDeadline(time.Time{})
		jump = ssh.NewClient(clientConn, channels, requests)
		d.logf("conectado ao bastiao %s", addr)
	}
	return jump.Dial("tcp", target)
}

func (d *Dialer) dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: d.timeout()}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("nao foi possivel conectar em %s: %w", addr, err)
	}
	return conn, nil
}

// startKeepAlive mantem a sessao viva quando o host pede ServerAliveInterval.
func (d *Dialer) startKeepAlive(client *ssh.Client) {
	if d.Opts.ServerAliveInterval <= 0 {
		return
	}
	max := d.Opts.ServerAliveCountMax
	if max <= 0 {
		max = 3
	}
	go func() {
		failures := 0
		ticker := time.NewTicker(d.Opts.ServerAliveInterval)
		defer ticker.Stop()
		for range ticker.C {
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				failures++
				d.debugf("depuracao: keepalive falhou (%d/%d): %v", failures, max, err)
				if failures >= max {
					d.logf("conexao perdida (sem resposta ao keepalive)")
					client.Close()
					return
				}
				continue
			}
			failures = 0
		}
	}()
}

// Run abre a sessao (shell ou comando remoto) e devolve o codigo de saida.
//
// O pseudo-terminal e pedido quando a sessao e interativa (igual ao ssh), ou
// sempre que -t for usado; -T desliga o terminal.
func (d *Dialer) Run(client *ssh.Client, command []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		return 0, fmt.Errorf("nao foi possivel abrir a sessao: %w", err)
	}
	defer session.Close()

	useTTY := d.useTTY(len(command) > 0)
	terminal := LocalTerminal()
	if useTTY {
		width, height := TerminalSize(terminal)
		if width <= 0 {
			width = 80
		}
		if height <= 0 {
			height = 24
		}
		modes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty(TermType(), height, width, modes); err != nil {
			return 0, fmt.Errorf("nao foi possivel pedir o terminal ao servidor: %w", err)
		}
	}

	restore := func() {}
	if useTTY && terminal != nil && stdin == os.Stdin {
		var err error
		if restore, err = RawMode(terminal); err != nil {
			d.debugf("depuracao: terminal nao entrou em modo bruto: %v", err)
			restore = func() {}
		}
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	if len(command) > 0 {
		if err := session.Start(strings.Join(command, " ")); err != nil {
			restore()
			return 0, fmt.Errorf("nao foi possivel executar o comando remoto: %w", err)
		}
	} else {
		if err := session.Shell(); err != nil {
			restore()
			return 0, fmt.Errorf("nao foi possivel abrir o shell remoto: %w", err)
		}
	}

	done := make(chan struct{})
	if useTTY && terminal != nil {
		go watchWindowSize(session, terminal, done)
	}

	err = session.Wait()
	close(done)
	restore()

	if code, ok := ExitStatus(err); ok {
		return code, nil
	}
	return 0, err
}

// WaitWithoutShell mantem a conexao aberta (-N) ate ela ser encerrada
// (Ctrl+C fecha o cliente).
func (d *Dialer) WaitWithoutShell(client *ssh.Client) {
	if err := client.Wait(); err != nil {
		d.debugf("depuracao: conexao encerrada: %v", err)
	}
}

// useTTY decide se a sessao pede pseudo-terminal.
func (d *Dialer) useTTY(hasCommand bool) bool {
	switch {
	case d.Opts.DisableTTY:
		return false
	case d.Opts.ForceTTY:
		return true
	case hasCommand:
		return false
	default:
		return d.Opts.Interactive
	}
}

// watchWindowSize avisa o servidor quando o terminal local muda de tamanho.
func watchWindowSize(session *ssh.Session, terminal *os.File, done <-chan struct{}) {
	lastWidth, lastHeight := TerminalSize(terminal)
	trigger := resizeTrigger(done)
	for {
		select {
		case <-done:
			return
		case <-trigger:
			width, height := TerminalSize(terminal)
			if width <= 0 || height <= 0 || (width == lastWidth && height == lastHeight) {
				continue
			}
			lastWidth, lastHeight = width, height
			if err := session.WindowChange(height, width); err != nil {
				return
			}
		}
	}
}

// ExitStatus extrai o codigo de saida do comando remoto.
//
// ok=false quando o erro nao e uma saida normal (conexao caiu, por exemplo).
func ExitStatus(err error) (code int, ok bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus(), true
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return 255, true
	}
	return 0, false
}

// StartLocalForwards abre os listening sockets de -L e devolve a funcao que os
// encerra.
func (d *Dialer) StartLocalForwards(client *ssh.Client) (func(), error) {
	var listeners []net.Listener
	stop := func() {
		for _, listener := range listeners {
			listener.Close()
		}
	}
	for _, forward := range d.Opts.LocalForwards {
		bindHost := forward.BindHost
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
		bind := net.JoinHostPort(bindHost, strconv.Itoa(forward.BindPort))
		listener, err := net.Listen("tcp", bind)
		if err != nil {
			stop()
			return nil, fmt.Errorf("nao foi possivel escutar em %s: %w", bind, err)
		}
		listeners = append(listeners, listener)
		d.logf("encaminhando %s para %s:%d (pelo servidor remoto)", bind, forward.DestHost, forward.DestPort)
		go acceptForward(listener, client, forward, d.Stderr)
	}
	return stop, nil
}

func acceptForward(listener net.Listener, client *ssh.Client, forward Forward, stderr io.Writer) {
	for {
		local, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer local.Close()
			remote, err := client.Dial("tcp", net.JoinHostPort(forward.DestHost, strconv.Itoa(forward.DestPort)))
			if err != nil {
				fmt.Fprintf(stderr, "sshm: encaminhamento %s falhou: %v\n", forward, err)
				return
			}
			defer remote.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
			go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
			<-done
		}()
	}
}

// parseJumpSpec interpreta "[usuario@]host[:porta]" de um bastiao.
func parseJumpSpec(jump string, spec Spec) (addr, username string, err error) {
	host := strings.TrimSpace(jump)
	if host == "" {
		return "", "", errors.New("bastiao (-J) vazio")
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		username = host[:at]
		host = host[at+1:]
	}
	port := spec.Port
	if port <= 0 {
		port = config.DefaultPort
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	} else if strings.Count(host, ":") == 1 {
		// host:porta sem brackets
		parts := strings.SplitN(host, ":", 2)
		if parsed, err := strconv.Atoi(parts[1]); err == nil {
			host = parts[0]
			port = parsed
		}
	}
	if host == "" {
		return "", "", fmt.Errorf("bastiao invalido: %q", jump)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), username, nil
}

func splitHostPort(addr string) (string, string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return host, port
}

// normalizeHandshakeError troca erros cripticos do handshake por algo acionavel.
func normalizeHandshakeError(err error, target string) error {
	msg := err.Error()
	if strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain") {
		return fmt.Errorf("nao foi possivel autenticar em %s: %w "+
			"(verifique a chave em 'sshm edit' ou guarde a senha com 'sshm passwd')", target, err)
	}
	return fmt.Errorf("falha no handshake SSH com %s: %w", target, err)
}

// currentUser devolve o usuario local (usado como padrao no cadastro).
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		if idx := strings.LastIndexAny(name, `\/`); idx >= 0 {
			name = name[idx+1:]
		}
		return name
	}
	for _, env := range []string{"USER", "USERNAME", "LOGNAME"} {
		if value := os.Getenv(env); value != "" {
			return value
		}
	}
	return ""
}

// CurrentUser expoe o usuario local para o cadastro de novos hosts.
func CurrentUser() string { return currentUser() }
