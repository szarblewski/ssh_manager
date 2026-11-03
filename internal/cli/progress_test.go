package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/szarblewski/ssh_manager/internal/transfer"
)

func TestProgressLineShowsPercentAndFitsWidth(t *testing.T) {
	line := progressLine(80, "srv-a:/var/log/syslog", 512, 1024, 2048)
	if !strings.Contains(line, " 50%") {
		t.Fatalf("linha sem a porcentagem: %q", line)
	}
	if !strings.Contains(line, "[") || !strings.Contains(line, "]") {
		t.Fatalf("linha sem a barra: %q", line)
	}
	if !strings.Contains(line, "512 B/1.0 KiB") {
		t.Fatalf("linha sem os tamanhos: %q", line)
	}
	if got := utf8.RuneCountInString(line); got > 80 {
		t.Fatalf("linha com %d colunas: %q", got, line)
	}
}

func TestProgressLineWithoutTotal(t *testing.T) {
	line := progressLine(80, "arquivo", 1024, 0, 0)
	if !strings.Contains(line, "  ?") {
		t.Fatalf("sem total deveria mostrar ?: %q", line)
	}
	if strings.Contains(line, "/") {
		t.Fatalf("sem total nao deveria mostrar fracao: %q", line)
	}
}

func TestProgressLineTruncatesLongPath(t *testing.T) {
	long := "srv-a:/uma/pasta/muito/comprida/que/nao/cabe/na/linha/arquivo-final.log"
	line := progressLine(60, long, 10, 20, 0)
	if utf8.RuneCountInString(line) > 60 {
		t.Fatalf("linha larga demais (%d): %q", utf8.RuneCountInString(line), line)
	}
	if !strings.Contains(line, "arquivo-final.log") {
		t.Fatalf("deveria manter o nome do arquivo: %q", line)
	}
	if !strings.HasPrefix(line, "..") {
		t.Fatalf("deveria marcar o corte no inicio: %q", line)
	}
}

func TestTruncateLeft(t *testing.T) {
	if got := truncateLeft("curto", 10); got != "curto" {
		t.Fatalf("got = %q", got)
	}
	if got := truncateLeft("0123456789", 6); got != "..6789" {
		t.Fatalf("got = %q", got)
	}
	if got := truncateLeft("0123456789", 3); got != "789" {
		t.Fatalf("got = %q", got)
	}
}

func TestCopyProgressDisabledWithoutTerminal(t *testing.T) {
	buf := &bytes.Buffer{}
	progress := newCopyProgress(buf, false)
	if progress.Enabled() {
		t.Fatal("bytes.Buffer nao e terminal")
	}
	progress.Update(transfer.Progress{File: "x", Done: 1, Total: 2})
	progress.Clear()
	progress.Finish()
	if buf.Len() != 0 {
		t.Fatalf("nao deveria escrever nada: %q", buf.String())
	}
}

func TestCopyProgressDrawsOnWriter(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	progress := &copyProgress{out: file, width: 80}
	progress.Update(transfer.Progress{File: "srv-a:/tmp/x.log", Done: 50, Total: 100})
	progress.Update(transfer.Progress{File: "srv-a:/tmp/x.log", Done: 100, Total: 100})
	progress.Finish()

	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "\r") < 3 {
		t.Fatalf("esperava 2 redesenhos e a limpeza: %q", text)
	}
	if !strings.Contains(text, " 50%") || !strings.Contains(text, "100%") {
		t.Fatalf("barra sem as porcentagens: %q", text)
	}
	if !strings.Contains(text, "x.log") {
		t.Fatalf("barra sem o nome do arquivo: %q", text)
	}
}

func TestCopyProgressQuietDisables(t *testing.T) {
	progress := newCopyProgress(os.Stderr, true)
	if progress.Enabled() {
		t.Fatal("-q deveria desligar a barra")
	}
}
