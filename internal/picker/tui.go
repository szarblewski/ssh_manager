package picker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/szarblewski/ssh_manager/internal/ansi"
	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/table"
)

// displayItem e uma linha da tela: cabecalho de grupo ou host.
type displayItem struct {
	header bool
	text   string
	row    config.Row
	number int // numero do host na lista completa (1..N)
}

type tui struct {
	rows    []config.Row
	grouped bool

	in      *os.File
	outW    io.Writer
	errW    io.Writer
	outFile *os.File
	style   table.Style

	reader *bufio.Reader

	filter    string
	filtering bool
	digits    string
	message   string
	cursor    int
	offset    int
	keys      int // quantas teclas foram lidas (0 = entrada fechada)
}

// errNoRawMode indica que o terminal nao aceitou o modo bruto.
var errNoRawMode = errors.New("modo bruto indisponivel")

// tuiPick abre o menu em tela cheia.
//
// Sem terminal (ou quando nao da para preparar o modo bruto, ou quando o
// terminal nao entrega tecla alguma) cai no menu numerado: e melhor mostrar a
// lista para digitar um numero do que sair sem nada na tela.
func tuiPick(rows []config.Row, opts Options) (int, error) {
	if !isTerminal(opts.InFile) {
		fmt.Fprintln(opts.Err, "sshm: sem terminal interativo; usando o menu numerado.")
		return numberedPick(rows, opts)
	}

	screen := &tui{
		rows:    rows,
		grouped: opts.Grouped,
		in:      opts.InFile,
		outW:    opts.Out,
		errW:    opts.Err,
		outFile: opts.OutFile,
		style:   opts.Style,
		reader:  bufio.NewReader(opts.InFile),
	}
	// o menu desenha no console, e nao no stdout capturado: assim aparece mesmo
	// com a saida redirecionada (sshm > arquivo.txt) ou em consoles integrados
	if console, restore, err := ansi.PrepareConsole(); err == nil {
		defer restore()
		screen.outW = console
		screen.outFile = console
		// desenhando direto no console, cor e seguro mesmo com stdout em pipe
		screen.style = table.NewStyle(true)
	}

	id, err := screen.run()
	if errors.Is(err, errNoRawMode) && screen.keys == 0 {
		fmt.Fprintln(opts.Err, "sshm: nao consegui preparar o menu em tela cheia; usando o menu numerado.")
		return numberedPick(rows, opts)
	}
	if err != nil {
		return 0, err
	}
	if id == 0 && screen.keys == 0 {
		fmt.Fprintln(opts.Err, "sshm: o menu em tela cheia nao recebeu teclas; usando o menu numerado.")
		return numberedPick(rows, opts)
	}
	return id, nil
}

// run desenha o menu e devolve o id escolhido (0 se cancelar). O numero de
// teclas lidas fica em t.keys, usado para decidir o fallback.
func (t *tui) run() (int, error) {
	restore, err := t.enterFullScreen()
	if err != nil {
		return 0, err
	}
	defer restore()

	for {
		items := t.items()
		t.clamp(items)
		t.render(items)

		key, err := readKey(t.reader)
		if err != nil {
			if t.keys == 0 {
				return 0, nil // entrada fechada: quem chamou decide o que fazer
			}
			fmt.Fprintln(t.errW, "sshm: selecao cancelada.")
			return 0, nil
		}
		t.keys++

		switch t.act(key, items) {
		case actionAccept:
			if id := t.selectedID(items); id != 0 {
				return id, nil
			}
			t.message = "nenhum host selecionado (Esc limpa o filtro)"
		case actionCancel:
			fmt.Fprintln(t.errW, "sshm: selecao cancelada.")
			return 0, nil
		}
	}
}

func (t *tui) enterFullScreen() (func(), error) {
	restoreTerm := func() {}
	if isTerminal(t.in) {
		fd := int(t.in.Fd())
		state, err := term.MakeRaw(fd)
		if err != nil {
			return nil, errNoRawMode
		}
		restoreTerm = func() { _ = term.Restore(fd, state) }
	}
	_ = ansi.EnableVirtualTerminal(t.outFile)
	fmt.Fprint(t.outW, ansi.AltScreenOn+ansi.CursorHide)
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		fmt.Fprint(t.outW, ansi.CursorShow+ansi.AltScreenOf)
		restoreTerm()
	}
	return restore, nil
}

type action int

const (
	actionNone action = iota
	actionAccept
	actionCancel
)

