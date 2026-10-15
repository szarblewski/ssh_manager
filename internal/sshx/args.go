// Package sshx implementa o cliente SSH nativo do sshm.
//
// O sshm.sh entregava os argumentos extras para o ssh do sistema; aqui o cliente
// e proprio (golang.org/x/crypto/ssh), entao os flags usados com mais frequencia
// sao aceitos diretamente e o restante e recusado com uma mensagem explicativa.
package sshx

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Forward descreve um encaminhamento local (-L).
type Forward struct {
	BindHost string
	BindPort int
	DestHost string
	DestPort int
}

// String devolve o encaminhamento no formato [bind:]porta:host:porta.
func (f Forward) String() string {
	bind := f.BindHost
	if bind == "" {
		bind = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d:%s:%d", bind, f.BindPort, f.DestHost, f.DestPort)
}

// Options sao as opcoes de conexao (flags extras + campos do host).
type Options struct {
	// IdentityFile substitui a chave do cadastro (-i).
	IdentityFile string
	// Port substitui a porta do cadastro (-p / -o Port).
	Port int
	// User substitui o usuario do cadastro (-l / -o User).
	User string
	// ProxyJumps guarda a cadeia de bastiões (-J / -o ProxyJump).
	ProxyJumps []string
	// LocalForwards sao os encaminhamentos locais (-L).
	LocalForwards []Forward
	// KnownHostsFiles substitui o known_hosts padrao (-o UserKnownHostsFile).
	KnownHostsFiles []string
	// StrictHostKey exige confirmacao manual da chave desconhecida.
	StrictHostKey bool
	// StrictHostKeySet indica que a opcao foi informada explicitamente.
	StrictHostKeySet bool
	// ConnectTimeout limita a espera pelo handshake (-o ConnectTimeout).
	ConnectTimeout time.Duration
	// ServerAliveInterval/Count enviam keepalive durante a sessao.
	ServerAliveInterval time.Duration
	ServerAliveCountMax int
	// Verbosity liga o log de depuracao (-v, -vv).
	Verbosity int
	// ForceTTY pede um pseudo-terminal mesmo com comando remoto (-t).
	ForceTTY bool
	// DisableTTY nao pede pseudo-terminal (-T).
	DisableTTY bool
	// NoRemoteCommand mantem a conexao aberta sem shell (-N).
	NoRemoteCommand bool
	// BatchMode nao pergunta nada no terminal (-o BatchMode=yes).
	BatchMode bool
	// Password e a senha guardada do host (quando houver).
	Password string
	// Interactive indica que a entrada padrao e um terminal (decide o PTY).
	Interactive bool
}

// Args e o resultado do parser de argumentos extras.
type Args struct {
	Options  Options
	Command  []string
	Warnings []string
}

// ParseArgs interpreta os argumentos extras passados depois do host.
//
// Aceita: -L, -J, -o (subconjunto), -i, -p, -l, -N, -T, -t, -v, -vv e o comando
// remoto (tudo que vier depois de "--" ou que nao comece com "-").
func ParseArgs(argv []string) (Args, error) {
	var out Args
	i := 0
	needsValue := map[string]string{
		"-L": "encaminhamento local",
		"-J": "bastiao (ProxyJump)",
		"-o": "opcao do ssh",
		"-i": "arquivo de chave",
		"-p": "porta",
		"-l": "usuario",
	}

	value := func(flag string) (string, error) {
		if i+1 >= len(argv) {
			return "", fmt.Errorf("a opcao %s exige um valor", flag)
		}
		i++
		return argv[i], nil
	}

	for ; i < len(argv); i++ {
		arg := argv[i]

		if arg == "--" {
			out.Command = append(out.Command, argv[i+1:]...)
			break
		}
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			// ssh trata o primeiro argumento nao-flag como inicio do comando remoto
			out.Command = append(out.Command, argv[i:]...)
			break
		}
		if description, ok := needsValue[arg]; ok {
			v, err := value(arg)
			if err != nil {
				return out, fmt.Errorf("%s (%s)", err.Error(), description)
			}
			if err := out.applyFlag(arg, v); err != nil {
				return out, err
			}
			continue
		}

		// -oKey=Value, -Lporta:host:porta, -p2222, -Jbastiao (valor colado)
		if len(arg) > 2 {
			if glued := arg[:2]; needsValue[glued] != "" || glued == "-o" {
				if err := out.applyFlag(glued, arg[2:]); err != nil {
					return out, err
				}
				continue
			}
		}
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			switch parts[0] {
			case "--identity-file":
				out.Options.IdentityFile = parts[1]
			case "--port":
				port, err := parsePort(parts[1])
				if err != nil {
					return out, err
				}
				out.Options.Port = port
			case "--proxy-jump":
				out.Options.ProxyJumps = splitJumps(parts[1])
			default:
				return out, fmt.Errorf("opcao desconhecida: %s", arg)
			}
			continue
		}

		switch arg {
		case "-v", "-vv", "-vvv":
			out.Options.Verbosity = len(arg) - 1
		case "-t":
			out.Options.ForceTTY = true
		case "-T":
			out.Options.DisableTTY = true
		case "-N":
			out.Options.NoRemoteCommand = true
		case "-q":
			out.Options.Verbosity = 0
		case "-C":
			out.Warnings = append(out.Warnings,
				"aviso: -C (compressao) nao e suportada pelo cliente nativo; seguindo sem compressao.")
		case "-A":
			out.Warnings = append(out.Warnings,
				"aviso: -A (agent forwarding) nao e suportado pelo cliente nativo; seguindo sem encaminhar o agente.")
		case "-R", "-D", "-X", "-Y":
			return out, fmt.Errorf("a opcao %s ainda nao e suportada pelo sshm (veja 'sshm --help')", arg)
		default:
			return out, fmt.Errorf("opcao desconhecida: %s (veja 'sshm --help')", arg)
		}
	}
	return out, nil
}

