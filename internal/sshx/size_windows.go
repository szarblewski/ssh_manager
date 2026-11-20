//go:build windows

package sshx

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// consoleSize devolve o tamanho da janela do console (0,0 se nao houver um).
//
// GetConsoleScreenBufferInfo so aceita um handle de buffer de tela: no handle
// de entrada (stdin) ele falha com "identificador invalido", e a saida pode
// estar redirecionada. Por isso tentamos as saidas padrao e, por fim, o
// proprio dispositivo do console.
func consoleSize() (width, height int) {
	for _, handle := range []windows.Handle{windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE} {
		if w, h, err := screenBufferSize(handle); err == nil {
			return w, h
		}
	}
	console, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return 0, 0
	}
	defer console.Close()
	w, h, err := screenBufferSize(windows.Handle(console.Fd()))
	if err != nil {
		return 0, 0
	}
	return w, h
}

func screenBufferSize(handle windows.Handle) (width, height int, err error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return 0, 0, err
	}
	width = int(info.Window.Right-info.Window.Left) + 1
	height = int(info.Window.Bottom-info.Window.Top) + 1
	if width <= 0 || height <= 0 {
		return 0, 0, errors.New("tamanho do console indisponivel")
	}
	return width, height, nil
}
