package sshx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseArgs(t *testing.T) {
	args, err := ParseArgs([]string{
		"-p", "2222",
		"-l", "deploy",
		"-i", "~/chave",
		"-L", "8080:localhost:80",
		"-L127.0.0.1:9090:db:5432",
		"-J", "bastiao1,bastiao2",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=2",
		"-N",
		"-T",
		"-vv",
		"--", "echo", "oi",
	})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	opts := args.Options
	if opts.Port != 2222 || opts.User != "deploy" || opts.IdentityFile != "~/chave" {
		t.Fatalf("opcoes basicas erradas: %+v", opts)
	}
	if len(opts.LocalForwards) != 2 {
		t.Fatalf("encaminhamentos = %+v", opts.LocalForwards)
	}
	if opts.LocalForwards[1].BindHost != "127.0.0.1" || opts.LocalForwards[1].DestPort != 5432 {
		t.Fatalf("encaminhamento colado lido errado: %+v", opts.LocalForwards[1])
	}
	if len(opts.ProxyJumps) != 2 || opts.ProxyJumps[1] != "bastiao2" {
		t.Fatalf("bastioes = %+v", opts.ProxyJumps)
	}
	if !opts.StrictHostKey || !opts.StrictHostKeySet {
		t.Fatalf("StrictHostKeyChecking nao aplicado: %+v", opts)
	}
	if opts.ConnectTimeout != 5*time.Second {
		t.Fatalf("ConnectTimeout = %v", opts.ConnectTimeout)
	}
	if opts.ServerAliveInterval != 30*time.Second || opts.ServerAliveCountMax != 2 {
		t.Fatalf("keepalive = %v/%d", opts.ServerAliveInterval, opts.ServerAliveCountMax)
	}
	if !opts.NoRemoteCommand || !opts.DisableTTY {
		t.Fatalf("-N/-T nao aplicados: %+v", opts)
	}
	if opts.Verbosity != 2 {
		t.Fatalf("Verbosity = %d", opts.Verbosity)
	}
	if strings.Join(args.Command, " ") != "echo oi" {
		t.Fatalf("comando remoto = %v", args.Command)
	}
}

