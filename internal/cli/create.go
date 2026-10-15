package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/szarblewski/ssh_manager/internal/config"
	"github.com/szarblewski/ssh_manager/internal/sshx"
)

// cmdCreate cadastra uma nova conexao.
//
// Sem opcoes, pergunta campo a campo (como o sshm.sh); com -n/-H/-u/-p/-i/-g/-t
// aceita os dados direto na linha de comando.
func (a *App) cmdCreate(args []string) error {
	var (
		name        string
		hostAddress string
		userName    string
		identity    string
		group       string
		tags        string
		port        int
		askPassword bool
		plain       bool
	)

	set := flag.NewFlagSet("create", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&name, "n", "", "nome")
	set.StringVar(&name, "name", "", "nome")
	set.StringVar(&hostAddress, "H", "", "host")
	set.StringVar(&hostAddress, "host", "", "host")
	set.StringVar(&userName, "u", "", "usuario")
	set.StringVar(&userName, "user", "", "usuario")
	set.IntVar(&port, "p", 0, "porta")
	set.IntVar(&port, "port", 0, "porta")
	set.StringVar(&identity, "i", "", "chave")
	set.StringVar(&identity, "identity", "", "chave")
	set.StringVar(&identity, "identity-file", "", "chave")
	set.StringVar(&group, "g", "", "grupo")
	set.StringVar(&group, "group", "", "grupo")
	set.StringVar(&tags, "t", "", "tags")
	set.StringVar(&tags, "tags", "", "tags")
	set.BoolVar(&askPassword, "P", false, "pergunta a senha")
	set.BoolVar(&askPassword, "password", false, "pergunta a senha")
	set.BoolVar(&plain, "plain", false, "grava a senha em texto puro")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() > 0 {
		return fmt.Errorf("opcao desconhecida para create: %s", set.Arg(0))
	}

	store, cfg, err := a.openConfig()
	if err != nil {
		return err
	}

	// nome: obrigatorio, sem espacos e sem repetir um ja cadastrado
	name = config.SanitizeName(name)
	for {
		if name == "" {
			value, err := a.Prompter.Text("Nome/apelido (sem espacos)", "")
			if err != nil {
				return canceled(err)
			}
			name = config.SanitizeName(value)
			if name == "" {
				a.info("o nome e obrigatorio.")
				continue
			}
		}
		if cfg.NameExists(name) {
			if a.isInteractive() {
				a.info("ja existe uma conexao chamada '%s'; escolha outro nome.", name)
				name = ""
				continue
			}
			return fmt.Errorf("ja existe uma conexao chamada '%s'.", name)
		}
		break
	}

	for hostAddress == "" {
		value, err := a.Prompter.Text("Host/IP", "")
		if err != nil {
			return canceled(err)
		}
		hostAddress = value
		if hostAddress == "" {
			a.info("o host e obrigatorio.")
		}
	}

	if userName == "" {
		if a.isInteractive() {
			value, err := a.Prompter.Text("Usuario", sshx.CurrentUser())
			if err != nil {
				return canceled(err)
			}
			userName = value
		} else {
			userName = sshx.CurrentUser()
		}
	}

	if port == 0 {
		if a.isInteractive() {
			value, err := a.Prompter.Text("Porta", "22")
			if err != nil {
				return canceled(err)
			}
			if _, err := fmt.Sscanf(value, "%d", &port); err != nil && value != "" {
				return fmt.Errorf("porta invalida: %q", value)
			}
		}
		if port == 0 {
			port = config.DefaultPort
		}
	}
	if err := config.ValidatePort(port); err != nil {
		return err
	}

	if identity == "" && a.isInteractive() {
		value, err := a.Prompter.Text("Arquivo de chave (Enter para nenhum)", "")
		if err != nil {
			return canceled(err)
		}
		identity = value
	}
	if group == "" && a.isInteractive() {
		value, err := a.Prompter.Text("Grupo (Enter para nenhum)", "")
		if err != nil {
			return canceled(err)
		}
		group = value
	}
	if tags == "" && a.isInteractive() {
		value, err := a.Prompter.Text("Tags separadas por virgula (Enter para nenhuma)", "")
		if err != nil {
			return canceled(err)
		}
		tags = value
	}

	if identity != "" && !config.IsFile(config.ExpandPath(identity)) {
		a.info("aviso: o arquivo de chave '%s' nao existe (a conexao sera criada mesmo assim).", identity)
	}

	newHost := config.Host{
		ID:           cfg.NextID(),
		Name:         name,
		Host:         hostAddress,
		User:         userName,
		Port:         port,
		IdentityFile: identity,
		Group:        group,
		Tags:         config.NormalizeTags(tags),
	}
	cfg.Hosts = append(cfg.Hosts, newHost)
	if err := store.Save(cfg); err != nil {
		return err
	}
	index := len(cfg.Hosts) - 1

	a.info("conexao '%s' criada (id %d): %s", newHost.Name, newHost.ID, shortDescription(newHost))

	// senha: com -P pergunta sempre; sem -P, oferece no modo interativo
	if !askPassword && a.isInteractive() {
		confirmed, err := a.Prompter.Confirm("Guardar a senha para entrar direto (sem digitar toda vez)? [s/N]: ")
		if err != nil {
			return canceled(err)
		}
		askPassword = confirmed
	}
	if askPassword {
		if err := a.updatePassword(store, cfg, index, plain); err != nil {
			return canceled(err)
		}
	}

	if a.isInteractive() {
		confirmed, err := a.Prompter.Confirm("Conectar agora? [s/N]: ")
		switch {
		case err != nil && errors.Is(err, ErrCancelled):
			a.info("use 'sshm %s' ou 'sshm menu' quando quiser conectar.", newHost.Name)
			return nil
		case err != nil:
			return err
		case confirmed:
			return a.connectTo(cfg.Hosts[index], nil)
		default:
			a.info("use 'sshm %s' ou 'sshm menu' quando quiser conectar.", newHost.Name)
		}
	}
	return nil
}

// canceled converte a interrupcao da entrada (Ctrl+D) em saida silenciosa,
// como fazia o 'exit 0' do sshm.sh.
func canceled(err error) error {
	if errors.Is(err, ErrCancelled) {
		return &ExitError{Code: 0}
	}
	return err
}
