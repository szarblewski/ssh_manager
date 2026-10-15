package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/szarblewski/ssh_manager/internal/secrets"
)

// cmdDelete apaga uma conexao (senha do cofre inclusa).
func (a *App) cmdDelete(args []string) error {
	var assumeYes bool
	set := flag.NewFlagSet("delete", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&assumeYes, "y", false, "nao pede confirmacao")
	set.BoolVar(&assumeYes, "yes", false, "nao pede confirmacao")
	set.BoolVar(&assumeYes, "force", false, "nao pede confirmacao")
	set.BoolVar(&assumeYes, "f", false, "nao pede confirmacao")

	key, flags, err := extractKey(args, nil)
	if err != nil {
		return err
	}
	if err := set.Parse(flags); err != nil {
		return err
	}
	if key == "" {
		return errors.New("uso: sshm delete <id|nome> [-y]")
	}

	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	index, err := a.findHost(cfg, key)
	if err != nil {
		return err
	}
	host := cfg.Hosts[index]

	if !assumeYes {
		if !a.isInteractive() {
			return fmt.Errorf("sem terminal para confirmar; use: sshm delete '%s' -y", key)
		}
		confirmed, err := a.Prompter.Confirm(fmt.Sprintf("Apagar a conexao '%s' (%s:%d)? [s/N]: ",
			host.Name, host.Target(), host.PortOrDefault()))
		if err != nil {
			return canceled(err)
		}
		if !confirmed {
			a.info("nada foi apagado.")
			return nil
		}
	}

	if label := host.PasswordSecret; label != "" {
		if err := secrets.Remove(a.Vault, label); err != nil {
			a.info("aviso: %v", err)
		}
	}

	removed := cfg.Hosts[index]
	cfg.Hosts = append(cfg.Hosts[:index], cfg.Hosts[index+1:]...)
	if err := store.Save(cfg); err != nil {
		return err
	}

	a.info("conexao '%s' (%s:%d) apagada.", removed.Name, removed.Target(), removed.PortOrDefault())
	if len(cfg.Hosts) == 0 {
		a.info("nao ha mais conexoes cadastradas; use 'sshm create' para adicionar.")
	}
	return nil
}