// act aplica a tecla ao estado da tela.
func (t *tui) act(key keyEvent, items []displayItem) action {
	t.message = ""

	if key.kind == keyCtrlC {
		return actionCancel
	}

	// modo filtro: qualquer texto vai para o filtro
	if t.filtering {
		switch key.kind {
		case keyEscape:
			t.filter, t.filtering, t.digits = "", false, ""
			t.cursor, t.offset = 0, 0
		case keyEnter:
			t.filtering = false
			t.cursor, t.offset = 0, 0
		case keyBackspace, keyDelete:
			if t.filter != "" {
				runes := []rune(t.filter)
				t.filter = string(runes[:len(runes)-1])
			}
			t.cursor, t.offset = 0, 0
		case keyCtrlU:
			t.filter = ""
			t.cursor, t.offset = 0, 0
		case keyRune:
			t.filter += string(key.r)
			t.cursor, t.offset = 0, 0
		}
		return actionNone
	}

	switch key.kind {
	case keyEscape:
		return actionCancel
	case keyEnter:
		if t.digits != "" {
			number, err := strconv.Atoi(t.digits)
			if err == nil && t.jumpTo(items, number) {
				// igual ao sshm.sh: numero + Enter ja conecta no host
				t.digits = ""
				return actionAccept
			}
			t.message = fmt.Sprintf("host invalido: %s (use 1..%d)", t.digits, len(t.rows))
			t.digits = ""
			return actionNone
		}
		return actionAccept
	case keyUp:
		t.move(items, -1)
	case keyDown:
		t.move(items, 1)
	case keyHome:
		t.cursor = 0
	case keyEnd:
		t.cursor = len(items) - 1
	case keyPageUp:
		t.cursor -= t.pageSize()
		t.clamp(items)
	case keyPageDown:
		t.cursor += t.pageSize()
		t.clamp(items)
	case keyBackspace:
		if t.digits != "" {
			t.digits = t.digits[:len(t.digits)-1]
		}
	case keyRune:
		switch {
		case key.r == 'q' || key.r == 'Q':
			return actionCancel
		case key.r == 'j':
			t.move(items, 1)
		case key.r == 'k':
			t.move(items, -1)
		case key.r == 'g':
			t.cursor = 0
		case key.r == 'G':
			t.cursor = len(items) - 1
		case key.r == '/':
			t.filtering = true
		case key.r == ' ':
			t.cursor += t.pageSize()
			t.clamp(items)
		case key.r >= '0' && key.r <= '9':
			if len(t.digits) < 5 {
				t.digits += string(key.r)
			}
		}
	}
	t.clamp(items)
	return actionNone
}

// move anda para cima/baixo pulando os cabecalhos de grupo.
func (t *tui) move(items []displayItem, step int) {
	if len(items) == 0 {
		return
	}
	index := t.cursor + step
	for index >= 0 && index < len(items) && items[index].header {
		index += step
	}
	if index < 0 {
		// da a volta na lista
		index = len(items) - 1
		for index >= 0 && items[index].header {
			index--
		}
	}
	if index >= len(items) {
		index = 0
		for index < len(items) && items[index].header {
			index++
		}
	}
	if index >= 0 && index < len(items) {
		t.cursor = index
	}
}

func (t *tui) jumpTo(items []displayItem, number int) bool {
	for index, item := range items {
		if !item.header && item.number == number {
			t.cursor = index
			return true
		}
	}
	return false
}

