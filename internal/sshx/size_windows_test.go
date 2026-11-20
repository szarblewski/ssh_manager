//go:build windows

package sshx

import (
	"os"
	"testing"
)

// O stdin e um handle de entrada e nao responde a GetConsoleScreenBufferInfo:
// o tamanho tem que vir do console mesmo assim (senao o PTY remoto ficaria no
// padrao 80x24).
func TestTerminalSizeComHandleDeEntrada(t *testing.T) {
	consoleWidth, consoleHeight := consoleSize()
	if consoleWidth <= 0 || consoleHeight <= 0 {
		t.Skip("ambiente sem console anexado")
	}
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("CONIN$ indisponivel: %v", err)
	}
	defer conin.Close()

	width, height := TerminalSize(conin)
	if width != consoleWidth || height != consoleHeight {
		t.Fatalf("TerminalSize(handle de entrada) = %dx%d, quer %dx%d",
			width, height, consoleWidth, consoleHeight)
	}
}
