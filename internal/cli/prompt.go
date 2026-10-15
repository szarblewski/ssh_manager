package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/szarblewski/ssh_manager/internal/sshx"
)

// ErrCancelled indica que o usuario interrompeu a entrada (Ctrl+D/Ctrl+C).
var ErrCancelled = errors.New("entrada cancelada")

// Prompter pergunta dados ao usuario no terminal.
type Prompter struct {
	In  *os.File
	Err io.Writer

	reader *bufio.Reader
}

// NewPrompter monta o leitor de dados do terminal.
func NewPrompter(in *os.File, errOut io.Writer) *Prompter {
	p := &Prompter{In: in, Err: errOut}
	if in != nil {
		p.reader = bufio.NewReader(in)
	} else {
		p.reader = bufio.NewReader(strings.NewReader(""))
	}
	return p
}

// Interactive diz se ha terminal para as perguntas.
func (p *Prompter) Interactive() bool {
	return p.In != nil && sshx.IsTerminal(p.In)
}

// Text pergunta um valor mostrando o padrao entre colchetes.
func (p *Prompter) Text(label, defaultValue string) (string, error) {
	if !p.Interactive() {
		return "", errors.New("sem terminal interativo: informe os dados por opcao (-n, -H, -u, -p, -i, -g, -t)")
	}
	if defaultValue != "" {
		fmt.Fprintf(p.Err, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(p.Err, "%s: ", label)
	}
	answer, err := p.readLine()
	if err != nil {
		return "", err
	}
	if answer == "" {
		return defaultValue, nil
	}
	return answer, nil
}

// Line le uma linha simples (sem valor padrao).
func (p *Prompter) Line(label string) (string, error) {
	fmt.Fprint(p.Err, label)
	return p.readLine()
}

// Secret le um valor sem eco (usado pelo sshx nas senhas e frase-senhas).
func (p *Prompter) Secret(label string) (string, error) {
	if !p.Interactive() {
		return "", errors.New("sem terminal interativo para pedir a senha")
	}
	fmt.Fprint(p.Err, label)
	value, err := p.readSecret()
	fmt.Fprintln(p.Err)
	if err != nil {
		return "", err
	}
	return value, nil
}

// Confirm faz uma pergunta de sim/nao (padrao nao, como no sshm.sh).
func (p *Prompter) Confirm(label string) (bool, error) {
	if !p.Interactive() {
		return false, nil
	}
	fmt.Fprint(p.Err, label)
	answer, err := p.readLine()
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "s", "sim", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// Password pergunta a senha duas vezes (igual ao sshm.sh).
func (p *Prompter) Password(label string) (string, error) {
	if !p.Interactive() {
		return "", errors.New("sem terminal interativo para pedir a senha")
	}
	fmt.Fprintf(p.Err, "Senha de %s (nao aparece na tela): ", label)
	first, err := p.readSecret()
	fmt.Fprintln(p.Err)
	if err != nil {
		return "", err
	}
	fmt.Fprint(p.Err, "Repita a senha: ")
	second, err := p.readSecret()
	fmt.Fprintln(p.Err)
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("as senhas nao conferem.")
	}
	return first, nil
}

func (p *Prompter) readLine() (string, error) {
	line, err := p.reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if err != nil {
		if line != "" {
			return line, nil
		}
		if errors.Is(err, io.EOF) {
			return "", ErrCancelled
		}
		return "", err
	}
	return line, nil
}

func (p *Prompter) readSecret() (string, error) {
	if p.Interactive() {
		value, err := term.ReadPassword(int(p.In.Fd()))
		if err != nil {
			return "", err
		}
		return string(value), nil
	}
	return p.readLine()
}
