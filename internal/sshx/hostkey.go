package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// Prompter pergunta dados ao usuario no terminal.
type Prompter interface {
	// Secret le um valor sem eco (senha/frase-senha).
	Secret(prompt string) (string, error)
	// Confirm faz uma pergunta de sim/nao.
	Confirm(prompt string) (bool, error)
}

// NopPrompter e usado quando nao ha terminal: nunca aceita nada.
type NopPrompter struct{}

// Secret devolve erro: sem terminal nao ha como perguntar.
func (NopPrompter) Secret(string) (string, error) {
	return "", errors.New("sem terminal interativo para pedir a senha")
}

// Confirm responde sempre "nao".
func (NopPrompter) Confirm(string) (bool, error) { return false, nil }

// DefaultKnownHostsFile devolve o known_hosts do usuario.
func DefaultKnownHostsFile() string {
	return filepath.Join(config.HomeSSHDir(), "known_hosts")
}

// knownHostsCallback monta o verificador de chave de host.
//
// Comportamento igual ao sshm.sh (StrictHostKeyChecking=accept-new):
//   - chave conhecida e igual: conecta;
//   - host desconhecido: aceita e registra no known_hosts (ou pergunta, se
//     SSHM_STRICT_HOSTKEY=1);
//   - chave diferente da registrada: recusa sempre, sem opcao de aceitar.
func knownHostsCallback(files []string, strict bool, prompt Prompter) (ssh.HostKeyCallback, error) {
	if len(files) == 0 {
		files = []string{DefaultKnownHostsFile()}
	}
	resolved := make([]string, 0, len(files))
	for _, file := range files {
		path := config.ExpandPath(file)
		if err := ensureKnownHostsFile(path); err != nil {
			return nil, err
		}
		resolved = append(resolved, path)
	}
	check, err := knownhosts.New(resolved...)
	if err != nil {
		return nil, fmt.Errorf("nao foi possivel ler %s: %w", strings.Join(resolved, ", "), err)
	}
	writeTo := resolved[0]

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}

		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return fmt.Errorf("a chave de %s esta revogada em %s:%d",
				hostname, revoked.Revoked.Filename, revoked.Revoked.Line)
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}

		fingerprint := ssh.FingerprintSHA256(key)

		if len(keyErr.Want) > 0 {
			// chave registrada e diferente: pode ser troca legitima ou ataque
			var expected []string
			for _, want := range keyErr.Want {
				expected = append(expected, fmt.Sprintf("%s (%s:%d)",
					ssh.FingerprintSHA256(want.Key), want.Filename, want.Line))
			}
			return fmt.Errorf(
				"a chave de host de %s MUDOU.\n"+
					"  recebida:  %s\n"+
					"  registrada: %s\n"+
					"o sshm nunca troca essa chave sozinho. Se a troca for legitima, remova a linha do "+
					"arquivo indicado e conecte de novo.",
				hostname, fingerprint, strings.Join(expected, ", "))
		}

		// host desconhecido
		if strict {
			ok, err := prompt.Confirm(fmt.Sprintf(
				"A chave de %s e desconhecida (a autenticidade nao pode ser confirmada).\n"+
					"  %s\nConfiar e continuar conectando? [s/N]: ", hostname, fingerprint))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("conexao recusada: chave de host de %s nao confirmada", hostname)
			}
		}

		line := knownhosts.Line([]string{hostname}, key)
		file, err := os.OpenFile(writeTo, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("host novo aceito, mas nao foi possivel gravar em %s: %w", writeTo, err)
		}
		defer file.Close()
		if _, err := fmt.Fprintln(file, line); err != nil {
			return fmt.Errorf("host novo aceito, mas nao foi possivel gravar em %s: %w", writeTo, err)
		}
		return nil
	}, nil
}

// ensureKnownHostsFile garante que o arquivo exista (o knownhosts.New falha com
// ENOENT) e cria ~/.ssh com permissao restrita quando necessario.
func ensureKnownHostsFile(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s e um diretorio; esperado um arquivo known_hosts", path)
		}
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("nao foi possivel criar %s: %w", dir, err)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("nao foi possivel criar %s: %w", path, err)
	}
	return file.Close()
}
