//go:build windows

package ansi

import (
	"os"

	"golang.org/x/sys/windows"
)

// consoleOutput e o dispositivo do console, mesmo com a saida redirecionada.
const consoleOutput = "CONOUT$"

// EnableVirtualTerminal liga o processamento de sequencias ANSI no console.
//
// O Windows Terminal ja vem ligado, mas o conhost classico (PowerShell 5.1 na
// janela antiga) precisa disso para interpretar cores e posicionamento.
func EnableVirtualTerminal(f *os.File) error {
	if f == nil {
		return nil
	}
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err // nao e um console (arquivo, pipe): nada a fazer
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return nil
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}

// Console abre o console diretamente (equivalente ao /dev/tty no Unix).
//
// Serve para desenhar o menu em tela cheia mesmo quando a saida padrao esta
// redirecionada para um arquivo ou capturada por outra ferramenta.
func Console() (*os.File, error) {
	return os.OpenFile(consoleOutput, os.O_WRONLY, 0)
}

// PrepareConsole abre o console, liga o modo ANSI e ajusta a pagina de codigo
// para UTF-8 (o console classico usa CP850/CP1252 e mostraria acentos e o
// caractere de truncamento errados). A funcao devolvida restaura tudo.
func PrepareConsole() (*os.File, func(), error) {
	const codePageUTF8 = 65001

	file, err := os.OpenFile(consoleOutput, os.O_WRONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	_ = EnableVirtualTerminal(file)

	restore := func() { _ = file.Close() }
	if previous, err := windows.GetConsoleOutputCP(); err == nil && previous != 0 && previous != codePageUTF8 {
		if err := windows.SetConsoleOutputCP(codePageUTF8); err == nil {
			restore = func() {
				_ = windows.SetConsoleOutputCP(previous)
				_ = file.Close()
			}
		}
	}
	return file, restore, nil
}
