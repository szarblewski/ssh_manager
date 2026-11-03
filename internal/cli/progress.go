package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/szarblewski/ssh_manager/internal/sshx"
	"github.com/szarblewski/ssh_manager/internal/transfer"
)

// minProgressInterval limita a quantas vezes a barra e redesenhada, para nao
// inundar o terminal (e o console do Windows) com escritas.
const minProgressInterval = 100 * time.Millisecond

// copyProgress desenha uma barra de progresso de uma linha durante a copia.
//
// So aparece quando o erro padrao e um terminal: em pipe/arquivo (scripts,
// logs) nada e escrito, como acontece com as cores da listagem.
type copyProgress struct {
	out     *os.File
	width   int
	last    time.Time
	file    string
	start   time.Time
	visible bool
}

func newCopyProgress(w io.Writer, quiet bool) *copyProgress {
	p := &copyProgress{width: 80}
	if quiet {
		return p
	}
	file, ok := w.(*os.File)
	if !ok || !sshx.IsTerminal(file) {
		return p
	}
	p.out = file
	if width, _ := sshx.TerminalSize(file); width > 0 {
		p.width = width
	}
	return p
}

// Enabled diz se ha terminal para desenhar a barra.
func (p *copyProgress) Enabled() bool { return p.out != nil }

// Update redesenha a barra; o ultimo bloco de cada arquivo sempre e desenhado.
func (p *copyProgress) Update(update transfer.Progress) {
	if p.out == nil {
		return
	}
	now := time.Now()
	finished := update.Total > 0 && update.Done >= update.Total
	if p.visible && !finished && now.Sub(p.last) < minProgressInterval {
		return
	}
	p.last = now
	p.draw(update)
}

func (p *copyProgress) draw(update transfer.Progress) {
	if update.File != p.file {
		p.file = update.File
		p.start = time.Now()
	}
	speed := 0.0
	if elapsed := time.Since(p.start).Seconds(); elapsed > 0.05 {
		speed = float64(update.Done) / elapsed
	}
	fmt.Fprint(p.out, "\r"+progressLine(p.width, update.File, update.Done, update.Total, speed))
	p.visible = true
}

// Clear apaga a barra antes de uma mensagem normal, deixando a linha vazia.
func (p *copyProgress) Clear() {
	if p.out == nil || !p.visible {
		return
	}
	fmt.Fprint(p.out, "\r"+strings.Repeat(" ", p.width-1)+"\r")
	p.visible = false
}

// Finish apaga a barra ao final da copia.
func (p *copyProgress) Finish() { p.Clear() }

// progressLine monta a linha da barra, sem o "\r" e sem quebrar a linha.
func progressLine(width int, name string, done, total int64, speed float64) string {
	if width < 20 {
		width = 80
	}

	percent := "  ?"
	ratio := 0.0
	if total > 0 {
		ratio = float64(done) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
		percent = fmt.Sprintf("%3.0f%%", ratio*100)
	}

	detail := percent + " " + transfer.HumanSize(done)
	if total > 0 {
		detail += "/" + transfer.HumanSize(total)
	}
	if speed > 0 {
		detail += " " + transfer.HumanSize(int64(speed)) + "/s"
	}

	// reparte o que sobrou: o nome vem primeiro (e o que o usuario procura),
	// a barra fica com o resto, entre 10 e 40 colunas.
	available := width - 1 - len(detail) - 5 // " [" + "] " e o respiro
	if available < 14 {
		available = 14
	}
	nameWidth := available - 10
	if limit := available / 2; nameWidth > limit {
		nameWidth = limit
	}
	if nameWidth < 8 {
		nameWidth = 8
	}
	barWidth := available - nameWidth
	if barWidth > 40 {
		barWidth, nameWidth = 40, available-40
	}
	if barWidth < 10 {
		barWidth, nameWidth = 10, available-10
	}
	if nameWidth < 4 {
		nameWidth = 4
	}

	filled := int(float64(barWidth) * ratio)
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)

	name = truncateLeft(name, nameWidth)
	line := fmt.Sprintf("%s [%s] %s", name, bar, detail)
	if padding := width - 1 - utf8.RuneCountInString(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

// truncateLeft corta o inicio do caminho, preservando o nome do arquivo (o que
// interessa na barra).
func truncateLeft(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[len(runes)-max:])
	}
	return ".." + string(runes[len(runes)-(max-2):])
}
