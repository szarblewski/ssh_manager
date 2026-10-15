package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/secrets"
)

// cmdConfig agrupa utilitarios de configuracao.
//
// Subcomandos: path (padrao), show, init, validate e migrate-secrets.
func (a *App) cmdConfig(args []string) error {
	sub := "path"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "path":
		return a.cmdConfigPath()
	case "show":
		return a.cmdConfigShow()
	case "init":
		return a.cmdConfigInit()
	case "validate":
		return a.cmdConfigValidate()
	case "normalize", "normalizar":
		return a.cmdConfigNormalize()
	case "migrate-secrets", "migrate":
		return a.cmdMigrateSecrets()
	case "help", "-h", "--help":
		a.output("Uso: sshm config path|show|init|validate|normalize|migrate-secrets\n")
		return nil
	default:
		return fmt.Errorf("subcomando desconhecido: %s (use path, show, init, validate, normalize ou migrate-secrets)", sub)
	}
}

func (a *App) cmdConfigPath() error {
	if path, ok := config.Resolve(a.ConfigPath); ok {
		a.output("%s\n", path)
		return nil
	}
	a.output("%s\n", config.CreatePath(a.ConfigPath))
	return nil
}

func (a *App) cmdConfigShow() error {
	store, _, err := a.openConfig()
	if err != nil {
		return err
	}
	data, err := config.ReadFile(store.Path)
	if err != nil {
		return err
	}
	a.output("%s", string(data))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		a.output("\n")
	}
	return nil
}

func (a *App) cmdConfigInit() error {
	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	if err := store.Save(cfg); err != nil {
		return err
	}
	a.info("configuracao pronta em %s (%d conexao(oes)).", store.Path, len(cfg.Hosts))
	return nil
}

func (a *App) cmdConfigValidate() error {
	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	data, err := config.ReadFile(store.Path)
	if err != nil {
		return err
	}
	if len(data) > 0 {
		var check map[string]json.RawMessage
		if err := json.Unmarshal(data, &check); err != nil {
			return fmt.Errorf("JSON invalido em %s: %w", store.Path, err)
		}
	}
	duplicated := map[string]int{}
	for _, host := range cfg.Hosts {
		duplicated[host.Name]++
	}
	var problems []string
	for name, count := range duplicated {
		if count > 1 {
			problems = append(problems, fmt.Sprintf("nome repetido: %s (%d vezes)", name, count))
		}
	}
	for _, host := range cfg.Hosts {
		if host.Name == "" || host.Host == "" || host.User == "" {
			problems = append(problems, fmt.Sprintf("id %d sem nome/host/usuario", host.ID))
		}
		if err := config.ValidatePort(host.PortOrDefault()); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", host.Name, err))
		}
	}

	a.output("%s: %d conexao(oes)\n", store.Path, len(cfg.Hosts))
	if len(problems) == 0 {
		a.output("nenhum problema encontrado.\n")
		return nil
	}
	for _, problem := range problems {
		a.info("aviso: %s", problem)
	}
	return errors.New("a configuracao tem problemas (veja os avisos acima)")
}

// cmdConfigNormalize reescreve o arquivo em UTF-8 sem BOM.
//
// Util quando o hosts.json veio do Bloco de Notas, de um 'Out-File' do
// PowerShell (UTF-16) ou de uma copia entre sistemas: o sshm ja le esses
// formatos, mas deixar o arquivo em UTF-8 puro evita surpresas com outras
// ferramentas (jq, editores antigos).
func (a *App) cmdConfigNormalize() error {
	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		return err
	}
	before := config.DetectEncoding(raw)

	if err := store.Save(cfg); err != nil {
		return err
	}

	after := "UTF-8 sem BOM"
	if updated, err := os.ReadFile(store.Path); err == nil {
		after = config.DetectEncoding(updated)
	}
	if before == after {
		a.info("%s ja estava em %s (%d conexao(oes)); reescrito no formato canonico.", store.Path, after, len(cfg.Hosts))
		return nil
	}
	a.info("%s convertido de %s para %s (%d conexao(oes)).", store.Path, before, after, len(cfg.Hosts))
	return nil
}

// cmdMigrateSecrets move as senhas em texto puro para o cofre do sistema.
func (a *App) cmdMigrateSecrets() error {
	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	if !secrets.Enabled() {
		return fmt.Errorf("o cofre do sistema esta desativado (%s=off); tire a variavel e tente de novo", secrets.EnvKeyring)
	}
	if !secrets.Available(a.Vault) {
		return fmt.Errorf("o cofre do sistema nao esta disponivel")
	}
	result := secrets.MigrateToVault(a.Vault, cfg)
	if result.Moved > 0 {
		if err := store.Save(cfg); err != nil {
			return err
		}
	}
	for _, failure := range result.Failures {
		a.info("aviso: %s", failure)
	}
	if result.Moved == 0 {
		a.info("nenhuma senha em texto puro para migrar.")
		return nil
	}
	a.info("%d senha(s) movida(s) para o %s.", result.Moved, secrets.Description(a.Vault))
	return nil
}