func TestParseArgsCommandWithoutSeparator(t *testing.T) {
	args, err := ParseArgs([]string{"uptime"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if strings.Join(args.Command, " ") != "uptime" {
		t.Fatalf("comando = %v", args.Command)
	}
}

func TestParseArgsWarningsAndErrors(t *testing.T) {
	args, err := ParseArgs([]string{"-C", "-o", "ForwardAgent=yes", "-o", "Compression=yes"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(args.Warnings) != 3 {
		t.Fatalf("avisos = %v", args.Warnings)
	}

	if _, err := ParseArgs([]string{"-R", "8080:localhost:80"}); err == nil {
		t.Fatal("-R deveria ser recusado no v1")
	}
	if _, err := ParseArgs([]string{"-Z"}); err == nil {
		t.Fatal("flag desconhecida deveria falhar")
	}
	if _, err := ParseArgs([]string{"-o", "NaoExiste=1"}); err == nil {
		t.Fatal("-o desconhecida deveria falhar")
	}
	if _, err := ParseArgs([]string{"-p"}); err == nil {
		t.Fatal("-p sem valor deveria falhar")
	}
	if _, err := ParseArgs([]string{"-p", "abc"}); err == nil {
		t.Fatal("porta invalida deveria falhar")
	}
}

func TestParseForward(t *testing.T) {
	forward, err := ParseForward("8080:localhost:80")
	if err != nil {
		t.Fatalf("ParseForward: %v", err)
	}
	if forward.BindPort != 8080 || forward.DestHost != "localhost" || forward.DestPort != 80 {
		t.Fatalf("forward = %+v", forward)
	}
	if got := forward.String(); got != "127.0.0.1:8080:localhost:80" {
		t.Fatalf("String = %q", got)
	}
	if _, err := ParseForward("8080:localhost"); err == nil {
		t.Fatal("formato invalido deveria falhar")
	}
	if _, err := ParseForward("0:localhost:80"); err == nil {
		t.Fatal("porta zero deveria falhar")
	}
}

func TestParseJumpSpec(t *testing.T) {
	addr, user, err := parseJumpSpec("deploy@bastiao:2222", Spec{Port: 22})
	if err != nil {
		t.Fatalf("parseJumpSpec: %v", err)
	}
	if addr != "bastiao:2222" || user != "deploy" {
		t.Fatalf("addr=%q user=%q", addr, user)
	}

	addr, user, err = parseJumpSpec("bastiao", Spec{Port: 2200})
	if err != nil {
		t.Fatalf("parseJumpSpec: %v", err)
	}
	if addr != "bastiao:2200" || user != "" {
		t.Fatalf("addr=%q user=%q", addr, user)
	}

	if _, _, err := parseJumpSpec("", Spec{}); err == nil {
		t.Fatal("bastiao vazio deveria falhar")
	}
}

func TestSpecAddrAndDefaults(t *testing.T) {
	spec := Spec{Host: "10.0.0.1"}
	if spec.Addr() != "10.0.0.1:22" {
		t.Fatalf("Addr = %q", spec.Addr())
	}
	dialer := NewDialer(Spec{Host: "h", User: "u", Port: 22}, Options{}, nil, nil)
	if dialer.Spec.Port != 22 || dialer.Spec.User != "u" {
		t.Fatalf("spec = %+v", dialer.Spec)
	}
	if dialer.Target() != "u@h:22" {
		t.Fatalf("Target = %q", dialer.Target())
	}
	if dialer.timeout() != DefaultConnectTimeout {
		t.Fatalf("timeout = %v", dialer.timeout())
	}
	// flags vencem os campos do cadastro
	overridden := NewDialer(Spec{Host: "h", User: "u", Port: 22}, Options{User: "outro", Port: 2200}, nil, nil)
	if overridden.Spec.User != "outro" || overridden.Spec.Port != 2200 {
		t.Fatalf("opcoes nao venceram o cadastro: %+v", overridden.Spec)
	}
}

func TestExitStatus(t *testing.T) {
	if code, ok := ExitStatus(nil); !ok || code != 0 {
		t.Fatalf("ExitStatus(nil) = %d,%v", code, ok)
	}
	if _, ok := ExitStatus(errors.New("conexao caiu")); ok {
		t.Fatal("erro generico nao deveria ter codigo de saida")
	}
}

func TestKnownHostsAcceptsNewHostAndWritesEntry(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	key := publicKey(t)
	callback, err := knownHostsCallback([]string{knownHosts}, false, NopPrompter{})
	if err != nil {
		t.Fatalf("knownHostsCallback: %v", err)
	}
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}

	if err := callback("127.0.0.1:2222", remote, key); err != nil {
		t.Fatalf("host desconhecido deveria ser aceito (accept-new): %v", err)
	}
	data, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatalf("known_hosts deveria existir: %v", err)
	}
	if !strings.Contains(string(data), "[127.0.0.1]:2222") {
		t.Fatalf("entrada escrita errada: %s", data)
	}

	// uma nova execucao rele o arquivo: a chave agora e conhecida
	fresh, err := knownHostsCallback([]string{knownHosts}, false, NopPrompter{})
	if err != nil {
		t.Fatalf("knownHostsCallback: %v", err)
	}
	if err := fresh("127.0.0.1:2222", remote, key); err != nil {
		t.Fatalf("segunda conexao deveria passar: %v", err)
	}
}

func TestKnownHostsDefaultPortEntry(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	callback, err := knownHostsCallback([]string{knownHosts}, false, NopPrompter{})
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 22}
	if err := callback("10.0.0.5:22", remote, publicKey(t)); err != nil {
		t.Fatalf("host novo: %v", err)
	}
	data, _ := os.ReadFile(knownHosts)
	if !strings.Contains(string(data), "\n10.0.0.5 ") && !strings.HasPrefix(string(data), "10.0.0.5 ") {
		t.Fatalf("porta 22 deveria ser gravada sem brackets: %s", data)
	}
}

func TestKnownHostsRejectsChangedKey(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}

	first, err := knownHostsCallback([]string{knownHosts}, false, NopPrompter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := first("127.0.0.1:22", remote, publicKey(t)); err != nil {
		t.Fatalf("primeira conexao: %v", err)
	}

	// a nova execucao le a chave registrada e recusa uma chave diferente
	second, err := knownHostsCallback([]string{knownHosts}, false, NopPrompter{})
	if err != nil {
		t.Fatal(err)
	}
	err = second("127.0.0.1:22", remote, publicKey(t))
	if err == nil {
		t.Fatal("troca de chave deveria falhar")
	}
	if !strings.Contains(err.Error(), "MUDOU") {
		t.Fatalf("mensagem inesperada: %v", err)
	}
}

func TestKnownHostsStrictModeAsksForConfirmation(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}

	denied, err := knownHostsCallback([]string{knownHosts}, true, NopPrompter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := denied("127.0.0.1:22", remote, publicKey(t)); err == nil {
		t.Fatal("sem confirmacao a conexao deveria ser recusada")
	}

	accepted, err := knownHostsCallback([]string{knownHosts}, true, yesPrompter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := accepted("127.0.0.1:22", remote, publicKey(t)); err != nil {
		t.Fatalf("com confirmacao a conexao deveria ser aceita: %v", err)
	}
}

func TestKnownHostsCreatesMissingDirectory(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "novo", ".ssh", "known_hosts")
	if _, err := knownHostsCallback([]string{knownHosts}, false, NopPrompter{}); err != nil {
		t.Fatalf("knownHostsCallback: %v", err)
	}
	if _, err := os.Stat(knownHosts); err != nil {
		t.Fatalf("arquivo deveria ter sido criado: %v", err)
	}
}

func TestFillAnswers(t *testing.T) {
	answers, err := fillAnswers([]string{"Senha:", "Codigo:"}, "guardada", NopPrompter{})
	if err != nil {
		t.Fatalf("fillAnswers: %v", err)
	}
	if len(answers) != 2 || answers[0] != "guardada" || answers[1] != "guardada" {
		t.Fatalf("respostas = %v", answers)
	}

	answers, err = fillAnswers([]string{"Senha:"}, "", yesPrompter{secret: "digitada"})
	if err != nil {
		t.Fatalf("fillAnswers: %v", err)
	}
	if answers[0] != "digitada" {
		t.Fatalf("resposta = %v", answers)
	}

	if _, err := fillAnswers([]string{"Senha:"}, "", NopPrompter{}); err == nil {
		t.Fatal("sem terminal deveria falhar")
	}
}

// --- servidor SSH em processo: cobre dial, autenticacao e execucao ---

func TestDialAndRunAgainstInProcessServer(t *testing.T) {
	server := startTestServer(t, "tester", "segredo")

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	dialer := NewDialer(
		Spec{Host: "127.0.0.1", Port: server.port, User: "tester"},
		Options{Password: "segredo", KnownHostsFiles: []string{knownHosts}},
		NopPrompter{},
		io.Discard,
	)
	client, err := dialer.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	defer dialer.Close()

	var stdout, stderr bytes.Buffer
	code, err := dialer.Run(client, []string{"echo", "oi"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("codigo de saida = %d", code)
	}
	if !strings.Contains(stdout.String(), "echo oi") {
		t.Fatalf("o servidor nao recebeu o comando: %q", stdout.String())
	}

	// segundo comando no mesmo cliente, agora com codigo de saida diferente
	stdout.Reset()
	server.exitStatus = 7
	code, err = dialer.Run(client, []string{"false"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 7 {
		t.Fatalf("codigo de saida = %d, esperado 7", code)
	}
}

func TestDialAndRunWithPublicKey(t *testing.T) {
	server := startTestServer(t, "tester", "segredo")

	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	writePrivateKey(t, keyPath)
	server.authorizedKey = publicKeyFile(t, keyPath)

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	dialer := NewDialer(
		Spec{Host: "127.0.0.1", Port: server.port, User: "tester", IdentityFile: keyPath},
		Options{KnownHostsFiles: []string{knownHosts}},
		NopPrompter{},
		io.Discard,
	)
	client, err := dialer.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial com chave: %v", err)
	}
	defer client.Close()
	defer dialer.Close()

	var stdout bytes.Buffer
	code, err := dialer.Run(client, []string{"uptime"}, strings.NewReader(""), &stdout, io.Discard)
	if err != nil || code != 0 {
		t.Fatalf("Run = %d, %v", code, err)
	}
}

func TestDialFailsWithWrongPassword(t *testing.T) {
	server := startTestServer(t, "tester", "segredo")
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")

	dialer := NewDialer(
		Spec{Host: "127.0.0.1", Port: server.port, User: "tester"},
		Options{
			Password:        "errada",
			KnownHostsFiles: []string{knownHosts},
			BatchMode:       true,
			ConnectTimeout:  5 * time.Second,
		},
		NopPrompter{},
		io.Discard,
	)
	if _, err := dialer.Dial(context.Background()); err == nil {
		t.Fatal("senha errada deveria falhar")
	}
}

func TestDialThroughJumpHost(t *testing.T) {
	target := startTestServer(t, "tester", "segredo")
	jump := startTestServer(t, "tester", "segredo")

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	dialer := NewDialer(
		Spec{Host: "127.0.0.1", Port: target.port, User: "tester"},
		Options{
			Password:        "segredo",
			KnownHostsFiles: []string{knownHosts},
			ProxyJumps:      []string{fmt.Sprintf("tester@127.0.0.1:%d", jump.port)},
		},
		NopPrompter{},
		io.Discard,
	)
	client, err := dialer.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial com bastiao: %v", err)
	}
	defer client.Close()
	defer dialer.Close()

	var stdout bytes.Buffer
	if code, err := dialer.Run(client, []string{"hostname"}, strings.NewReader(""), &stdout, io.Discard); err != nil || code != 0 {
		t.Fatalf("Run = %d, %v", code, err)
	}
	if !strings.Contains(stdout.String(), "hostname") {
		t.Fatalf("saida = %q", stdout.String())
	}
}

func TestLocalForward(t *testing.T) {
	server := startTestServer(t, "tester", "segredo")
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")

	port := freePort(t)
	dialer := NewDialer(
		Spec{Host: "127.0.0.1", Port: server.port, User: "tester"},
		Options{
			Password:        "segredo",
			KnownHostsFiles: []string{knownHosts},
			LocalForwards:   []Forward{{BindHost: "127.0.0.1", BindPort: port, DestHost: "127.0.0.1", DestPort: server.port}},
		},
		NopPrompter{},
		io.Discard,
	)
	client, err := dialer.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	defer dialer.Close()

	stop, err := dialer.StartLocalForwards(client)
	if err != nil {
		t.Fatalf("StartLocalForwards: %v", err)
	}
	defer stop()

	// o destino do forward e o proprio servidor ssh: basta conectar para
	// confirmar que o tunel abriu.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		t.Fatalf("nao foi possivel usar o tunel: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("SSH-2.0-teste\r\n")); err != nil {
		t.Fatalf("escrita no tunel: %v", err)
	}
	buf := make([]byte, 4)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("leitura no tunel: %v", err)
	}
	if string(buf) != "SSH-" {
		t.Fatalf("resposta inesperada pelo tunel: %q", buf)
	}
}