func (a *Args) applyFlag(flag, value string) error {
	switch flag {
	case "-L":
		forward, err := ParseForward(value)
		if err != nil {
			return err
		}
		a.Options.LocalForwards = append(a.Options.LocalForwards, forward)
	case "-J":
		a.Options.ProxyJumps = append(a.Options.ProxyJumps, splitJumps(value)...)
	case "-i":
		a.Options.IdentityFile = value
	case "-l":
		a.Options.User = value
	case "-p":
		port, err := parsePort(value)
		if err != nil {
			return err
		}
		a.Options.Port = port
	case "-o":
		return a.applyOption(value)
	}
	return nil
}

func (a *Args) applyOption(option string) error {
	key, raw, found := strings.Cut(option, "=")
	if !found {
		return fmt.Errorf("opcao -o invalida: %q (use -o Chave=Valor)", option)
	}
	key = strings.TrimSpace(key)
	raw = strings.TrimSpace(raw)

	switch strings.ToLower(key) {
	case "stricthostkeychecking":
		a.Options.StrictHostKeySet = true
		switch strings.ToLower(raw) {
		case "yes", "ask", "true":
			a.Options.StrictHostKey = true
		case "accept-new", "no", "false", "off":
			a.Options.StrictHostKey = false
		default:
			return fmt.Errorf("valor nao suportado para StrictHostKeyChecking: %q (use yes ou accept-new)", raw)
		}
	case "userknownhostsfile":
		a.Options.KnownHostsFiles = splitHostKeyFiles(raw)
	case "identityfile":
		a.Options.IdentityFile = raw
	case "port":
		port, err := parsePort(raw)
		if err != nil {
			return err
		}
		a.Options.Port = port
	case "user":
		a.Options.User = raw
	case "proxyjump":
		a.Options.ProxyJumps = splitJumps(raw)
	case "connecttimeout":
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 0 {
			return fmt.Errorf("ConnectTimeout invalido: %q", raw)
		}
		a.Options.ConnectTimeout = time.Duration(seconds) * time.Second
	case "serveraliveinterval":
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 0 {
			return fmt.Errorf("ServerAliveInterval invalido: %q", raw)
		}
		a.Options.ServerAliveInterval = time.Duration(seconds) * time.Second
	case "serveralivecountmax":
		count, err := strconv.Atoi(raw)
		if err != nil || count < 0 {
			return fmt.Errorf("ServerAliveCountMax invalido: %q", raw)
		}
		a.Options.ServerAliveCountMax = count
	case "batchmode":
		switch strings.ToLower(raw) {
		case "yes", "true":
			a.Options.BatchMode = true
		case "no", "false":
			a.Options.BatchMode = false
		default:
			return fmt.Errorf("valor nao suportado para BatchMode: %q", raw)
		}
	case "loglevel":
		switch strings.ToLower(raw) {
		case "quiet", "error":
			a.Options.Verbosity = 0
		case "info":
			a.Options.Verbosity = 1
		case "verbose":
			a.Options.Verbosity = 2
		case "debug", "debug1":
			a.Options.Verbosity = 3
		default:
			a.Options.Verbosity = 1
		}
	case "compression":
		a.Warnings = append(a.Warnings,
			"aviso: Compression nao e suportada pelo cliente nativo; seguindo sem compressao.")
	case "forwardagent":
		if isTruthy(raw) {
			a.Warnings = append(a.Warnings,
				"aviso: ForwardAgent nao e suportado pelo cliente nativo; seguindo sem encaminhar o agente.")
		}
	default:
		return fmt.Errorf("opcao -o nao suportada: %s (suportadas: StrictHostKeyChecking, UserKnownHostsFile, "+
			"IdentityFile, Port, User, ProxyJump, ConnectTimeout, ServerAliveInterval, ServerAliveCountMax, "+
			"BatchMode, LogLevel)", key)
	}
	return nil
}

// ParseForward interpreta "[bind:]porta:host:porta" como o -L do ssh.
func ParseForward(value string) (Forward, error) {
	parts := strings.Split(value, ":")
	f := Forward{}
	switch len(parts) {
	case 3:
		// porta:host:porta (destino local)
	case 4:
		f.BindHost = parts[0]
		parts = parts[1:]
	default:
		return f, fmt.Errorf("encaminhamento invalido: %q (use [bind:]porta:host:porta)", value)
	}

	bindPort, err := parsePort(parts[0])
	if err != nil {
		return f, fmt.Errorf("porta local invalida em %q", value)
	}
	destPort, err := parsePort(parts[2])
	if err != nil {
		return f, fmt.Errorf("porta de destino invalida em %q", value)
	}
	destHost := strings.Trim(parts[1], "[]")
	if destHost == "" {
		return f, fmt.Errorf("host de destino ausente em %q", value)
	}
	f.BindPort = bindPort
	f.DestHost = destHost
	f.DestPort = destPort
	return f, nil
}

// parsePort valida e converte uma porta (1-65535).
func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("porta invalida: %q", value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("porta fora da faixa 1-65535: %d", port)
	}
	return port, nil
}

func splitJumps(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitHostKeyFiles(value string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == ',' }) {
		part = strings.TrimSpace(part)
		if part == "" || part == "none" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}
