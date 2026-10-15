package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/secrets"
	"github.com/szarblewski/ssh_manager/internal/table"
)

// cmdPasswd guarda, troca ou apaga a senha de uma conexao.
//
// Sem argumentos mostra onde cada senha esta guardada, como o 'sshm passwd' do
// sshm.sh.
func (a *App) cmdPasswd(args []string) error {
	var (
		remove bool
		plain  bool
	)
	set := flag.NewFlagSet("passwd", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&remove, "remove", false, "apaga a senha")
	set.BoolVar(&remove, "rm", false, "apaga a senha")
	set.BoolVar(&remove, "delete", false, "apaga a senha")
	set.BoolVar(&remove, "apagar", false, "apaga a senha")
	set.BoolVar(&plain, "plain", false, "texto puro")
	set.BoolVar(&plain, "texto", false, "texto puro")

	key, flags, err := extractKey(args, nil)
	if err != nil {
		return err
	}
	if err := set.Parse(flags); err != nil {
		return err
	}

	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}

	if key == "" {
		a.printPasswordStatus(cfg)
		return nil
	}

	index, err := a.findHost(cfg, key)
	if err != nil {
		return err
	}
	if remove {
		return a.clearPassword(store, cfg, index)
	}
	return a.updatePassword(store, cfg, index, plain)
}

// printPasswordStatus mostra em que formato a senha de cada host esta guardada.
func (a *App) printPasswordStatus(cfg *config.Config) {
	if len(cfg.Hosts) == 0 {
		a.info("nenhum host cadastrado. Use 'sshm create' para adicionar uma conexao.")
		return
	}
	style := a.style()
	var rows [][]string
	var paint []func(string) string
	for i, host := range cfg.Hosts {
		status := a.passwordStatus(host)
		rows = append(rows, []string{fmt.Sprint(host.ID), host.Name, status})
		if i == 0 {
			paint = []func(string) string{style.ID, style.Name, func(text string) string {
				switch {
				case strings.HasPrefix(text, "sem senha"):
					return style.Muted(text)
				case strings.HasPrefix(text, "texto puro"):
					return style.Warning(text)
				default:
					return style.OK(text)
				}
			}}
		}
	}
	a.output("%s", table.RenderStyled([]string{"ID", "NOME", "SENHA"}, rows, paint))
	a.output("\n%s\n%s\n",
		style.Muted("Guardar ou trocar: sshm passwd <nome>"),
		style.Muted("Apagar:             sshm passwd <nome> --remove"))
}

// updatePassword pergunta a senha e a guarda (cofre por padrao, texto puro com --plain).
func (a *App) updatePassword(store *config.Store, cfg *config.Config, index int, plain bool) error {
	host := &cfg.Hosts[index]

	password, err := a.Prompter.Password(host.Name)
	if err != nil {
		return err
	}
	if password == "" {
		a.info("nenhuma senha guardada (senha vazia).")
		return nil
	}

	backend, err := secrets.Store(a.Vault, host.Name, password, plain)
	if err != nil {
		return err
	}

	switch backend {
	case secrets.BackendVault:
		host.PasswordSecret = host.Name
		host.Password = ""
		if err := store.Save(cfg); err != nil {
			return err
		}
		a.info("senha de '%s' guardada no %s.", host.Name, secrets.Description(a.Vault))
	case secrets.BackendPlain:
		host.Password = password
		host.PasswordSecret = ""
		if err := store.Save(cfg); err != nil {
			return err
		}
		a.info("senha de '%s' guardada em texto puro em %s.", host.Name, store.Path)
		if !secrets.Available(a.Vault) {
			a.info("para guardar cifrada, reative o cofre (tire o SSHM_KEYRING=off); ou use uma chave SSH (sshm create -i ...).")
		} else if plain {
			a.info("a senha ficou legivel para quem ler o hosts.json; para usar o cofre, repita sem --plain.")
		}
	default:
		a.info("nenhuma senha foi alterada.")
	}
	return nil
}

// clearPassword apaga a senha do host (cofre e arquivo).
func (a *App) clearPassword(store *config.Store, cfg *config.Config, index int) error {
	host := &cfg.Hosts[index]
	if label := host.PasswordSecret; label != "" {
		if err := secrets.Remove(a.Vault, label); err != nil {
			return err
		}
	}
	host.Password = ""
	host.PasswordSecret = ""
	if err := store.Save(cfg); err != nil {
		return err
	}
	a.info("senha removida de '%s'.", host.Name)
	return nil
}
