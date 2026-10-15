package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/szarblewski/ssh_manager/internal/ansi"
	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/secrets"
	"github.com/szarblewski/ssh_manager/internal/sshx"
	"github.com/szarblewski/ssh_manager/internal/table"
)

// EnvStrictHostKey liga a confirmacao manual da chave de host (igual ao sshm.sh).
const EnvStrictHostKey = "SSHM_STRICT_HOSTKEY"

// context devolve o contexto usado na conexao.
//
// Nao instalamos tratador de SIGINT de proposito: com o Ctrl+C padrao o
// processo morre e a conexao cai, que e o comportamento esperado - dentro da
// sessao o Ctrl+C vai como tecla para o servidor (o terminal fica em modo bruto).
func (a *App) context() context.Context { return context.Background() }

// strictHostKeyEnv diz se o usuario quer confirmar a chave de host na mao.
func strictHostKeyEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStrictHostKey))) {
	case "1", "yes", "true", "on", "sim":
		return true
	default:
		return false
	}
}

// info escreve uma mensagem informativa (stderr, como no sshm.sh).
func (a *App) info(format string, args ...any) {
	fmt.Fprintf(a.Err, "sshm: "+format+"\n", args...)
}

// output escreve dados (stdout).
func (a *App) output(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

// findHost resolve id/nome de host e devolve o indice na configuracao.
func (a *App) findHost(cfg *config.Config, key string) (int, error) {
	index, err := cfg.Find(key)
	if err != nil {
		return 0, fmt.Errorf("%w (use 'sshm list' para ver os disponiveis)", err)
	}
	return index, nil
}

// shortDescription resume o host para mensagens.
func shortDescription(host config.Host) string {
	line := fmt.Sprintf("%s@%s:%d", host.User, host.Host, host.PortOrDefault())
	if host.Group != "" {
		line += "  [grupo: " + host.Group + "]"
	}
	if len(host.Tags) > 0 {
		line += "  [tags: " + strings.Join(host.Tags, ",") + "]"
	}
	return line
}

// passwordStatus descreve onde a senha do host esta guardada.
func (a *App) passwordStatus(host config.Host) string {
	switch {
	case host.PasswordSecret != "":
		return "cofre (" + secrets.Description(a.Vault) + ")"
	case host.Password != "":
		return "texto puro no arquivo"
	default:
		return "sem senha"
	}
}

// isInteractive diz se ha terminal para perguntar dados.
func (a *App) isInteractive() bool { return sshx.IsTerminal(a.In) }

// style devolve o estilo usado nas saidas coloridas.
func (a *App) style() table.Style {
	out, _ := a.Out.(*os.File)
	return table.NewStyle(ansi.ColorEnabled(a.ColorMode, a.NoColor, out))
}

// idString converte um id numerico para a chave usada nas buscas.
func idString(id int) string { return strconv.Itoa(id) }

// extractKey separa o argumento posicional (o host) das opcoes.
//
// O pacote flag para de interpretar opcoes no primeiro argumento solto, mas o
// sshm.sh aceitava 'sshm edit <id|nome> -n novo' (host primeiro). Este ajudante
// reorganiza a linha para manter essa ordem.
//
// valueFlags lista as opcoes que consomem o argumento seguinte.
func extractKey(args []string, valueFlags map[string]bool) (key string, flags []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			for _, rest := range args[i+1:] {
				if rest == "" {
					continue
				}
				if key != "" {
					return "", nil, errors.New("informe apenas um host por vez.")
				}
				key = rest
			}
			break
		}

		if strings.HasPrefix(arg, "-") && arg != "-" {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if !strings.Contains(arg, "=") && valueFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}

		if key != "" {
			return "", nil, errors.New("informe apenas um host por vez.")
		}
		key = arg
	}
	return key, flags, nil
}
