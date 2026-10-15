// Package picker escolhe um host na lista: menu navegavel em tela cheia (padrao,
// sem dependencias), fzf quando instalado, ou menu numerado quando a entrada nao
// e um terminal - os mesmos seletors do sshm.sh, controlados por SSHM_PICKER.
package picker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/table"
)

// Mode identifica o seletor usado.
type Mode string

const (
	// ModeAuto escolhe sozinho (fzf, tela cheia ou numerado).
	ModeAuto Mode = "auto"
	// ModeTUI forca o menu navegavel em tela cheia.
	ModeTUI Mode = "tui"
	// ModeNumber forca o menu numerado.
	ModeNumber Mode = "number"
	// ModeFZF forca o seletor fuzzy (exige fzf instalado).
	ModeFZF Mode = "fzf"
)

// EnvMode e a variavel que forca o seletor (mesma do sshm.sh).
const EnvMode = "SSHM_PICKER"

// Options controla a selecao.
type Options struct {
	Mode    Mode
	Grouped bool
	Style   table.Style
	In      io.Reader
	InFile  *os.File
	Out     io.Writer
	OutFile *os.File
	Err     io.Writer
}

// DefaultOptions devolve as opcoes com as entradas/saidas padrao do processo.
func DefaultOptions() Options {
	return Options{
		Style:   table.NewStyle(false),
		In:      os.Stdin,
		InFile:  os.Stdin,
		Out:     os.Stdout,
		OutFile: os.Stdout,
		Err:     os.Stderr,
	}
}

// Pick devolve o ID do host escolhido (0 quando o usuario cancela).
func Pick(rows []config.Row, opts Options) (int, error) {
	if len(rows) == 0 {
		return 0, errors.New("nenhum host para selecionar")
	}
	opts = opts.withDefaults()
	if mode := opts.Mode; !ValidMode(mode) {
		return 0, fmt.Errorf("seletor invalido: %q (use fzf, tui ou number)", mode)
	}

	switch ResolveMode(opts.Mode, opts.InFile) {
	case ModeFZF:
		return fzfPick(rows, opts)
	case ModeTUI:
		return tuiPick(rows, opts)
	case ModeNumber:
		return numberedPick(rows, opts)
	default:
		return 0, fmt.Errorf("seletor invalido: %q (use fzf, tui ou number)", opts.Mode)
	}
}

// ValidMode diz se o valor pedido e um seletor conhecido.
func ValidMode(mode Mode) bool {
	switch Mode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case "", ModeAuto, ModeTUI, "menu", "navegavel", ModeNumber, "numerado", ModeFZF:
		return true
	default:
		return false
	}
}

// ResolveMode decide o seletor efetivo a partir do modo pedido.
func ResolveMode(mode Mode, in *os.File) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case ModeFZF:
		return ModeFZF
	case ModeTUI, "menu", "navegavel":
		return ModeTUI
	case ModeNumber, "numerado":
		return ModeNumber
	default:
		// vazio, "auto" ou valor desconhecido: escolhe pelo ambiente
		return autoMode(in)
	}
}

func autoMode(in *os.File) Mode {
	if _, err := exec.LookPath("fzf"); err == nil {
		return ModeFZF
	}
	if isTerminal(in) {
		return ModeTUI
	}
	return ModeNumber
}

// EnvMode devolve o seletor pedido em SSHM_PICKER.
func EnvModeOrDefault() Mode {
	return Mode(strings.TrimSpace(os.Getenv(EnvMode)))
}

func (o Options) withDefaults() Options {
	defaults := DefaultOptions()
	if o.In == nil {
		o.In = defaults.In
	}
	if o.InFile == nil {
		o.InFile = defaults.InFile
	}
	if o.Out == nil {
		o.Out = defaults.Out
	}
	if o.OutFile == nil {
		o.OutFile = defaults.OutFile
	}
	if o.Err == nil {
		o.Err = defaults.Err
	}
	return o
}

// --- menu numerado (quando a entrada nao e um terminal) ---

func numberedPick(rows []config.Row, opts Options) (int, error) {
	grouped := opts.Grouped && config.RowsHaveGroup(rows)

	// a numeracao e a posicao na lista (como no sshm.sh): o ID aparece so na
	// listagem, para nao confundir "2)" com o id 2
	cells := make([][]string, 0, len(rows))
	for i, row := range rows {
		line := append([]string{fmt.Sprintf("%d)", i+1)}, table.Cells(row, grouped)[1:]...)
		cells = append(cells, line)
	}
	headers := append([]string{"#"}, headersFor(rows, grouped)[1:]...)
	paints := append([]func(string) string{opts.Style.Number}, opts.Style.CellPainters(grouped)...)

	fmt.Fprintln(opts.Err, "Selecione um host:")
	fmt.Fprint(opts.Out, table.RenderStyled(headers, cells, paints))

	fmt.Fprint(opts.Err, "Numero (Enter para cancelar): ")
	line, err := bufio.NewReader(opts.In).ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		fmt.Fprintln(opts.Err, "sshm: selecao cancelada.")
		return 0, nil
	}
	if line == "" {
		fmt.Fprintln(opts.Err, "sshm: selecao cancelada.")
		return 0, nil
	}
	number, err := strconv.Atoi(line)
	if err != nil || number < 1 || number > len(rows) {
		return 0, fmt.Errorf("opcao invalida: %q (use 1..%d)", line, len(rows))
	}
	return rows[number-1].ID, nil
}

// --- seletor fuzzy via fzf ---

func fzfPick(rows []config.Row, opts Options) (int, error) {
	path, err := exec.LookPath("fzf")
	if err != nil {
		// fzf sumiu do PATH: cai no menu interno em vez de falhar
		fmt.Fprintln(opts.Err, "sshm: fzf nao esta instalado; usando o menu interno.")
		if isTerminal(opts.InFile) {
			return tuiPick(rows, opts)
		}
		return numberedPick(rows, opts)
	}
	lines := table.Lines(rows, opts.Grouped && config.RowsHaveGroup(rows))
	header := strings.Join(headersFor(rows, opts.Grouped), " ")

	cmd := exec.Command(path,
		"--header="+header,
		"--prompt=sshm> ",
		"--height=60%",
		"--border",
		"--no-multi",
		"--with-nth=1..",
	)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Stderr = opts.Err
	var picked strings.Builder
	cmd.Stdout = &picked
	if err := cmd.Run(); err != nil {
		// fzf devolve erro quando o usuario cancela (Esc/Ctrl+C)
		fmt.Fprintln(opts.Err, "sshm: selecao cancelada.")
		return 0, nil
	}
	fields := strings.Fields(strings.TrimSpace(picked.String()))
	if len(fields) == 0 {
		fmt.Fprintln(opts.Err, "sshm: selecao cancelada.")
		return 0, nil
	}
	id, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("nao consegui interpretar a selecao do fzf: %q", picked.String())
	}
	return id, nil
}

func headersFor(rows []config.Row, grouped bool) []string {
	if grouped && config.RowsHaveGroup(rows) {
		return table.HeaderGrouped()
	}
	return table.HeaderFlat()
}
