// Package table monta as tabelas de hosts da listagem e da selecao, no mesmo
// formato do sshm.sh: tabela unica com a coluna GRUPO ou blocos por grupo.
package table

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/szarblewski/ssh_manager/internal/config"
)

// MaxColumnWidth limita a largura de cada coluna.
const MaxColumnWidth = 40

// HeaderFlat sao os titulos da tabela unica.
func HeaderFlat() []string {
	return []string{"ID", "NOME", "USER@HOST", "PORTA", "GRUPO", "TAGS"}
}

// HeaderGrouped sao os titulos dos blocos por grupo.
func HeaderGrouped() []string {
	return []string{"ID", "NOME", "USER@HOST", "PORTA", "TAGS"}
}

// Cells devolve as celulas de uma linha.
func Cells(row config.Row, grouped bool) []string {
	cells := []string{
		strconv.Itoa(row.ID),
		row.Name,
		row.UserHost,
		strconv.Itoa(row.Port),
	}
	if grouped {
		return append(cells, row.TagsCSV())
	}
	group := row.Group
	if group == "" {
		group = "-" // mesmo marcador usado pelo sshm.sh na tabela unica
	}
	return append(cells, group, row.TagsCSV())
}

// Rows devolve as celulas de todas as linhas.
func Rows(rows []config.Row, grouped bool) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, Cells(row, grouped))
	}
	return out
}

// Render alinha as colunas com espaco simples entre elas.
func Render(headers []string, rows [][]string) string {
	r := newRenderer(headers, rows)
	return renderRows(r, headers, rows, nil)
}

// RenderStyled e o Render com cor: as larguras saem do texto puro e a pintura e
// aplicada depois do alinhamento, entao as colunas continuam alinhadas.
//
// paints tem uma funcao por coluna (pode ser nil ou menor que o numero de
// colunas: o que faltar sai sem cor).
func RenderStyled(headers []string, rows [][]string, paints []func(string) string) string {
	return renderRows(newRenderer(headers, rows), headers, rows, paints)
}

func renderRows(r *renderer, headers []string, rows [][]string, paints []func(string) string) string {
	var builder strings.Builder
	if len(headers) > 0 {
		builder.WriteString(r.line(headers, paints))
	}
	for _, row := range rows {
		builder.WriteString(r.line(row, paints))
	}
	return builder.String()
}

// renderer guarda a largura calculada de cada coluna.
type renderer struct{ widths []int }

func newRenderer(headers []string, rows [][]string) *renderer {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for i := range widths {
		if widths[i] > MaxColumnWidth {
			widths[i] = MaxColumnWidth
		}
	}
	return &renderer{widths: widths}
}

func (r *renderer) line(cells []string, paints []func(string) string) string {
	var line strings.Builder
	for i, cell := range cells {
		if i >= len(r.widths) {
			break
		}
		padded := Truncate(cell, r.widths[i])
		if i < len(cells)-1 {
			padding := r.widths[i] - utf8.RuneCountInString(padded) + 1
			padded += strings.Repeat(" ", padding)
		}
		if i < len(paints) && paints[i] != nil {
			padded = paints[i](padded)
		}
		line.WriteString(padded)
	}
	return strings.TrimRight(line.String(), " ") + "\n"
}

// Text monta a listagem completa (agrupada por grupo ou em tabela unica).
func Text(rows []config.Row, grouped bool) string {
	return TextStyled(rows, grouped, NewStyle(false))
}

// TextStyled monta a listagem destacando grupos, nomes, destinos, portas e tags.
func TextStyled(rows []config.Row, grouped bool, style Style) string {
	if !grouped || !config.RowsHaveGroup(rows) {
		return RenderStyled(HeaderFlat(), Rows(rows, false), style.CellPainters(false))
	}

	renderer := newRenderer(HeaderGrouped(), Rows(rows, true))
	groupPaints := style.CellPainters(true)
	headerPaints := style.HeaderPainters(true)
	var builder strings.Builder
	lastGroup := ""
	first := true
	for _, row := range rows {
		if first || row.Group != lastGroup {
			if !first {
				builder.WriteString("\n")
			}
			builder.WriteString(style.groupHeader(row.Group))
			builder.WriteString("\n")
			builder.WriteString(renderer.line(HeaderGrouped(), headerPaints))
			first = false
			lastGroup = row.Group
		}
		builder.WriteString(renderer.line(Cells(row, true), groupPaints))
	}
	return builder.String()
}

// groupHeader monta o cabecalho de um grupo, apagando o rotulo "(sem grupo)".
func (s Style) groupHeader(group string) string {
	if group == "" {
		return s.Muted("[" + config.NoGroupLabel + "]")
	}
	return s.Group("[" + group + "]")
}

// Lines devolve uma linha por host (usada pelo fzf), com as mesmas colunas da
// tabela unica.
func Lines(rows []config.Row, grouped bool) []string {
	if grouped && config.RowsHaveGroup(rows) {
		grouped = true
	}
	headers := HeaderFlat()
	if grouped {
		headers = HeaderGrouped()
	}
	rendered := Render(headers, Rows(rows, grouped))
	return strings.Split(strings.TrimRight(rendered, "\n"), "\n")
}

// Truncate corta o texto no numero de runas informado.
func Truncate(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

// Fits diz se o texto cabe na largura informada.
func Fits(text string, limit int) bool {
	return limit <= 0 || utf8.RuneCountInString(text) <= limit
}
