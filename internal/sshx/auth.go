package sshx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// defaultIdentityFiles sao as chaves tentadas quando o host nao indica uma.
var defaultIdentityFiles = []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"}

// authMethods monta os metodos de autenticacao, tentados nesta ordem:
// agente, chave privada (a do cadastro ou as padrao de ~/.ssh), senha guardada
// e, por fim, a senha perguntada no terminal (quando ha terminal).
func (d *Dialer) authMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if method := d.agentMethod(); method != nil {
		methods = append(methods, method)
	}

	explicit, files := d.identityFiles()
	for _, path := range files {
		signer, err := loadSigner(path, d.Prompt)
		if err != nil {
			if explicit && errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("identity_file nao encontrado: %s", path)
			}
			if explicit {
				return nil, fmt.Errorf("nao foi possivel usar a chave %s: %w", path, err)
			}
			d.logf("aviso: chave %s ignorada (%v)", path, err)
			continue
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if d.Opts.Password != "" {
		methods = append(methods, ssh.Password(d.Opts.Password))
		methods = append(methods, keyboardInteractive(d.Opts.Password, d.Prompt))
	}

	if !d.Opts.BatchMode {
		// deixa o servidor pedir a senha no terminal, como faria o ssh
		methods = append(methods, ssh.KeyboardInteractive(d.askPasswords))
	}

	if len(methods) == 0 {
		methods = append(methods, ssh.KeyboardInteractive(d.askPasswords))
	}
	if d.Opts.Verbosity > 0 {
		for _, path := range files {
			d.debugf("depuracao: usando a chave %s", path)
		}
	}
	return methods, nil
}

// agentMethod conecta no ssh-agent e devolve o metodo de autenticacao.
func (d *Dialer) agentMethod() ssh.AuthMethod {
	if os.Getenv("SSHM_NO_AGENT") != "" {
		return nil
	}
	conn, err := dialAgent()
	if err != nil {
		d.debugf("depuracao: ssh-agent indisponivel em %s (%v)", agentAddress(), err)
		return nil
	}
	client := agent.NewClient(conn)
	signers, err := client.Signers()
	if err != nil {
		conn.Close()
		d.debugf("depuracao: falha ao listar chaves do agente (%v)", err)
		return nil
	}
	if len(signers) == 0 {
		conn.Close()
		d.debugf("depuracao: ssh-agent em %s sem chaves carregadas", agentAddress())
		return nil
	}
	d.agentConn = conn
	d.debugf("depuracao: %d chave(s) do ssh-agent em %s", len(signers), agentAddress())
	return ssh.PublicKeysCallback(client.Signers)
}

// identityFiles devolve (informado explicitamente, caminhos das chaves).
func (d *Dialer) identityFiles() (bool, []string) {
	if d.Opts.IdentityFile != "" {
		return true, []string{config.ResolveIdentityFile(d.Opts.IdentityFile)}
	}
	if d.Spec.IdentityFile != "" {
		return true, []string{config.ResolveIdentityFile(d.Spec.IdentityFile)}
	}
	var files []string
	for _, name := range defaultIdentityFiles {
		candidate := filepath.Join(config.HomeSSHDir(), name)
		if config.IsFile(candidate) {
			files = append(files, candidate)
		}
	}
	return false, files
}

// loadSigner le uma chave privada, pedindo a frase-senha quando necessario.
func loadSigner(path string, prompt Prompter) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return signer, nil
	}
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		passphrase, perr := prompt.Secret(fmt.Sprintf("Frase-senha de %s: ", filepath.Base(path)))
		if perr != nil {
			return nil, fmt.Errorf("a chave %s precisa de frase-senha: %w", path, perr)
		}
		return ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	}
	return nil, err
}

// keyboardInteractive responde a qualquer pergunta com a senha guardada.
func keyboardInteractive(password string, prompt Prompter) ssh.AuthMethod {
	return ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		return fillAnswers(questions, password, prompt)
	})
}

// askPasswords pergunta a senha no terminal, como o ssh faria.
func (d *Dialer) askPasswords(name, instruction string, questions []string, echos []bool) ([]string, error) {
	if d.Opts.BatchMode {
		return nil, errors.New("BatchMode ativo: o sshm nao pergunta senhas")
	}
	return fillAnswers(questions, "", d.Prompt)
}

func fillAnswers(questions []string, password string, prompt Prompter) ([]string, error) {
	answers := make([]string, len(questions))
	for i, question := range questions {
		if password != "" {
			answers[i] = password
			continue
		}
		label := strings.TrimSpace(question)
		if label == "" {
			label = "Senha"
		}
		answer, err := prompt.Secret(label + ": ")
		if err != nil {
			return nil, err
		}
		answers[i] = answer
	}
	return answers, nil
}
