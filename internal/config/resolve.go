package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveIdentityFile resolve o caminho de uma chave privada.
//
// O mesmo hosts.json pode ser usado no Linux e no Windows, onde o caminho
// original (ex.: /home/fulano/.ssh/id_rsa) nao existe. A ordem tentada e:
//
//  1. o caminho como esta (com "~" e variaveis de ambiente expandidos);
//  2. o mesmo arquivo dentro de ~/.ssh (mesmo nome de arquivo).
//
// Quando nada existe, devolve o caminho expandido para que a mensagem de erro
// mostre o caminho original.
func ResolveIdentityFile(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	expanded := ExpandPath(path)
	if isFile(expanded) {
		return expanded
	}
	base := filepath.Base(filepath.FromSlash(path))
	if base != "" && base != "." && base != string(filepath.Separator) {
		if candidate := filepath.Join(HomeSSHDir(), base); isFile(candidate) {
			return candidate
		}
	}
	return expanded
}

// SanitizeName remove espacos e caracteres de controle do nome do host.
func SanitizeName(name string) string {
	return strings.Join(strings.Fields(name), "")
}

// ValidatePort confere a faixa valida de porta.
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("porta fora da faixa 1-65535: %d", port)
	}
	return nil
}

func isFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// IsFile informa se o caminho existe e e um arquivo (usado nas validacoes).
func IsFile(path string) bool { return isFile(path) }
