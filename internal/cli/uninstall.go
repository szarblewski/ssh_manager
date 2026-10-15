package cli

import (
	"flag"
	"io"
	"os"
	"path/filepath"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// cmdUninstall remove o binario e, se pedido, a configuracao.
//
// No Windows tambem tira o diretorio do sshm do PATH do usuario. O executavel em
// uso nao pode ser apagado na hora, entao a remocao e agendada (veja
// uninstall_windows.go / uninstall_unix.go).
func (a *App) cmdUninstall(args []string) error {
	var purge bool
	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&purge, "purge-config", false, "apaga tambem a configuracao")
	if err := set.Parse(args); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)

	if removed, err := removeFromUserPath(dir); err == nil && removed {
		a.info("removi %s do PATH do usuario (abra um terminal novo para valer).", dir)
	}

	if purge {
		for _, target := range []string{config.UserConfigDir(), config.LegacyConfigDir()} {
			if _, err := os.Stat(target); err != nil {
				continue
			}
			if err := os.RemoveAll(target); err != nil {
				a.info("aviso: nao consegui apagar %s: %v", target, err)
				continue
			}
			a.info("configuracao apagada: %s", target)
		}
	}

	if err := scheduleSelfDelete(exe); err != nil {
		a.info("aviso: nao consegui agendar a remocao de %s: %v", exe, err)
		a.info("apague o arquivo manualmente para concluir a desinstalacao.")
		return nil
	}
	a.info("sshm removido (a configuracao fica em %s).", config.CreatePath(a.ConfigPath))
	if !purge {
		a.info("para apagar tambem as conexoes e senhas: sshm uninstall --purge-config")
	}
	return nil
}
