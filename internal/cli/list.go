package cli

import (
	"errors"
	"flag"
	"io"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/table"
)

// listFilters sao os filtros aceitos por list e select.
type listFilters struct {
	group string
	tag   string
	flat  bool
}

// parseFilters interpreta -g, -t e --flat (com o apelido --sem-grupo).
func parseFilters(name string, args []string) (listFilters, error) {
	var filters listFilters
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&filters.group, "g", "", "grupo")
	set.StringVar(&filters.group, "group", "", "grupo")
	set.StringVar(&filters.tag, "t", "", "tag")
	set.StringVar(&filters.tag, "tag", "", "tag")
	set.BoolVar(&filters.flat, "flat", false, "tabela unica")
	set.BoolVar(&filters.flat, "sem-grupo", false, "tabela unica")
	if err := set.Parse(args); err != nil {
		return filters, err
	}
	if set.NArg() > 0 {
		return filters, errors.New("opcao desconhecida: " + set.Arg(0))
	}
	return filters, nil
}

// cmdList mostra as conexoes cadastradas.
func (a *App) cmdList(args []string) error {
	filters, err := parseFilters("list", args)
	if err != nil {
		return err
	}
	_, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	if len(cfg.Hosts) == 0 {
		a.info("nenhum host cadastrado. Use 'sshm create' para adicionar uma conexao.")
		return nil
	}
	rows := cfg.FilterRows(filters.group, filters.tag)
	if len(rows) == 0 {
		return errors.New("nenhum host encontrado para os filtros informados.")
	}
	a.output("%s", table.TextStyled(rows, !filters.flat, a.style()))
	return nil
}

// cmdConnectSelected abre a selecao interativa e conecta no host escolhido.
func (a *App) cmdConnectSelected(args []string) error {
	filters, err := parseFilters("select", args)
	if err != nil {
		return err
	}
	_, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	if len(cfg.Hosts) == 0 {
		return errors.New("nenhum host cadastrado. Use 'sshm create' para adicionar uma conexao.")
	}
	rows := cfg.FilterRows(filters.group, filters.tag)
	if len(rows) == 0 {
		return errors.New("nenhum host encontrado para os filtros informados.")
	}

	id, err := a.pick(rows, !filters.flat)
	if err != nil {
		return err
	}
	if id == 0 {
		return nil // o seletor ja avisou que foi cancelado
	}

	index, err := a.findHost(cfg, idString(id))
	if err != nil {
		return err
	}
	return a.connectTo(cfg.Hosts[index], nil)
}

// cmdConnect conecta direto em um host (id ou nome), repassando os args extras.
func (a *App) cmdConnect(key string, extra []string) error {
	_, cfg, err := a.openConfig()
	if err != nil {
		return err
	}
	index, err := a.findHost(cfg, key)
	if err != nil {
		return err
	}
	return a.connectTo(cfg.Hosts[index], extra)
}

// connectTo chama a conexao injetada e converte o codigo de saida.
//
// Falha de conexao devolve 255 (como o ssh); codigo 0 com erro significa erro
// de uso (opcao invalida), que sai com 1.
func (a *App) connectTo(host config.Host, extra []string) error {
	code, err := a.connect(host, extra)
	switch {
	case err != nil && code != 0:
		return &ExitError{Code: code, Message: err.Error()}
	case err != nil:
		return err
	case code != 0:
		return &ExitError{Code: code}
	default:
		return nil
	}
}
