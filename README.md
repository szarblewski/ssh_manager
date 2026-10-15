# sshm — gerenciador de conexões SSH

CLI de linha de comando para cadastrar, organizar e conectar em servidores SSH.
Um **binário único em Go**, sem dependências externas: nada de `jq`, `fzf`,
`sshpass` ou `secret-tool`. Roda em **Windows, Linux e macOS**.

```console
$ sshm                      # seleção interativa (menu em tela cheia)
$ sshm list                 # lista por grupo
$ sshm vps-cliente-a        # conecta direto
$ sshm vps-cliente-a -- df -h   # roda um comando remoto
```

> O `sshm.sh` (bash + jq) continua no repositório como **legado**. Veja
> [docs/migracao.md](docs/migracao.md) para trocar de um para o outro.

## Instalação

### Windows

```powershell
# a partir de uma release publicada
irm https://raw.githubusercontent.com/szarblewski/ssh_manager/main/install.ps1 | iex

# ou, já com o repositório clonado: detecta o checkout e compila com o Go
.\install.ps1
```

> **Ainda não há release publicada?** Rodando o script de dentro do repositório
> (com o Go instalado), ele detecta o checkout e **compila localmente** — não é
> preciso esperar por uma release. Para publicar a primeira:
> `git tag v2.0.0` e `git push origin v2.0.0` (o workflow
> `.github/workflows/release.yml` gera os pacotes e o `checksums.txt`).

O instalador copia o `sshm.exe` para `%LOCALAPPDATA%\Programs\sshm`, coloca a
pasta no **PATH do usuário** (sem `sudo`, sem `setx`), garante o
`%APPDATA%\sshm\hosts.json` e testa o comando. Opções úteis:

| Opção | Efeito |
| --- | --- |
| `-BinDir DIR` | instala em outro diretório (padrão: `%LOCALAPPDATA%\Programs\sshm`) |
| `-Prefix DIR` | instala em `DIR\bin` |
| `-Version 2.0.0` | escolhe a versão (padrão: a última release) |
| `-Source CAMINHO` | pasta do repositório (faz o build) ou um `sshm.exe` local (automático quando o script está dentro do repositório) |
| `-Config ARQUIVO` | usa um `hosts.json` específico (grava `SSHM_CONFIG` no ambiente) |
| `-NoPath` / `-NoConfig` | não altera o PATH / não mexe na configuração |
| `-Uninstall [-PurgeConfig]` | desfaz a instalação (e apaga a configuração, se pedido) |

### Linux e macOS

```bash
go install github.com/szarblewski/ssh_manager/cmd/sshm@latest
```

Ou baixe o `.zip` da release (a página de releases traz `sshm_<versão>_<so>_<arch>.zip`).
O binário é estático (`CGO_ENABLED=0`), então basta colocá-lo no PATH.

Para desinstalar: `sshm uninstall [--purge-config]`.

## Primeiros passos

```console
$ sshm create                       # pergunta nome, host, usuário, porta, chave, grupo, tags
$ sshm create -n lab -H 10.0.0.5 -u root -p 2222 -g cliente-a -t prod,sap
$ sshm passwd lab                   # guarda a senha no cofre do sistema
$ sshm list
[cliente-a]
ID NOME USER@HOST   PORTA TAGS
1  lab  root@10.0.0.5 2222 prod,sap
```

## Comandos

| Comando | Apelidos | O que faz |
| --- | --- | --- |
| `sshm` | | seleção interativa e conexão |
| `sshm select` | `-s`, `menu`, `pick` | idem, aceita `-g`, `-t`, `--flat` |
| `sshm list` | `ls`, `-l` | lista as conexões (`-g`, `-t`, `--flat`) |
| `sshm <id\|nome>` | | conecta direto, repassando as opções do ssh |
| `sshm create` | `new`, `-c`, `--create` | cadastra uma conexão |
| `sshm edit <id\|nome>` | `editar`, `update` | edita a conexão (sem opções, pergunta campo a campo) |
| `sshm delete <id\|nome>` | `del`, `rm`, `remove`, `apagar` | apaga a conexão (`-y` não pede confirmação) |
| `sshm passwd [<id\|nome>]` | `password`, `senha`, `pass` | guarda/troca/apaga a senha (`--remove`, `--plain`) |
| `sshm config` | | `path`, `show`, `init`, `validate`, `normalize`, `migrate-secrets` |
| `sshm version` | `-V`, `--version` | mostra a versão |
| `sshm uninstall` | | remove o binário (`--purge-config` apaga a configuração) |

### Grupos, tags e filtros

