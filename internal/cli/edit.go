package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/secrets"
)

// cmdEdit altera uma conexao existente.
//
// Sem opcoes pergunta campo a campo mostrando o valor atual ('-' limpa os
// opcionais); com opcoes altera so o que foi informado.
func (a *App) cmdEdit(args []string) error {
	var (
		name        string
		hostAddress string
		userName    string
		identity    string
		group       string
		tags        string
		port        int
	)
	var hasName, hasHost, hasUser, hasIdentity, hasGroup, hasTags, hasPort bool

	set := flag.NewFlagSet("edit", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&name, "n", "", "nome")
	set.StringVar(&name, "name", "", "nome")
	set.StringVar(&hostAddress, "H", "", "host")
	set.StringVar(&hostAddress, "host", "", "host")
	set.StringVar(&userName, "u", "", "usuario")
	set.StringVar(&userName, "user", "", "usuario")
	set.IntVar(&port, "p", 0, "porta")
	set.IntVar(&port, "port", 0, "porta")
	set.StringVar(&identity, "i", "", "chave")
	set.StringVar(&identity, "identity", "", "chave")
	set.StringVar(&identity, "identity-file", "", "chave")
	set.StringVar(&group, "g", "", "grupo")
	set.StringVar(&group, "group", "", "grupo")
	set.StringVar(&tags, "t", "", "tags")
	set.StringVar(&tags, "tags", "", "tags")

	// o host pode vir antes das opcoes: 'sshm edit <id|nome> -n novo'
	key, flags, err := extractKey(args, map[string]bool{
		"n": true, "name": true, "H": true, "host": true, "u": true, "user": true,
		"p": true, "port": true, "i": true, "identity": true, "identity-file": true,
		"g": true, "group": true, "t": true, "tags": true,
	})
	if err != nil {
		return err
	}
	if err := set.Parse(flags); err != nil {
		return err
	}
	if key == "" {
		return errors.New("uso: sshm edit <id|nome> [-n nome] [-H host] [-u usuario] [-p porta] [-i chave] [-g grupo] [-t tags]")
	}

	set.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "n", "name":
			hasName = true
		case "H", "host":
			hasHost = true
		case "u", "user":
			hasUser = true
		case "p", "port":
			hasPort = true
		case "i", "identity", "identity-file":
			hasIdentity = true
		case "g", "group":
			hasGroup = true
		case "t", "tags":
			hasTags = true
		}
	})

	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	index, err := a.findHost(cfg, key)
	if err != nil {
		return err
	}
	old := cfg.Hosts[index].Clone()

	updated := old.Clone()
	if !hasName && !hasHost && !hasUser && !hasPort && !hasIdentity && !hasGroup && !hasTags {
		if !a.isInteractive() {
			return errors.New("sem terminal interativo: use as opcoes (-n, -H, -u, -p, -i, -g, -t).")
		}
		converted, err := a.promptAllFields(old)
		if err != nil {
			return canceled(err)
		}
		updated = converted
	} else {
		if hasName {
			updated.Name = name
		}
		if hasHost {
			updated.Host = hostAddress
		}
		if hasUser {
			updated.User = userName
		}
		if hasPort {
			updated.Port = port
		}
		if hasIdentity {
			updated.IdentityFile = identity
		}
		if hasGroup {
			updated.Group = group
		}
		if hasTags {
			updated.Tags = config.NormalizeTags(tags)
		}
	}

	updated.Name = config.SanitizeName(updated.Name)
	updated.Tags = config.NormalizeTags(joinTags(updated.Tags))
	if updated.Name == "" {
		return errors.New("o nome nao pode ficar vazio.")
	}
	if updated.Host == "" {
		return errors.New("o host nao pode ficar vazio.")
	}
	if err := config.ValidatePort(updated.Port); err != nil {
		return err
	}
	if updated.Name != old.Name && cfg.NameExists(updated.Name) {
		return fmt.Errorf("ja existe uma conexao chamada '%s'.", updated.Name)
	}
	if updated.IdentityFile != "" && !config.IsFile(config.ExpandPath(updated.IdentityFile)) {
		a.info("aviso: o arquivo de chave '%s' nao existe (a conexao sera salva mesmo assim).", updated.IdentityFile)
	}

	// senha no cofre: acompanha o nome novo quando a conexao e renomeada
	if label := old.PasswordSecret; label != "" && updated.Name != old.Name {
		if err := a.renameSecret(label, updated.Name); err != nil {
			a.info("aviso: %v; rode 'sshm passwd %s' para guardar a senha de novo.", err, updated.Name)
		} else {
			updated.PasswordSecret = updated.Name
		}
	}

	cfg.Hosts[index] = updated
	if err := store.Save(cfg); err != nil {
		return err
	}

	a.info("conexao '%s' atualizada: %s", updated.Name, shortDescription(updated))
	if updated.Name != old.Name {
		a.info("nome alterado de '%s' para '%s'.", old.Name, updated.Name)
	}
	return nil
}

// promptAllFields pergunta campo a campo mostrando o valor atual.
func (a *App) promptAllFields(old config.Host) (config.Host, error) {
	fmt.Fprintf(a.Err, "Editando '%s' - Enter mantem o valor atual ('-' limpa os opcionais)\n", old.Name)
	updated := old.Clone()

	value, err := a.Prompter.Text("Nome/apelido", old.Name)
	if err != nil {
		return old, err
	}
	updated.Name = value

	value, err = a.Prompter.Text("Host/IP", old.Host)
	if err != nil {
		return old, err
	}
	updated.Host = value

	value, err = a.Prompter.Text("Usuario", old.User)
	if err != nil {
		return old, err
	}
	updated.User = value

	value, err = a.Prompter.Text("Porta", strconv.Itoa(old.PortOrDefault()))
	if err != nil {
		return old, err
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return old, fmt.Errorf("porta invalida: %q", value)
	}
	updated.Port = port

	optionalValue := func(label, current string) (string, error) {
		value, err := a.Prompter.Text(label, current)
		if err != nil {
			return "", err
		}
		if value == "-" {
			return "", nil
		}
		return value, nil
	}

	if updated.IdentityFile, err = optionalValue("Arquivo de chave", old.IdentityFile); err != nil {
		return old, err
	}
	if updated.Group, err = optionalValue("Grupo", old.Group); err != nil {
		return old, err
	}
	tags, err := optionalValue("Tags (virgula)", joinTags(old.Tags))
	if err != nil {
		return old, err
	}
	updated.Tags = config.NormalizeTags(tags)
	return updated, nil
}

// renameSecret move a senha do cofre para o nome novo do host.
func (a *App) renameSecret(oldLabel, newLabel string) error {
	if !secrets.Available(a.Vault) {
		return errors.New("o uso do cofre esta desativado")
	}
	secret, err := a.Vault.Get(oldLabel)
	if err != nil {
		return fmt.Errorf("nao consegui ler a senha de '%s' no %s", oldLabel, secrets.Description(a.Vault))
	}
	if err := a.Vault.Set(newLabel, secret); err != nil {
		return fmt.Errorf("nao consegui gravar a senha de '%s' no %s", newLabel, secrets.Description(a.Vault))
	}
	if err := a.Vault.Delete(oldLabel); err != nil {
		return fmt.Errorf("nao consegui apagar a senha antiga de '%s'", oldLabel)
	}
	return nil
}

func joinTags(tags []string) string {
	joined := ""
	for i, tag := range tags {
		if i > 0 {
			joined += ","
		}
		joined += tag
	}
	return joined
}
