package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// DirName e o nome do diretorio de configuracao do sshm.
	DirName = "sshm"

	// FileName e o nome do arquivo de conexoes.
	FileName = "hosts.json"

	// EnvConfig aponta para um hosts.json especifico (mesma variavel do sshm.sh).
	EnvConfig = "SSHM_CONFIG"
)

// UserConfigDir devolve o diretorio de configuracao do usuario.
//
// Windows: %APPDATA%\sshm. Outros: $XDG_CONFIG_HOME/sshm ou ~/.config/sshm.
func UserConfigDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, DirName)
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, DirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return DirName
	}
	return filepath.Join(home, ".config", DirName)
}

// LegacyConfigDir devolve o diretorio usado pelo sshm.sh (~/.config/sshm).
// No Windows isso permite reaproveitar um hosts.json vindo do Linux/Git Bash.
func LegacyConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return DirName
	}
	return filepath.Join(home, ".config", DirName)
}

// HomeSSHDir devolve o diretorio ~/.ssh do usuario.
func HomeSSHDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ssh"
	}
	return filepath.Join(home, ".ssh")
}

// ExecutableDir devolve a pasta do binario em execucao (vazio se indisponivel).
func ExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// Candidates devolve, na ordem de procura, os caminhos possiveis do hosts.json:
//
//  1. o caminho explicito (--config ou $SSHM_CONFIG) - quando informado, e o
//     unico candidato, como no sshm.sh ("com SSHM_CONFIG so ele conta");
//  2. %APPDATA%\sshm\hosts.json (Linux/macOS: ~/.config/sshm/hosts.json);
//  3. ~/.config/sshm/hosts.json (compatibilidade com o sshm.sh e com o Linux);
//  4. hosts.json na pasta atual;
//  5. hosts.json na pasta do executavel (quando ela nao e um "bin").
//
// Caminhos repetidos sao descartados.
func Candidates(explicit string) []string {
	if explicit == "" {
		explicit = os.Getenv(EnvConfig)
	}
	if explicit != "" {
		return []string{explicit}
	}

	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		for _, seen := range out {
			if strings.EqualFold(seen, p) {
				return
			}
		}
		out = append(out, p)
	}

	add(filepath.Join(UserConfigDir(), FileName))
	add(filepath.Join(LegacyConfigDir(), FileName))
	if cwd, err := os.Getwd(); err == nil {
		add(filepath.Join(cwd, FileName))
	}
	if dir := ExecutableDir(); dir != "" {
		switch strings.ToLower(filepath.Base(dir)) {
		case "bin", "sbin":
			// binario instalado como comando: a configuracao nao mora junto
		default:
			add(filepath.Join(dir, FileName))
		}
	}
	return out
}

// Resolve devolve o primeiro arquivo de configuracao existente.
func Resolve(explicit string) (string, bool) {
	for _, candidate := range Candidates(explicit) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// CreatePath diz onde criar o arquivo quando nenhum existe:
// o caminho explicito ou <dir de configuracao do usuario>/hosts.json.
func CreatePath(explicit string) string {
	if explicit == "" {
		explicit = os.Getenv(EnvConfig)
	}
	if explicit != "" {
		return explicit
	}
	return filepath.Join(UserConfigDir(), FileName)
}

// ExpandPath expande "~" e variaveis de ambiente em um caminho.
func ExpandPath(path string) string {
	if path == "" {
		return ""
	}
	path = os.ExpandEnv(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return filepath.FromSlash(path)
}