// clamp mantem o cursor em uma linha de host.
func (t *tui) clamp(items []displayItem) {
	if len(items) == 0 {
		t.cursor, t.offset = 0, 0
		return
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= len(items) {
		t.cursor = len(items) - 1
	}
	if !items[t.cursor].header {
		return
	}
	for i := t.cursor; i < len(items); i++ {
		if !items[i].header {
			t.cursor = i
			return
		}
	}
	for i := t.cursor; i >= 0; i-- {
		if !items[i].header {
			t.cursor = i
			return
		}
	}
	t.cursor, t.offset = 0, 0
}

func (t *tui) selectedID(items []displayItem) int {
	if t.cursor < 0 || t.cursor >= len(items) {
		return 0
	}
	if items[t.cursor].header {
		return 0
	}
	return items[t.cursor].row.ID
}

// items monta as linhas visiveis aplicando o filtro.
func (t *tui) items() []displayItem {
	var items []displayItem
	lastGroup := ""
	grouped := t.grouped && config.RowsHaveGroup(t.rows)
	for index, row := range t.rows {
		if !rowMatches(row, t.filter) {
			continue
		}
		if grouped && row.Group != lastGroup {
			items = append(items, displayItem{header: true, text: "[" + row.GroupLabel() + "]", row: row})
			lastGroup = row.Group
		}
		items = append(items, displayItem{
			text:   t.line(row, grouped),
			row:    row,
			number: index + 1,
		})
	}
	return items
}

// line monta uma linha de host com as colunas ja destacadas.
//
// O preenchimento e feito ANTES de colorir: as sequencias ANSI contam como
// caracteres no %-18s e desalinhariam as colunas.
func (t *tui) line(row config.Row, grouped bool) string {
	cells := table.Cells(row, grouped)
	pad := func(width int, text string, paint func(string) string) string {
		if missing := width - len([]rune(text)); missing > 0 {
			text += strings.Repeat(" ", missing)
		}
		return paint(text)
	}

	if !grouped {
		return fmt.Sprintf("%4d) %s %s %s %s %s",
			row.ID,
			pad(18, cells[1], t.style.Name),
			pad(26, cells[2], t.style.Target),
			pad(5, cells[3], t.style.Port),
			pad(12, cells[4], groupPainter(t.style, cells[4])),
			t.style.Tags(cells[5]))
	}
	return fmt.Sprintf("%4d) %s %s %s %s",
		row.ID,
		pad(18, cells[1], t.style.Name),
		pad(26, cells[2], t.style.Target),
		pad(5, cells[3], t.style.Port),
		t.style.Tags(cells[4]))
}

// groupPainter escolhe o destaque da coluna GRUPO ("-" fica apagado).
func groupPainter(style table.Style, cell string) func(string) string {
	if cell == "-" || cell == "" {
		return style.Muted
	}
	return style.Group
}

func (t *tui) pageSize() int {
	_, height := t.size()
	size := height - 2
	if size < 1 {
		size = 1
	}
	return size
}

func (t *tui) size() (width, height int) {
	if f := t.outFile; isTerminal(f) {
		if w, h, err := term.GetSize(int(f.Fd())); err == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	return 80, 24
}

func (t *tui) render(items []displayItem) {
	width, height := t.size()
	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	if maxOffset := len(items) - visible; t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}

	var screen strings.Builder
	screen.WriteString(ansi.Home)

	title := fmt.Sprintf(" sshm - %d host(s)", len(t.rows))
	if t.grouped && config.RowsHaveGroup(t.rows) {
		title += " (agrupado por grupo)"
	}
	screen.WriteString(t.bar(table.Truncate(title, width), width))
	screen.WriteString(ansi.ClearLine + "\n")
	for i := 0; i < visible; i++ {
		index := t.offset + i
		if index >= len(items) {
			screen.WriteString(ansi.ClearLine + "\n")
			continue
		}
		item := items[index]
		text := ansi.Truncate(item.text, width)
		switch {
		case index == t.cursor && !item.header:
			screen.WriteString(t.bar(text, width))
		case item.header:
			screen.WriteString(t.groupTitle(item))
		default:
			screen.WriteString(text)
		}
		screen.WriteString(ansi.ClearLine + "\n")
	}
	screen.WriteString(t.bar(table.Truncate(t.footer(), width), width) + ansi.ClearLine)

	fmt.Fprint(t.outW, screen.String())
}

// groupTitle destaca o cabecalho de grupo na lista.
func (t *tui) groupTitle(item displayItem) string {
	if item.row.Group == "" {
		return t.style.Muted(item.text)
	}
	return t.style.Group(item.text)
}

func (t *tui) footer() string {
	switch {
	case t.message != "":
		return " " + t.message
	case t.filtering:
		return fmt.Sprintf(" filtro: %s_   (Enter confirma, Esc limpa)", t.filter)
	case t.filter != "":
		return fmt.Sprintf(" filtro ativo: %s   (/ editar, Esc limpa)   %d resultado(s)", t.filter, countHosts(t.items()))
	case t.digits != "":
		return fmt.Sprintf(" ir para o host: %s_   (Enter confirma, Backspace apaga)", t.digits)
	default:
		return " setas/j k: mover   g/G: topo/fim   numero+Enter: ir   /: filtrar   Enter: conectar   q: sair"
	}
}

func (t *tui) bar(text string, width int) string {
	padded := text + strings.Repeat(" ", maxInt(0, width-len([]rune(text))))
	return t.style.Selected(padded)
}

func countHosts(items []displayItem) int {
	total := 0
	for _, item := range items {
		if !item.header {
			total++
		}
	}
	return total
}

func isTerminal(f *os.File) bool { return ansi.IsTerminal(f) }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