A listagem e a seleção separam os hosts por grupo (ordem alfabética, sem grupo no
fim) e, dentro do grupo, por nome. `--flat` volta à tabela única com a coluna
`GRUPO`. Os filtros `-g <grupo>` e `-t <tag>` valem para `list` e `select`.

### Cores

A listagem destaca cada parte da linha: **grupo** em ciano, **nome** em negrito,
`usuário@host` em ciano, **porta** em amarelo e **tags** em verde; host sem grupo
sai apagado. O menu em tela cheia e o menu numerado usam as mesmas cores.

As cores se ajustam sozinhas:

| Situação | Resultado |
| --- | --- |
| Saída em um terminal | colorida |
| Saída em pipe/arquivo (`sshm list > hosts.txt`) | sem cor (texto limpo para scripts) |
| `--color=always` | colorida mesmo em pipe (útil com `less -R`) |
| `--color=never` ou `--no-color` | sempre sem cor |
| `NO_COLOR=1` (convenção) ou `TERM=dumb` | sem cor |

O menu em tela cheia desenha direto no console (e não no stdout), então ele
aparece mesmo com a saída redirecionada — e troca a página de código do console
para UTF-8 enquanto está aberto, voltando ao normal ao sair.

### Seleção interativa

O seletor é escolhido por `SSHM_PICKER` ou `--picker`:

* `tui` (padrão em terminal): menu em tela cheia, com filtro incremental (`/`);
  setas/`j`/`k` movem, `g`/`G` vão ao topo/fim, `PageUp`/`PageDown` rolam,
  número + `Enter` vai direto ao host, `Enter` conecta, `q`/`Esc` cancela;
* `number`: menu numerado (usado automaticamente quando a entrada não é um
  terminal, ex.: em scripts);
* `fzf`: usa o `fzf` instalado, se houver;
* `auto` (padrão): `fzf` se existir, senão `tui` em terminal e `number` em pipe.

### Opções aceitas depois do host

Como o cliente SSH é nativo, um subconjunto das opções do `ssh` é interpretado
pelo próprio sshm:

| Opção | Situação |
| --- | --- |
| `-L [bind:]porta:host:porta` | suportado (encaminhamento local) |
| `-J [usuário@]host[:porta]` | suportado (bastião/`ProxyJump`, inclusive cadeias) |
| `-o Chave=Valor` | `StrictHostKeyChecking`, `UserKnownHostsFile`, `IdentityFile`, `Port`, `User`, `ProxyJump`, `ConnectTimeout`, `ServerAliveInterval`, `ServerAliveCountMax`, `BatchMode`, `LogLevel` |
| `-i`, `-p`, `-l`, `-N`, `-T`, `-t`, `-v` | suportados |
| `-- comando` | executa o comando remoto (sem terminal) e propaga o código de saída |
| `-R`, `-D`, `-X`, `-Y` | **não suportados** (erro explicativo) |
| `-C`, `-A` | aceitos com aviso: compressão e agent forwarding não são suportados |

O `known_hosts` é respeitado: host novo é aceito e registrado
(*accept-new*, como o `sshm.sh`), chave **alterada é sempre recusada** — nunca
trocamos a chave sozinhos. Com `SSHM_STRICT_HOSTKEY=1` a primeira conexão pede
confirmação manual.

## Senhas

| Onde | Quando |
| --- | --- |
| Cofre do sistema | padrão — Credential Manager (Windows), Keychain (macOS), Secret Service (Linux) |
| Texto puro no `hosts.json` | `sshm passwd <host> --plain`, ou quando o cofre está desligado (`SSHM_KEYRING=off`) |

`sshm config migrate-secrets` move as senhas em texto puro para o cofre.
Como o cofre é **por sistema operacional**, uma senha gravada no Linux não é
lida no Windows (basta rodar `sshm passwd <host>` de novo na outra máquina).

Chaves privadas continuam sendo o caminho recomendado: `sshm create -i ~/.ssh/id_ed25519`
ou use o `ssh-agent` (no Windows, o serviço *OpenSSH Authentication Agent*).

## Arquivo de configuração

`hosts.json`, procurado nesta ordem (o primeiro que existir vence):

1. `--config <arquivo>` ou `$SSHM_CONFIG` — **quando informado, é o único usado**;
2. `%APPDATA%\sshm\hosts.json` (Linux/macOS: `~/.config/sshm/hosts.json`);
3. `~/.config/sshm/hosts.json` (compatibilidade com o `sshm.sh`);
4. `hosts.json` na pasta atual;
5. `hosts.json` na pasta do executável.

