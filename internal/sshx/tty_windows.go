//go:build windows

package sshx

import "time"

// resizeInterval e o intervalo de checagem do tamanho do console.
//
// O Windows nao tem SIGWINCH: o console avisa o processo por eventos de
// console, mais chatos de consumir do que simplesmente conferir o tamanho de
// tempo em tempo.
const resizeInterval = 250 * time.Millisecond

// resizeTrigger devolve um canal que dispara periodicamente para conferir o
// tamanho do console.
func resizeTrigger(done <-chan struct{}) <-chan struct{} {
	events := make(chan struct{}, 1)
	ticker := time.NewTicker(resizeInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()
	return events
}
