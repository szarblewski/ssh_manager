package picker

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/szarblewski/ssh_manager/internal/ansi"
	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/table"
)

func testRows() []config.Row {
	return []config.Row{
		{ID: 10, Name: "alfa", UserHost: "root@10.0.0.1", Port: 22, Group: "cliente-a", Tags: []string{"prod"}},
		{ID: 20, Name: "beta", UserHost: "deploy@10.0.0.2", Port: 2222},
		{ID: 30, Name: "gama", UserHost: "root@10.0.0.3", Port: 22, Group: "cliente-b", Tags: []string{"homolog"}},
	}
}

func newTestTUI(t *testing.T, keys string) (*tui, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	screen := &tui{
		rows:    testRows(),
		grouped: true,
		outW:    out,
		errW:    errOut,
		style:   table.NewStyle(false),
		reader:  bufio.NewReader(strings.NewReader(keys)),
	}
	return screen, out, errOut
}

func TestTUIAcceptWithEnter(t *testing.T) {
	screen, out, errOut := newTestTUI(t, "\r")
	id, err := screen.run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 10 {
		t.Fatalf("id = %d, esperado 10 (primeiro host)", id)
	}
	if !strings.Contains(out.String(), "alfa") {
		t.Fatalf("tela nao desenhada: %q", out.String())
	}
	if !strings.Contains(out.String(), "[cliente-a]") {
		t.Fatalf("cabecalho de grupo ausente: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("erro inesperado: %q", errOut.String())
	}
}

func TestTUIArrowKeysSkipGroupHeaders(t *testing.T) {
	screen, _, _ := newTestTUI(t, "\x1b[B\x1b[B\r")
	id, err := screen.run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 30 {
		t.Fatalf("id = %d, esperado 30 (terceiro host, pulando cabecalhos)", id)
	}
}

func TestTUIJKgGKeys(t *testing.T) {
	screen, _, _ := newTestTUI(t, "jj\r")
	if id, err := screen.run(); err != nil || id != 30 {
		t.Fatalf("j j Enter = %d (%v), esperado 30", id, err)
	}

	screen, _, _ = newTestTUI(t, "G\r")
	if id, err := screen.run(); err != nil || id != 30 {
		t.Fatalf("G Enter = %d (%v), esperado 30", id, err)
	}

	screen, _, _ = newTestTUI(t, "Ggg\r")
	if id, err := screen.run(); err != nil || id != 10 {
		t.Fatalf("g Enter = %d (%v), esperado 10", id, err)
	}
}

func TestTUIJumpByHostNumber(t *testing.T) {
	screen, _, _ := newTestTUI(t, "2\r")
	id, err := screen.run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 20 {
		t.Fatalf("id = %d, esperado 20 (host numero 2)", id)
	}
}

func TestTUIInvalidNumberShowsMessageAndKeepsMenu(t *testing.T) {
	screen, out, _ := newTestTUI(t, "9\r\r")
	id, err := screen.run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 10 {
		t.Fatalf("id = %d, esperado 10 apos numero invalido", id)
	}
	if !strings.Contains(out.String(), "host invalido: 9") {
		t.Fatalf("mensagem de erro ausente na tela: %q", out.String())
	}
}

func TestTUIFilterThenAccept(t *testing.T) {
	screen, out, _ := newTestTUI(t, "/homolog\r\r")
	id, err := screen.run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 30 {
		t.Fatalf("id = %d, esperado 30 (filtro por tag homolog)", id)
	}
	if !strings.Contains(out.String(), "filtro") {
		t.Fatalf("rodape sem aviso de filtro: %q", out.String())
	}
}

func TestTUIFilterBackspaceAndEscape(t *testing.T) {
	// digita "betax", apaga o x e confirma o filtro
	screen, _, _ := newTestTUI(t, "/betax\x7f\r\r")
	if id, err := screen.run(); err != nil || id != 20 {
		t.Fatalf("filtro com backspace = %d (%v), esperado 20", id, err)
	}
	// Esc dentro do filtro limpa e mantem a lista
	screen, _, _ = newTestTUI(t, "/zzz\x1b\r")
	if id, err := screen.run(); err != nil || id != 10 {
		t.Fatalf("Esc no filtro = %d (%v), esperado 10", id, err)
	}
}

func TestTUIFilterWithoutMatchesKeepsMenuOpen(t *testing.T) {
	// filtra por algo inexistente, aceita (nada selecionado) e depois limpa o
	// filtro com "/" + Esc para poder selecionar o primeiro host
	screen, out, _ := newTestTUI(t, "/zzzz\r\r/\x1b\r")
	id, err := screen.run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 10 {
		t.Fatalf("id = %d, esperado 10 depois de limpar o filtro", id)
	}
	if !strings.Contains(out.String(), "nenhum host selecionado") {
		t.Fatalf("aviso de lista vazia ausente: %q", out.String())
	}
}

func TestTUICancelKeys(t *testing.T) {
	for _, keys := range []string{"q", "\x1b", "\x03"} {
		screen, _, errOut := newTestTUI(t, keys)
		id, err := screen.run()
		if err != nil {
			t.Fatalf("run(%q): %v", keys, err)
		}
		if id != 0 {
			t.Fatalf("run(%q) = %d, esperado 0 (cancelado)", keys, id)
		}
		if !strings.Contains(errOut.String(), "cancelada") {
			t.Fatalf("aviso de cancelamento ausente para %q", keys)
		}
	}
}

func TestTUIWithoutTerminalRestoresScreen(t *testing.T) {
	screen, out, _ := newTestTUI(t, "\r")
	if _, err := screen.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, ansi.AltScreenOn) || !strings.Contains(text, ansi.AltScreenOf) {
		t.Fatalf("tela alternativa nao foi ligada/desligada: %q", text)
	}
	if !strings.Contains(text, ansi.CursorShow) {
		t.Fatalf("cursor nao foi restaurado: %q", text)
	}
}

