package picker

import (
	"bufio"
)

// keyKind identifica uma tecla lida do terminal.
type keyKind int

const (
	keyRune keyKind = iota
	keyEnter
	keyEscape
	keyUp
	keyDown
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyBackspace
	keyDelete
	keyCtrlC
	keyCtrlL
	keyCtrlU
	keyOther
)

// keyEvent e uma tecla (com o caractere, quando for texto).
type keyEvent struct {
	kind keyKind
	r    rune
}

// readKey le uma tecla, traduzindo as sequencias ANSI de setas e PageUp/PageDown.
func readKey(reader *bufio.Reader) (keyEvent, error) {
	b, err := reader.ReadByte()
	if err != nil {
		return keyEvent{}, err
	}
	switch b {
	case 3:
		return keyEvent{kind: keyCtrlC}, nil
	case 12:
		return keyEvent{kind: keyCtrlL}, nil
	case 21:
		return keyEvent{kind: keyCtrlU}, nil
	case 13, 10:
		return keyEvent{kind: keyEnter}, nil
	case 127, 8:
		return keyEvent{kind: keyBackspace}, nil
	case 27:
		return readEscape(reader)
	}
	if b < 32 {
		return keyEvent{kind: keyOther}, nil
	}
	return keyEvent{kind: keyRune, r: decodeRune(reader, b)}, nil
}

// readEscape le o que vem depois do ESC. Sem mais bytes disponiveis o ESC
// sozinho e interpretado como cancelar (as sequencias chegam em um unico bloco).
func readEscape(reader *bufio.Reader) (keyEvent, error) {
	if reader.Buffered() == 0 {
		return keyEvent{kind: keyEscape}, nil
	}
	second, err := reader.ReadByte()
	if err != nil {
		return keyEvent{kind: keyEscape}, nil
	}
	switch second {
	case '[', 'O':
		return readCSI(reader)
	default:
		if err := reader.UnreadByte(); err != nil {
			return keyEvent{kind: keyEscape}, nil
		}
		return keyEvent{kind: keyEscape}, nil
	}
}

func readCSI(reader *bufio.Reader) (keyEvent, error) {
	third, err := reader.ReadByte()
	if err != nil {
		return keyEvent{kind: keyEscape}, nil
	}
	switch third {
	case 'A':
		return keyEvent{kind: keyUp}, nil
	case 'B':
		return keyEvent{kind: keyDown}, nil
	case 'H':
		return keyEvent{kind: keyHome}, nil
	case 'F':
		return keyEvent{kind: keyEnd}, nil
	case '1', '5', '6', '3', '4':
		// teclas com "~" no fim: 1~ home, 4~ end, 5~ page up, 6~ page down, 3~ delete
		for {
			next, err := reader.ReadByte()
			if err != nil {
				return keyEvent{kind: keyEscape}, nil
			}
			if next != '~' {
				continue
			}
			switch third {
			case '1':
				return keyEvent{kind: keyHome}, nil
			case '4':
				return keyEvent{kind: keyEnd}, nil
			case '5':
				return keyEvent{kind: keyPageUp}, nil
			case '6':
				return keyEvent{kind: keyPageDown}, nil
			case '3':
				return keyEvent{kind: keyDelete}, nil
			}
			return keyEvent{kind: keyOther}, nil
		}
	}
	return keyEvent{kind: keyOther}, nil
}

// decodeRune junta os bytes restantes de um caractere UTF-8.
func decodeRune(reader *bufio.Reader, first byte) rune {
	size := utf8Size(first)
	if size <= 1 {
		return rune(first)
	}
	rest := make([]byte, size-1)
	for i := range rest {
		b, err := reader.ReadByte()
		if err != nil {
			break
		}
		rest[i] = b
	}
	runes := []rune(string(append([]byte{first}, rest...)))
	if len(runes) == 0 {
		return rune(first)
	}
	return runes[0]
}

func utf8Size(first byte) int {
	switch {
	case first&0x80 == 0:
		return 1
	case first&0xE0 == 0xC0:
		return 2
	case first&0xF0 == 0xE0:
		return 3
	case first&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}
