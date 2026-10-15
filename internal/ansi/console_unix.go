//go:build !windows

package ansi

import "os"

// EnableVirtualTerminal nao e necessario fora do Windows.
func EnableVirtualTerminal(*os.File) error { return nil }

// Console abre o terminal do usuario diretamente (o /dev/tty), o que permite
// desenhar o menu em tela cheia mesmo com a saida padrao redirecionada.
func Console() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
}

// PrepareConsole abre o /dev/tty; no Unix nao ha pagina de codigo para ajustar.
func PrepareConsole() (*os.File, func(), error) {
	file, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	return file, func() { _ = file.Close() }, nil
}
