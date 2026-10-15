package sshx

import (
	"os"

	"golang.org/x/term"
)

// DefaultTermType e o TERM enviado ao servidor quando o local nao define um.
const DefaultTermType = "xterm-256color"

// IsTerminal diz se o arquivo e um terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// LocalTerminal devolve o terminal local usado para tamanho e modo bruto.
func LocalTerminal() *os.File {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if IsTerminal(f) {
			return f
		}
	}
	return nil
}

// TermType devolve o valor de TERM para enviar ao servidor.
func TermType() string {
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	return DefaultTermType
}

// TerminalSize devolve largura e altura do terminal (0,0 se indisponivel).
func TerminalSize(f *os.File) (width, height int) {
	if f == nil {
		return 0, 0
	}
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 0, 0
	}
	return w, h
}

// RawMode coloca o terminal em modo bruto e devolve a funcao de restauracao.
func RawMode(f *os.File) (func(), error) {
	if f == nil || !IsTerminal(f) {
		return func() {}, nil
	}
	state, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return func() {}, err
	}
	return func() { _ = term.Restore(int(f.Fd()), state) }, nil
}
