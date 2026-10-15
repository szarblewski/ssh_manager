package ansi

import (
	"os"
	"strings"
	"testing"
)

func TestPaintOnAndOff(t *testing.T) {
	on := Painter{Enabled: true}
	if got := on.Paint(Bold, "texto"); got != Bold+"texto"+Reset {
		t.Fatalf("Paint ligado = %q", got)
	}
	off := Painter{Enabled: false}
	if got := off.Paint(Bold, "texto"); got != "texto" {
		t.Fatalf("Paint desligado = %q", got)
	}
	if got := on.Paint(Bold, ""); got != "" {
		t.Fatalf("texto vazio deveria continuar vazio: %q", got)
	}
}

func TestEnabledRespectsFlags(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	if Enabled(true, nil) {
		t.Fatal("--no-color deveria desligar as cores")
	}
	if Enabled(false, nil) {
		t.Fatal("sem arquivo de saida nao ha como colorir")
	}

	t.Setenv("NO_COLOR", "1")
	if Enabled(false, nil) {
		t.Fatal("NO_COLOR=1 deveria desligar as cores")
	}

	// NO_COLOR vazia nao desliga (convencao do no-color.org)
	t.Setenv("NO_COLOR", "  ")
	if Enabled(false, nil) {
		t.Fatal("NO_COLOR vazia nao deveria desligar as cores")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if Enabled(false, nil) {
		t.Fatal("TERM=dumb nao deveria colorir")
	}
}

func TestEnabledWithRealPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	if Enabled(false, writer) {
		t.Fatal("um pipe nao deveria ser colorido")
	}
	if IsTerminal(writer) {
		t.Fatal("pipe nao e terminal")
	}
	if IsTerminal(nil) {
		t.Fatal("arquivo nulo nao e terminal")
	}
}

func TestCodesAreEscapeSequences(t *testing.T) {
	for name, code := range map[string]string{
		"Reset": Reset, "Bold": Bold, "Dim": Dim, "Cyan": Cyan,
		"AltScreenOn": AltScreenOn, "CursorHide": CursorHide,
	} {
		if !strings.HasPrefix(code, "\033[") {
			t.Fatalf("%s nao e uma sequencia ANSI: %q", name, code)
		}
	}
}

func TestStripAndVisibleLen(t *testing.T) {
	colored := Bold + "abc" + Reset + Cyan + "de" + Reset
	if got := Strip(colored); got != "abcde" {
		t.Fatalf("Strip = %q", got)
	}
	if got := VisibleLen(colored); got != 5 {
		t.Fatalf("VisibleLen = %d", got)
	}
	if got := VisibleLen("ação"); got != 4 {
		t.Fatalf("VisibleLen com acentos = %d", got)
	}
	if Strip("sem cor") != "sem cor" {
		t.Fatal("Strip nao deveria alterar texto simples")
	}
}

func TestTruncateKeepsAnsiSequencesIntact(t *testing.T) {
	colored := Cyan + "abcdefgh" + Reset
	got := Truncate(colored, 4)
	if Strip(got) != "abc…" {
		t.Fatalf("texto visivel = %q", Strip(got))
	}
	if !strings.HasPrefix(got, Cyan) {
		t.Fatalf("a cor inicial foi perdida: %q", got)
	}
	if !strings.HasSuffix(got, Reset) {
		t.Fatalf("faltou o reset no fim: %q", got)
	}

	if Truncate("ab", 5) != "ab" {
		t.Fatal("texto menor que o limite deveria voltar intacto")
	}
	if Truncate(colored, 0) != "" {
		t.Fatal("limite zero deveria devolver vazio")
	}
	// o corte nunca pode deixar uma sequencia pela metade
	for limit := 1; limit <= 9; limit++ {
		cut := Truncate(colored, limit)
		if strings.HasSuffix(Strip(cut), "[") || strings.Contains(cut, "\033[0") && !strings.HasSuffix(cut, Reset) {
			t.Fatalf("corte em %d deixou a sequencia incompleta: %q", limit, cut)
		}
	}
}
