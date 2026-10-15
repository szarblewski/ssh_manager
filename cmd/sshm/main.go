// Command sshm gerencia conexoes SSH.
//
// Uso rapido:
//
//	sshm                       selecao interativa
//	sshm list                  lista as conexoes
//	sshm <id|nome> [opcoes]    conecta direto
//	sshm create                cadastra uma conexao
package main

import (
	"os"

	"github.com/szarblewski/ssh_manager/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
