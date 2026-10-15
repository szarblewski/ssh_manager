// Package ansi cuida das cores e do terminal: define os codigos usados pelo
// sshm, decide quando vale a pena colorir e abre o console diretamente quando a
// saida padrao esta redirecionada.
package ansi

import (
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Codigos ANSI usados na interface.
const (
	Reset       = "\033[0m"
	Bold        = "\033[1m"
	Dim         = "\033[2m"
	Italic      = "\033[3m"
	Inverse     = "\033[7m"
	Red         = "\033[31m"
	Green       = "\033[32m"
	Yellow      = "\033[33m"
	Blue        = "\033[34m"
	Magenta     = "\033[35m"
	Cyan        = "\033[36m"
	BoldCyan    = "\033[1;36m"
	BoldWhite   = "\033[1;37m"
	AltScreenOn = "\033[?1049h"
	AltScreenOf = "\033[?1049l"
	CursorHide  = "\033[?25l"
	CursorShow  = "\033[?25h"
	ClearLine   = "\033[K"
	Home        = "\033[H"
)

// Painter aplica um codigo ANSI quando as cores estao ligadas.
type Painter struct{ Enabled bool }

// Paint envolve o texto com o codigo (nada acontece com as cores desligadas).
func (p Painter) Paint(code, text string) string {
	if !p.Enabled || text == "" {
		return text
	}
	return code + text + Reset
}

// IsTerminal diz se o arquivo e um terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Enabled decide se vale usar cor no modo automatico: respeita --no-color, a
// variavel NO_COLOR e o fato de a saida ser (ou nao) um terminal.
func Enabled(noColorFlag bool, f *os.File) bool {
	return ColorEnabled("auto", noColorFlag, f)
}

// ColorEnabled decide se vale usar cor a partir do modo pedido:
//
//	"auto"   (padrao) colore apenas em terminal, respeitando NO_COLOR/--no-color;
//	"always" forca cor (util para 'sshm list | less -R' e para logs);
//	"never"  desliga.
func ColorEnabled(mode string, noColorFlag bool, f *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "sempre", "on", "yes":
		return true
	case "never", "nunca", "off", "no":
		return false
	}
	if noColorFlag {
		return false
	}
	if value, set := os.LookupEnv("NO_COLOR"); set && strings.TrimSpace(value) != "" {
		return false
	}
	if termName := strings.ToLower(os.Getenv("TERM")); termName == "dumb" {
		return false
	}
	return IsTerminal(f)
}

// Disabled devolve um painter desligado (usado em testes e em pipes).
func Disabled() Painter { return Painter{Enabled: false} }

// escapeSequenceEnd devolve o indice logo apos a sequencia ANSI iniciada em i.
func escapeSequenceEnd(text string, i int) int {
	if i >= len(text) || text[i] != '\033' {
		return i
	}
	for j := i + 1; j < len(text); j++ {
		if (text[j] >= 'a' && text[j] <= 'z') || (text[j] >= 'A' && text[j] <= 'Z') {
			return j + 1
		}
	}
	return i
}

// Strip remove as sequencias ANSI, deixando so o texto visivel.
func Strip(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if end := escapeSequenceEnd(text, i); end > i {
			i = end
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// VisibleLen conta os caracteres visiveis (ignorando as sequencias ANSI).
func VisibleLen(text string) int {
	return len([]rune(Strip(text)))
}

// Truncate corta o texto no numero de caracteres visiveis, sem partir uma
// sequencia ANSI no meio (necessario porque as linhas ja vem coloridas).
func Truncate(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if VisibleLen(text) <= limit {
		return text
	}
	var out strings.Builder
	count := 0
	for i := 0; i < len(text); {
		if end := escapeSequenceEnd(text, i); end > i {
			out.WriteString(text[i:end])
			i = end
			continue
		}
		if count == limit-1 {
			out.WriteString("…")
			break
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		out.WriteRune(r)
		count++
		i += size
	}
	return out.String() + Reset
}