```json
{
  "hosts": [
    {
      "id": 1,
      "name": "vps-cliente-a",
      "host": "203.0.113.10",
      "user": "root",
      "port": 2222,
      "identity_file": "~/.ssh/id_rsa_cliente_a",
      "group": "cliente-a",
      "tags": ["producao", "sap-b1"],
      "proxy_jump": "bastiao@10.0.0.1:22",
      "password_secret": "vps-cliente-a"
    }
  ]
}
```

* campos opcionais: `port` (padrão 22), `identity_file`, `group`, `tags`,
  `proxy_jump`, `connect_timeout`, `password`, `password_secret`;
* campos desconhecidos são **preservados** na gravação;
* codificação: a leitura aceita UTF-8 (com ou sem BOM) e UTF-16; a gravação é
  sempre **UTF-8 sem BOM** (Bloco de Notas e PowerShell gravam BOM com
  facilidade, e o BOM invalida o JSON). Use `sshm config normalize` para
  converter um arquivo que veio de outra ferramenta;
* caminhos de chave que não existem no sistema atual são procurados pelo mesmo
  nome em `~/.ssh` — o mesmo `hosts.json` serve para Linux e Windows;
* a gravação é atômica (arquivo temporário + troca) e o arquivo fica com
  permissão restrita quando há senha em texto puro (no Linux/macOS).

### Variáveis de ambiente

| Variável | Efeito |
| --- | --- |
| `SSHM_CONFIG` | caminho do `hosts.json` (exclusivo) |
| `SSHM_PICKER` | `tui`, `number` ou `fzf` |
| `SSHM_STRICT_HOSTKEY=1` | confirmação manual da chave de host na 1ª conexão |
| `SSHM_KEYRING=off` | desliga o cofre do sistema (senhas ficam em texto puro) |
| `SSHM_NO_AGENT=1` | ignora o `ssh-agent` |
| `NO_COLOR` | desliga as cores (qualquer valor não vazio) |

Opções equivalentes na linha de comando: `--config <arquivo>`, `--picker <modo>`,
`--color auto|always|never` e `--no-color`.

## Códigos de saída

| Código | Significado |
| --- | --- |
| `0` | sucesso (ou seleção cancelada) |
| `1` | erro de uso/configuração |
| `255` | falha ao conectar/autenticar (igual ao `ssh`) |
| saída do comando remoto | repassada quando `sshm <host> -- comando` é usado |

## Diferenças em relação ao `sshm.sh`

| Assunto | `sshm.sh` (legado) | `sshm` (este) |
| --- | --- | --- |
| Dependências | `jq`; `fzf`/`secret-tool`/`sshpass` opcionais | nenhuma |
| Senha | `secret-tool` (libsecret) ou texto puro | Credential Manager / Keychain / Secret Service ou texto puro |
| Cliente SSH | `ssh` do sistema | biblioteca nativa (`x/crypto/ssh`) |
| Opções do `ssh` | todas (repassadas para o `ssh`) | subconjunto documentado acima |
| `-R`, `-D`, `-C`, `-A` | funcionam (via `ssh`) | não suportados (aviso/erro) |
| Formato do JSON | chaves na ordem original, arrays em uma linha | chaves ordenadas, arrays multilinha (mesmo JSON) |
| Interface | menu em tela cheia | mesma base, com cores, filtro incremental (`/`) e destaque de grupos |

## Desenvolvimento

```bash
go build ./...          # compila tudo
go vet ./...            # análise estática
go test ./...           # testes (inclui um servidor SSH em processo)
go build -o sshm ./cmd/sshm
```

Layout:

```
cmd/sshm/          ponto de entrada
internal/config/   hosts.json: leitura/gravação, filtros, caminhos
internal/secrets/  cofre do sistema e fallback em texto puro
internal/sshx/     cliente SSH nativo (autenticação, known_hosts, PTY, -L, -J)
internal/picker/   seleção: tela cheia, numerada e fzf
internal/table/    formatação das tabelas
internal/cli/      comandos, prompts e despacho
install.ps1        instalador do Windows
```

CI roda `go vet` e `go test` em Windows, Linux e macOS, além de cross-build para
`windows/linux/darwin` × `amd64/arm64`. As releases são publicadas pelo
GoReleaser em tags `v*`.

## Ainda não suportado

* `-R` (encaminhamento remoto), `-D` (proxy SOCKS), `-X`/`-Y` (X11);
* compressão (`-C`) e agent forwarding (`-A`);
* leitura de `~/.ssh/config` (`Host`, `Include`, `ProxyCommand`).