// Sem terminal, o menu em tela cheia cai para o numerado em vez de nao mostrar
// nada (sshm sem argumentos precisa sempre dar alguma resposta na tela).
func TestTUIFallsBackToNumberedMenuWithoutTerminal(t *testing.T) {
	entrada := filepath.Join(t.TempDir(), "nao-e-terminal")
	if err := os.WriteFile(entrada, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(entrada)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	rows := testRows()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	id, err := Pick(rows, Options{
		Mode:   ModeTUI, // pedido explicito de tela cheia
		In:     strings.NewReader("2\n"),
		InFile: file, // arquivo comum: nao e um terminal
		Out:    out,
		Err:    errOut,
	})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if id != rows[1].ID {
		t.Fatalf("id = %d, esperado %d", id, rows[1].ID)
	}
	if !strings.Contains(errOut.String(), "menu numerado") {
		t.Fatalf("deveria avisar sobre o fallback: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "#") {
		t.Fatalf("o menu numerado deveria ter sido impresso: %q", out.String())
	}
}

func TestNumberedPick(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	id, err := Pick(testRows(), Options{
		Mode:   ModeNumber,
		In:     strings.NewReader("2\n"),
		Out:    out,
		Err:    errOut,
		InFile: nil,
	})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if id != 20 {
		t.Fatalf("id = %d, esperado 20", id)
	}
	if !strings.Contains(out.String(), "alfa") || !strings.Contains(out.String(), "#") {
		t.Fatalf("tabela numerada inesperada: %q", out.String())
	}
}

func TestNumberedPickCancelAndInvalid(t *testing.T) {
	buffer := func() *bytes.Buffer { return &bytes.Buffer{} }
	id, err := Pick(testRows(), Options{Mode: ModeNumber, In: strings.NewReader("\n"), Out: buffer(), Err: buffer()})
	if err != nil || id != 0 {
		t.Fatalf("Enter deveria cancelar: id=%d err=%v", id, err)
	}

	if _, err := Pick(testRows(), Options{Mode: ModeNumber, In: strings.NewReader("9\n"), Out: buffer(), Err: buffer()}); err == nil {
		t.Fatal("numero fora da lista deveria falhar")
	}
	if _, err := Pick(testRows(), Options{Mode: ModeNumber, In: strings.NewReader("abc\n"), Out: buffer(), Err: buffer()}); err == nil {
		t.Fatal("entrada nao numerica deveria falhar")
	}
}

func TestPickWithEmptyList(t *testing.T) {
	if _, err := Pick(nil, Options{Mode: ModeNumber, In: strings.NewReader("1\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); err == nil {
		t.Fatal("lista vazia deveria falhar")
	}
}

func TestResolveModeExplicitValues(t *testing.T) {
	cases := map[Mode]Mode{
		ModeNumber: ModeNumber,
		"numerado": ModeNumber,
		ModeTUI:    ModeTUI,
		"menu":     ModeTUI,
		ModeFZF:    ModeFZF,
	}
	for input, want := range cases {
		if got := ResolveMode(input, nil); got != want {
			t.Fatalf("ResolveMode(%q) = %q, esperado %q", input, got, want)
		}
	}
	switch got := ResolveMode(ModeAuto, nil); got {
	case ModeNumber, ModeTUI, ModeFZF:
	default:
		t.Fatalf("ResolveMode(auto) = %q", got)
	}
}

func TestEnvModeDefault(t *testing.T) {
	t.Setenv(EnvMode, "number")
	if got := EnvModeOrDefault(); got != ModeNumber {
		t.Fatalf("EnvModeOrDefault = %q", got)
	}
}

func TestFuzzyMatchAndRowMatches(t *testing.T) {
	if !fuzzyMatch("", "qualquer") {
		t.Fatal("filtro vazio deveria casar")
	}
	if !fuzzyMatch("bt", "beta") {
		t.Fatal("subsequencia deveria casar")
	}
	if fuzzyMatch("zz", "beta") {
		t.Fatal("padrao ausente deveria falhar")
	}
	if !fuzzyMatch("BETA", "beta") {
		t.Fatal("match deveria ignorar maiusculas")
	}

	row := testRows()[0]
	if !rowMatches(row, "prod") || !rowMatches(row, "10.0.0.1") || !rowMatches(row, "cliente") {
		t.Fatal("filtro deveria casar por tag, host e grupo")
	}
	if rowMatches(row, "beta") {
		t.Fatal("filtro nao deveria casar com outro host")
	}
}

func TestKeyReaderTranslatesSequences(t *testing.T) {
	cases := map[string]keyKind{
		"\x1b[A":  keyUp,
		"\x1b[B":  keyDown,
		"\x1b[H":  keyHome,
		"\x1b[F":  keyEnd,
		"\x1b[5~": keyPageUp,
		"\x1b[6~": keyPageDown,
		"\x1b[3~": keyDelete,
		"\x1b":    keyEscape,
		"\r":      keyEnter,
		"\x7f":    keyBackspace,
		"\x03":    keyCtrlC,
		"a":       keyRune,
		"á":       keyRune,
	}
	for input, want := range cases {
		event, err := readKey(bufio.NewReader(strings.NewReader(input)))
		if err != nil {
			t.Fatalf("readKey(%q): %v", input, err)
		}
		if event.kind != want {
			t.Fatalf("readKey(%q) = %v, esperado %v", input, event.kind, want)
		}
	}
}