// --- infraestrutura dos testes ---

type testServer struct {
	port          int
	exitStatus    uint32
	authorizedKey ssh.PublicKey
}

func startTestServer(t *testing.T, user, password string) *testServer {
	t.Helper()
	states := &testServer{}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("autenticacao recusada para %s", meta.User())
		},
	}
	serverConfig.PublicKeyCallback = func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if states.authorizedKey != nil && bytes.Equal(key.Marshal(), states.authorizedKey.Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("chave nao autorizada")
	}
	serverConfig.AddHostKey(newSigner(t))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	states.port = listener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestConn(conn, serverConfig, states)
		}
	}()
	return states
}

func serveTestConn(conn net.Conn, serverConfig *ssh.ServerConfig, states *testServer) {
	_, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		switch newChannel.ChannelType() {
		case "session":
			acceptTestSession(newChannel, states)
		case "direct-tcpip":
			// o servidor de teste tambem faz as vezes de bastiao (ProxyJump/-L)
			go forwardTestChannel(newChannel)
		default:
			newChannel.Reject(ssh.UnknownChannelType, "apenas session e direct-tcpip")
		}
	}
}

func acceptTestSession(newChannel ssh.NewChannel, states *testServer) {
	channel, channelRequests, err := newChannel.Accept()
	if err != nil {
		return
	}
	go func() {
		for request := range channelRequests {
			if request.Type != "exec" {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			command := execCommand(request.Payload)
			fmt.Fprintf(channel, "%s\n", command)
			status := states.exitStatus
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
			channel.Close()
		}
	}()
}

func forwardTestChannel(newChannel ssh.NewChannel) {
	var payload struct {
		DestHost   string
		DestPort   uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		newChannel.Reject(ssh.ConnectionFailed, "payload invalido")
		return
	}
	dest, err := net.Dial("tcp", net.JoinHostPort(payload.DestHost, strconv.Itoa(int(payload.DestPort))))
	if err != nil {
		newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, requests, err := newChannel.Accept()
	if err != nil {
		dest.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	go func() { _, _ = io.Copy(dest, channel); dest.Close() }()
	go func() { _, _ = io.Copy(channel, dest); channel.Close() }()
}

func execCommand(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	size := binary.BigEndian.Uint32(payload[:4])
	if int(size) > len(payload)-4 {
		return string(payload[4:])
	}
	return string(payload[4 : 4+size])
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer
}

func publicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	return newSigner(t).PublicKey()
}

func writePrivateKey(t *testing.T, path string) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatalf("serializar chave: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("gravar chave: %v", err)
	}
}

func publicKeyFile(t *testing.T, path string) ssh.PublicKey {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler chave: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		t.Fatalf("parsear chave: %v", err)
	}
	return signer.PublicKey()
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

type yesPrompter struct{ secret string }

func (p yesPrompter) Secret(string) (string, error) {
	if p.secret == "" {
		return "", errors.New("sem segredo definido")
	}
	return p.secret, nil
}

func (yesPrompter) Confirm(string) (bool, error) { return true, nil }
