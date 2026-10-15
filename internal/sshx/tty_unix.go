//go:build !windows

package sshx

import (
	"os"
	"os/signal"
	"syscall"
)

// resizeTrigger devolve um canal que dispara quando o terminal pode ter mudado
// de tamanho (SIGWINCH no Unix).
func resizeTrigger(done <-chan struct{}) <-chan struct{} {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	events := make(chan struct{}, 1)
	go func() {
		defer signal.Stop(signals)
		for {
			select {
			case <-done:
				return
			case <-signals:
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()
	return events
}
