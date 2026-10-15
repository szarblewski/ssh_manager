package table

import (
	"github.com/szarblewski/ssh_manager/internal/ansi"
)

// Style define como cada parte da tabela e destacada.
//
// As cores sao aplicadas DEPOIS do alinhamento: as larguras sao calculadas com
// o texto puro, entao os codigos ANSI nao desalinham as colunas.
type Style struct {
	Enabled bool
}

// On devolve um estilo ligado ou desligado.
func NewStyle(enabled bool) Style { return Style{Enabled: enabled} }
func (s Style) paint(code, text string) string {
	if !s.Enabled {
		return text
	}
	return ansi.Painter{Enabled: true}.Paint(code, text)
}

// Header destaca a linha de titulos.
func (s Style) Header(text string) string { return s.paint(ansi.Dim, text) }

// ID destaca a coluna com o numero do cadastro.
func (s Style) ID(text string) string { return s.paint(ansi.Dim, text) }

// Name destaca o nome da conexao.
func (s Style) Name(text string) string { return s.paint(ansi.Bold, text) }

// Target destaca usuario@host.
func (s Style) Target(text string) string { return s.paint(ansi.Cyan, text) }

// Port destaca a porta.
func (s Style) Port(text string) string { return s.paint(ansi.Yellow, text) }

// Group destaca o nome do grupo (cabecalho e coluna GRUPO).
func (s Style) Group(text string) string { return s.paint(ansi.BoldCyan, text) }

// Tags destaca as tags.
func (s Style) Tags(text string) string { return s.paint(ansi.Green, text) }

// Muted apaga textos secundarios ("sem grupo", celulas vazias).
func (s Style) Muted(text string) string { return s.paint(ansi.Dim, text) }

// Warning destaca um aviso (senha em texto puro, por exemplo).
func (s Style) Warning(text string) string { return s.paint(ansi.Yellow, text) }

// OK destaca algo em ordem (senha no cofre, por exemplo).
func (s Style) OK(text string) string { return s.paint(ansi.Green, text) }

// Selected marca a linha escolhida no menu.
func (s Style) Selected(text string) string {
	if !s.Enabled {
		return text
	}
	return ansi.Inverse + text + ansi.Reset
}

// Number destaca o numero da linha nos menus numerados.
func (s Style) Number(text string) string { return s.paint(ansi.Bold, text) }

// cellsPainters devolve, por coluna, a funcao que pinta a celula.
//
// CellPainters devolve, por coluna (ID, NOME, USER@HOST, PORTA[, GRUPO], TAGS),
// a funcao que pinta a celula.
func (s Style) CellPainters(grouped bool) []func(string) string {
	if grouped {
		// ID NOME USER@HOST PORTA TAGS
		return []func(string) string{s.ID, s.Name, s.Target, s.Port, s.Tags}
	}
	// ID NOME USER@HOST PORTA GRUPO TAGS
	return []func(string) string{s.ID, s.Name, s.Target, s.Port, s.Group, s.Tags}
}

// cellPainters e o nome interno usado pela propria tabela.
func (s Style) cellPainters(grouped bool) []func(string) string { return s.CellPainters(grouped) }

// HeaderPainters pinta a linha de titulos (tudo em tom secundario).
func (s Style) HeaderPainters(grouped bool) []func(string) string {
	count := 6
	if grouped {
		count = 5
	}
	painters := make([]func(string) string, count)
	for i := range painters {
		painters[i] = s.Header
	}
	return painters
}
