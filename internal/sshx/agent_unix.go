//go:build !windows

package sshx

import (
	"fmt"
	"net"
	"os"
)

// agentAddress devolve o socket do ssh-agent.
func agentAddress() string { return os.Getenv("SSH_AUTH_SOCK") }

// dialAgent conecta no ssh-agent local via socket Unix.
func dialAgent() (net.Conn, error) {
	socket := agentAddress()
	if socket == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK nao definida (rode 'eval $(ssh-agent)' ou use uma chave)")
	}
	return net.Dial("unix", socket)
}
