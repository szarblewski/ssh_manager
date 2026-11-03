# Migrando do `sshm.sh` para o `sshm` (Go)

O `sshm` novo fala a **mesma linguagem** do script bash: mesmos comandos,
apelidos, filtros, formato de tabela e o mesmo `hosts.json`. A migração é trocar
o comando, não recadastrar tudo.

## Resumo

| Item | Situação |
| --- | --- |
| `hosts.json` | **compatível** — os mesmos campos, lidos e gravados |
| Comandos e apelidos | iguais (`create/new`, `edit/update`, `delete/rm`, `passwd/senha`, `list/ls/-l`, `select/menu`) |
| Filtros `-g`, `-t`, `--flat` | iguais |
| `SSHM_CONFIG`, `SSHM_PICKER`, `SSHM_STRICT_HOSTKEY` | iguais |
| Senhas no cofre | **não migram automaticamente** entre sistemas operacionais (veja abaixo) |
| Dependências | `jq`/`fzf`/`secret-tool`/`sshpass` deixam de ser necessárias |

## Passo a passo (Windows)

1. Instale o novo comando:

   ```powershell
   irm https://raw.githubusercontent.com/szarblewski/ssh_manager/main/install.ps1 | iex
   ```

2. Aponte para o seu `hosts.json` atual. O instalador evita criar uma
   configuração nova se já existir uma; se o seu arquivo estiver em outro lugar:

   ```powershell
   $env:SSHM_CONFIG = "C:\caminho\hosts.json"   # só nesta sessão
   sshm list
   ```

   Para fixar de vez, use `-Config` no instalador ou defina `SSHM_CONFIG` nas
   variáveis de ambiente do usuário.

3. Confira:

   ```powershell
   sshm config validate       # JSON, nomes repetidos, portas
   sshm list                  # mesma tabela de antes
   sshm <nome-do-host>        # conecta
   ```

4. Guarde as senhas no cofre do sistema (o cofre é por máquina):

   ```powershell
   sshm passwd <nome-do-host>
   ```

   Se você tinha senhas em texto puro no arquivo, pode movê-las de uma vez:

   ```powershell
   sshm config migrate-secrets
   ```

5. Quando estiver confortável, remova a instalação antiga
   (`install.sh --uninstall` ou apagando o `sshm` de `~/.local/bin`).

## Passo a passo (Linux/macOS)

```bash
go install github.com/szarblewski/ssh_manager/cmd/sshm@latest
# ou baixe o .zip da release
sshm list
```

O arquivo `~/.config/sshm/hosts.json` continua sendo encontrado (segundo
candidato da busca), então normalmente nada precisa ser configurado.

As senhas guardadas pelo `secret-tool` **não** são lidas pelo sshm (o sshm usa a
API do Secret Service diretamente, com outro esquema de nomes); rode
`sshm passwd <host>` uma vez por host.

## O que muda no dia a dia

### Caminhos de chave privada

O mesmo `hosts.json` pode ser usado no Linux e no Windows. Quando o caminho
gravado não existe na máquina atual (ex.: `/home/fulano/.ssh/id_rsa` no
Windows), o sshm procura o mesmo nome de arquivo em `~/.ssh` e usa o que
encontrar. Exemplo: `"identity_file": "/home/fulano/.ssh/id_rsa_cliente_a"`
resolve para `C:\Users\fulano\.ssh\id_rsa_cliente_a`.

### Opções extras de conexão

No script bash, tudo o que vinha depois do host ia direto para o `ssh`. Agora o
cliente é nativo, então um subconjunto é interpretado pelo sshm:

| Antes (`ssh`) | Agora |
| --- | --- |
| `sshm host -L 8080:localhost:80` | igual |
| `sshm host -J bastiao` | igual |
| `sshm host -R 9000:localhost:22` | **não suportado**: use `ssh` diretamente |
| `sshm host -C` | aceito, mas sem compressão (aviso) |
| `sshm host -o Port=2222` | igual, para as opções da tabela do README |

### Cópia de arquivos

O `sshm.sh` não copiava arquivos: era preciso usar `scp` à parte (e repetir
host, usuário, porta e chave na linha de comando). Agora o próprio `sshm cp`
fala SFTP usando o cadastro:

| Antes | Agora |
| --- | --- |
| `scp -P 2222 arquivo root@203.0.113.10:/tmp/` | `sshm cp arquivo vps-cliente-a:/tmp/` |
| `scp -P 2222 root@203.0.113.10:/etc/app.conf .` | `sshm cp vps-cliente-a:/etc/app.conf .` |
| `scp -r root@203.0.113.10:/var/log ./logs` | `sshm cp -r vps-cliente-a:/var/log ./logs` |
| `scp a user@hostB:/tmp/` (do host A) | `sshm cp vps-a:/caminho/a vps-b:/tmp/` (da sua máquina) |

O `cp` pode ser omitido quando os dois lados já se explicam, como no `scp` —
`sshm vps-a:/etc/app.conf vps-b:~/` é o mesmo comando (e `host:~/x` é o mesmo
que `host:/home/usuario/x`).

### Chave de host

Continua valendo o comportamento do script: host novo é aceito e registrado no
`known_hosts`; se a chave **mudou**, a conexão é recusada com uma mensagem
explicando qual linha do arquivo remover. `SSHM_STRICT_HOSTKEY=1` pede
confirmação na primeira conexão.

### Formato do arquivo

O conteúdo é o mesmo JSON, mas a gravação passou a ordenar as chaves e a quebrar
arrays em várias linhas:

```diff
-      "tags": ["producao", "sap-b1"]
+      "tags": [
+        "producao",
+        "sap-b1"
+      ]
```

Isso não muda nada para os dois comandos. Campos que o sshm não conhece são
preservados como estão.

## Revertendo

O `sshm.sh` continua no repositório. Enquanto ele estiver instalado, os dois
comandos podem conviver (o binário Go se chama `sshm`; se você instalou o bash
como `sshm` no PATH, remova-o primeiro para não haver ambiguidade).

* `sshm uninstall` remove o binário (mantendo a configuração);
* `sshm uninstall --purge-config` remove também `%APPDATA%\sshm` e `~/.config/sshm`
  (**apaga as conexões** — faça backup do `hosts.json` antes).
