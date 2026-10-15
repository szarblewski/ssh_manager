package table

import (
	"regexp"
	"strings"
	"testing"

	"github.com/szarblewski/ssh_manager/internal/ansi"
	"github.com/szarblewski/ssh_manager/internal/config"
)

func sampleRows() []config.Row {
	return []config.Row{
		{ID: 2, Name: "alfa", UserHost: "root@10.0.0.1", Port: 22, Group: "cliente-a", Tags: []string{"prod"}},
		{ID: 1, Name: "beta", UserHost: "deploy@10.0.0.2", Port: 2222, Tags: []string{"a", "b"}},
	}
}

func TestRenderAlignsColumns(t *testing.T) {
	out := Render([]string{"ID", "NOME"}, [][]string{{"1", "alfa"}, {"22", "beta"}})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("linhas = %v", lines)
	}
	if !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("cabecalho = %q", lines[0])
	}
	if lines[1] != "1  alfa" || lines[2] != "22 beta" {
		t.Fatalf("alinhamento inesperado: %q / %q", lines[1], lines[2])
	}
}

func TestRenderTruncatesLongCells(t *testing.T) {
	long := strings.Repeat("x", 60)
	out := Render([]string{"NOME", "TAGS"}, [][]string{{long, "t"}})
	if strings.Contains(out, long) {
		t.Fatalf("celula deveria ter sido truncada: %s", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("marca de truncamento ausente: %s", out)
	}
}

func TestTextFlatHasGroupColumn(t *testing.T) {
	out := Text(sampleRows(), false)
	if !strings.Contains(out, "GRUPO") {
		t.Fatalf("tabela unica deveria ter a coluna GRUPO:\n%s", out)
	}
	if !strings.Contains(out, "cliente-a") {
		t.Fatalf("saida inesperada:\n%s", out)
	}
	if !strings.Contains(out, "beta") || !strings.Contains(out, "-") {
		t.Fatalf("host sem grupo deveria aparecer com o marcador '-':\n%s", out)
	}
}

func TestTextGroupedSections(t *testing.T) {
	out := Text(sampleRows(), true)
	if !strings.Contains(out, "[cliente-a]") || !strings.Contains(out, "["+config.NoGroupLabel+"]") {
		t.Fatalf("blocos de grupo ausentes:\n%s", out)
	}
	if strings.Contains(out, "GRUPO") {
		t.Fatalf("bloco agrupado nao deveria repetir a coluna GRUPO:\n%s", out)
	}
	// a secao sem grupo sempre vem depois
	if strings.Index(out, "[cliente-a]") > strings.Index(out, config.NoGroupLabel) {
		t.Fatalf("ordem dos grupos errada:\n%s", out)
	}
}

func TestLinesForFzfAndNumberedMode(t *testing.T) {
	lines := Lines(sampleRows(), false)
	if len(lines) != 3 {
		t.Fatalf("linhas = %v", lines)
	}
	if !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("primeira linha = %q", lines[0])
	}
	if !strings.Contains(lines[1], "alfa") || !strings.Contains(lines[1], "cliente-a") {
		t.Fatalf("linha do host = %q", lines[1])
	}
}

func TestTruncateAndFits(t *testing.T) {
	if got := Truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Truncate("abc", 5); got != "abc" {
		t.Fatalf("Truncate curto = %q", got)
	}
	if !Fits("abc", 3) || Fits("abcd", 3) || !Fits("abc", 0) {
		t.Fatal("Fits com resultado inesperado")
	}
}

// ansiPattern casa as sequencias de cor/posicionamento emitidas pelo sshm.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripANSI(text string) string { return ansiPattern.ReplaceAllString(text, "") }

func TestStyledOutputKeepsSameLayoutWhenStripped(t *testing.T) {
	rows := sampleRows()
	styled := TextStyled(rows, true, NewStyle(true))
	if styled == Text(rows, true) {
		t.Fatal("a versao colorida deveria ter codigos ANSI")
	}
	if stripANSI(styled) != Text(rows, true) {
		t.Fatalf("com cores o alinhamento mudou:\n--- colorido (sem ANSI) ---\n%s\n--- simples ---\n%s",
			stripANSI(styled), Text(rows, true))
	}
}

func TestStyledHighlightsGroupsAndColumns(t *testing.T) {
	rows := sampleRows()
	out := TextStyled(rows, true, NewStyle(true))

	if !strings.Contains(out, ansi.BoldCyan+"[cliente-a]") {
		t.Fatalf("cabecalho de grupo sem destaque:\n%q", out)
	}
	if !strings.Contains(out, ansi.Bold) {
		t.Fatalf("nome da conexao sem destaque:\n%q", out)
	}
	if !strings.Contains(out, ansi.Cyan) {
		t.Fatalf("usuario@host sem destaque:\n%q", out)
	}
	if !strings.Contains(out, ansi.Green) {
		t.Fatalf("tags sem destaque:\n%q", out)
	}
	if !strings.Contains(out, ansi.Reset) {
		t.Fatalf("sem reset as cores vazariam para a proxima linha:\n%q", out)
	}
}

func TestStyledFlatModeColorsGroupColumn(t *testing.T) {
	out := TextStyled(sampleRows(), false, NewStyle(true))
	if !strings.Contains(out, ansi.BoldCyan) {
		t.Fatalf("coluna GRUPO sem destaque:\n%q", out)
	}
	if stripANSI(out) != Text(sampleRows(), false) {
		t.Fatal("tabela unica colorida perdeu o alinhamento")
	}
}

func TestDisabledStyleHasNoEscapeCodes(t *testing.T) {
	out := TextStyled(sampleRows(), true, NewStyle(false))
	if strings.Contains(out, "\x1b") {
		t.Fatalf("estilo desligado nao deveria emitir ANSI:\n%q", out)
	}
}
