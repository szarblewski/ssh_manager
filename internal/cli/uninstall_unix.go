//go:build !windows

package cli

import "os"

// removeFromUserPath nao se aplica fora do Windows: o PATH e alterado pelo
// proprio instalador (install.sh) ou pelo gerenciador de pacotes.
func removeFromUserPath(string) (bool, error) { return false, nil }

// scheduleSelfDelete apaga o proprio binario (permitido com o arquivo em uso).
func scheduleSelfDelete(exe string) error {
	if exe == "" {
		return nil
	}
	return os.Remove(exe)
}
