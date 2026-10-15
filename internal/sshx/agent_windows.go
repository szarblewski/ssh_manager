//go:build windows

package sshx

import (
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
)

// defaultAgentPipe e onde o servico "OpenSSH Authentication Agent" atende.
//
// O x/crypto/ssh nao sabe falar com o agente do Windows (golang/go#61383): o
// agente e exposto em um named pipe, e nao em um socket Unix, por isso o dial
// e feito aqui com o go-winio.
const defaultAgentPipe = `\\.\pipe\openssh-ssh-agent`

// agentAddress devolve o endereco do agente em uso.
func agentAddress() string {
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		return socket
	}
	return defaultAgentPipe
}

// dialAgent conecta no ssh-agent do Windows (named pipe) ou no SSH_AUTH_SOCK
// quando o sshm roda dentro do Git Bash/MSYS2.
func dialAgent() (net.Conn, error) {
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		return net.Dial("unix", socket)
	}
	timeout := 2 * time.Second
	return winio.DialPipe(defaultAgentPipe, &timeout)
}
