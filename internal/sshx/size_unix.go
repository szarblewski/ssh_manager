//go:build !windows

package sshx

import (
	"os"

	"golang.org/x/term"
)

// consoleSize tenta as saidas padrao quando o arquivo informado nao responde
// (0,0 se nenhuma delas for um terminal).
func consoleSize() (width, height int) {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if w, h, err := term.GetSize(int(f.Fd())); err == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	return 0, 0
}
