//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	environmentKey  = `Environment`
	pathValueName   = "Path"
	createNoWindow  = 0x08000000
	detachedProcess = 0x00000008
)

// removeFromUserPath tira o diretorio do PATH do usuario (nunca do PATH da
// maquina), sem estourar o limite do setx.
func removeFromUserPath(dir string) (bool, error) {
	if dir == "" {
		return false, nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, environmentKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()

	current, _, err := key.GetStringValue(pathValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	target := normalizeDir(dir)
	var kept []string
	removed := false
	for _, part := range strings.Split(current, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if normalizeDir(os.ExpandEnv(part)) == target {
			removed = true
			continue
		}
		kept = append(kept, part)
	}
	if !removed {
		return false, nil
	}
	if err := key.SetExpandStringValue(pathValueName, strings.Join(kept, ";")); err != nil {
		return false, err
	}
	return true, nil
}

func normalizeDir(dir string) string {
	cleaned := filepath.Clean(dir)
	return strings.ToLower(strings.TrimRight(cleaned, `\`))
}

// scheduleSelfDelete agenda a remocao do proprio executavel depois que o
// processo sair (o Windows nao permite apagar um .exe em execucao).
func scheduleSelfDelete(exe string) error {
	if exe == "" {
		return errors.New("caminho do executavel vazio")
	}
	cmd := exec.Command("cmd", "/c", fmt.Sprintf(`ping -n 3 127.0.0.1 >nul & del /f /q "%s"`, exe))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | detachedProcess,
	}
	return cmd.Start()
}
