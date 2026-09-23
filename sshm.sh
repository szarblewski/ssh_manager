#!/usr/bin/env bash
#
# sshm - gerenciador de conexoes SSH
#
# Uso:
#   sshm                              (abre selecao interativa)
#   sshm select [-g <grupo>] [-t <tag>] [--flat]
#                                     (idem, com filtro; atalhos: -s, menu)
#   sshm menu [--flat]                (forca a lista navegavel em tela cheia)
#   sshm create [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
#               [-g grupo] [-t tags] [-P]   (cadastra uma conexao)
#                                     (atalhos: new, --create, -c)
#   sshm passwd [<id|nome>] [--remove] [--plain]
#                                     (guarda/troca/apaga a senha da conexao)
#                                     (atalhos: password, senha, pass)
#   sshm edit <id|nome> [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
#                       [-g grupo] [-t tags]
#                                     (edita a conexao; sem opcoes, pergunta
#                                      campo a campo)  (atalhos: editar, update)
#   sshm delete <id|nome> [-y]        (apaga a conexao; -y nao pede confirmacao)
#                                     (atalhos: del, rm, remove, apagar)
#   sshm <numero|nome> [args extras repassados para o ssh]
#   sshm list [-g <grupo>] [-t <tag>] [--flat]
#   sshm ls | -l           (atalho para list sem filtro)
#   sshm -h | --help
#
# Listagem e selecao mostram os hosts separados por grupo (em ordem alfabetica,
# com os sem grupo no fim) e, dentro do grupo, em ordem de nome.
# --flat desliga o agrupamento e volta a tabela unica com a coluna GRUPO.
#
# Requer: jq (sudo apt install jq)
#
# Seletor da selecao interativa:
#   - fzf, quando instalado (sudo apt install fzf): busca fuzzy;
#   - menu navegavel em tela cheia (bash puro, sem dependencias):
#       setas ou j/k    mover a selecao
#       g / G           primeiro / ultimo
#       PageUp/PageDown rolar a lista
#       numero + Enter  ir direto para a linha
#       Enter           conectar no host selecionado
#       q ou Esc        cancelar
#   - menu numerado, quando a entrada nao e um terminal (scripts/pipes).
#
# SSHM_PICKER=fzf|menu|number forca um dos seletores (padrao: auto).
#
# Primeira conexao num host novo: a chave e aceita automaticamente (equivale a
# responder "yes") e fica no known_hosts; se a chave MUDAR, a conexao e
# recusada. Para exigir a confirmacao manual, use SSHM_STRICT_HOSTKEY=1.
#
# Arquivo de configuracao (JSON), procurado nesta ordem:
#   1) $SSHM_CONFIG, se definida;
#   2) hosts.json na mesma pasta deste script;
#   3) hosts.json na pasta atual;
#   4) $HOME/.config/sshm/hosts.json.
# Se nenhum existir, um arquivo vazio e criado na pasta do script (se gravavel)
# ou em $HOME/.config/sshm/hosts.json. Cadastre conexoes com 'sshm create'.
#
# Formato do arquivo:
# {
#   "hosts": [
#     {
#       "id": 1,
#       "name": "meuservidor",
#       "host": "192.168.1.10",
#       "user": "root",
#       "port": 2222,
#       "identity_file": "/home/usuario/.ssh/id_rsa_meuservidor",
#       "group": "cliente-a",
#       "tags": ["producao", "sap-b1"]
#     }
#   ]
# }
#
# "port" (padrao 22), "identity_file" (-i), "group" e "tags" sao opcionais.
#
# Senha guardada (opcional, para entrar direto sem digitar):
#   "password": "..."             texto puro no proprio arquivo
#   "password_secret": "<nome>"   no cofre do sistema (secret-tool/libsecret)
# Use 'sshm create -P' ou 'sshm passwd <host>' para gravar. Na conexao, a senha e
# entregue por sshpass (se instalado) ou por SSH_ASKPASS (OpenSSH >= 8.4).
# Aviso: senha em texto puro fica legivel por quem ler o hosts.json.

set -euo pipefail

SSHM_CONFIG="${SSHM_CONFIG:-}"

err() {
    echo "sshm: $*" >&2
    exit 1
}

# diretorio onde este script esta instalado
script_dir() {
    local src="${BASH_SOURCE[0]}" dir
    dir="$(dirname -- "$src")"
    if [ -e "$src" ] && command -v readlink >/dev/null 2>&1; then
        src="$(readlink -f -- "$src" 2>/dev/null)" || src="${BASH_SOURCE[0]}"
        dir="$(dirname -- "$src")"
    fi
    (cd -- "$dir" 2>/dev/null && pwd) || printf '%s\n' "$dir"
}

# caminhos onde o hosts.json e procurado, na ordem (com SSHM_CONFIG so ele conta)
config_candidates() {
    local -a candidates=()
    local c seen=""
    if [ -n "$SSHM_CONFIG" ]; then
        printf '%s\n' "$SSHM_CONFIG"
        return 0
    fi
    candidates+=("$(script_dir)/hosts.json" "$PWD/hosts.json" "$HOME/.config/sshm/hosts.json")
    for c in "${candidates[@]}"; do
        case " $seen " in
            *" $c "*) continue ;;
        esac
        seen="$seen $c"
        printf '%s\n' "$c"
    done
}

# onde criar o arquivo quando nenhum existe: $SSHM_CONFIG, pasta do script
# (se gravavel) ou $HOME/.config/sshm/hosts.json
config_new_path() {
    local dir
    if [ -n "$SSHM_CONFIG" ]; then
        printf '%s\n' "$SSHM_CONFIG"
        return 0
    fi
    dir="$(script_dir)"
    if [ -w "$dir" ]; then
        printf '%s\n' "$dir/hosts.json"
        return 0
    fi
    printf '%s\n' "$HOME/.config/sshm/hosts.json"
}

# imprime o primeiro arquivo de configuracao existente (rc=1 se nao achar nenhum)
locate_config() {
    local candidate
    while IFS= read -r candidate; do
        if [ -f "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done < <(config_candidates)
    return 1
}

command -v jq >/dev/null 2>&1 || err "jq nao esta instalado. Instale com: sudo apt install jq"

# cria o arquivo de configuracao (vazio) em $1
create_config_file() {
    local file="$1" dir
    dir="$(dirname -- "$file")"
    if [ ! -d "$dir" ]; then
        mkdir -p -- "$dir" 2>/dev/null || err "nao foi possivel criar o diretorio: $dir"
    fi
    printf '{\n  "hosts": []\n}\n' > "$file" 2>/dev/null || err "nao foi possivel criar o arquivo: $file"
    echo "sshm: nenhum cadastro encontrado; criado $file (adicione conexoes com 'sshm create')" >&2
}

# grava um JSON novo no arquivo de configuracao, validando antes de substituir
write_config() {
    local json="$1"
    printf '%s\n' "$json" > "${SSHM_CONFIG}.tmp" \
        || err "nao foi possivel escrever em ${SSHM_CONFIG}.tmp"
    if ! jq -e . "${SSHM_CONFIG}.tmp" >/dev/null 2>&1; then
        rm -f -- "${SSHM_CONFIG}.tmp"
        err "falha ao gerar o JSON; o arquivo $SSHM_CONFIG nao foi alterado."
    fi
    mv -- "${SSHM_CONFIG}.tmp" "$SSHM_CONFIG" || err "nao foi possivel atualizar $SSHM_CONFIG"
}

# (a atribuicao fica fora do teste: SSHM_CONFIG ainda precisa estar visivel
# na mensagem de erro quando o caminho indicado por ela nao existe)
CONFIG_FILE="$(locate_config)" || {
    CONFIG_FILE="$(config_new_path)"
    create_config_file "$CONFIG_FILE"
}
SSHM_CONFIG="$CONFIG_FILE"

usage() {
    cat <<EOF
Uso: sshm                              (selecao interativa)
     sshm select [-g <grupo>] [-t <tag>] [--flat]   (atalhos: -s, menu)
     sshm menu [--flat]                     (lista navegavel: setas/j k, Enter conecta, q sai)
     sshm create [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
                 [-g grupo] [-t tags] [-P]   (atalhos: new, --create, -c)
     sshm passwd [<id|nome>] [--remove] [--plain]
                                            (senha guardada; atalhos: senha, pass)
     sshm edit <id|nome> [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
                         [-g grupo] [-t tags]   (atalhos: editar, update)
     sshm delete <id|nome> [-y]              (atalhos: del, rm, remove, apagar)
     sshm <numero|nome> [args extras para o ssh]
     sshm list [-g <grupo>] [-t <tag>] [--flat]
     sshm ls | -l

Listagem e selecao separam os hosts por grupo (alfabetica, sem grupo no fim);
--flat volta a tabela unica com a coluna GRUPO.

Config atual: $SSHM_CONFIG
Procura nesta ordem: \$SSHM_CONFIG, hosts.json na pasta do script,
hosts.json na pasta atual, \$HOME/.config/sshm/hosts.json.
Se nenhum existir, cria um arquivo vazio (veja 'sshm create').
SSHM_PICKER=fzf|menu|number forca o seletor da selecao interativa.
SSHM_STRICT_HOSTKEY=1 pede confirmacao manual da chave de host na 1a conexao.
EOF
}

# imprime as linhas (tsv) dos hosts que casam com os filtros de grupo/tag,
# ordenados por grupo e depois por nome (hosts sem grupo ficam no fim)
filtered_rows() {
    local group_filter="$1" tag_filter="$2"
    jq -r --arg g "$group_filter" --arg t "$tag_filter" '
        [ .hosts[]
          | select($g == "" or (.group // "") == $g)
          | select($t == "" or ((.tags // []) | index($t)) != null)
          | { id: (.id|tostring),
              name: .name,
              userhost: (.user + "@" + .host),
              port: ((.port // 22)|tostring),
              group: (.group // ""),
              tags: ((.tags // []) | join(",")) } ]
        | sort_by([ (if .group == "" then "\u007f" else (.group | ascii_downcase) end),
                    (.name | ascii_downcase) ])
        | .[]
        | [ .id, .name, .userhost, .port,
            (if .group == "" then "-" else .group end), .tags ]
        | @tsv
    ' "$SSHM_CONFIG"
}

# rc=0 se alguma linha tsv da entrada tem grupo (usado para decidir o agrupamento)
rows_have_group() {
    local line id name userhost port group tags
    while IFS=$'\t' read -r id name userhost port group tags; do
        if [ -n "$group" ] && [ "$group" != "-" ]; then return 0; fi
    done
    return 1
}

# quantos hosts estao cadastrados
host_count() {
    jq -r '(.hosts // []) | length' "$SSHM_CONFIG"
}

# normaliza uma lista de tags: sem espacos nem itens vazios
normalize_tags() {
    printf '%s' "$1" | tr -d '[:space:]' | tr -s ',' | sed 's/^,//; s/,$//'
}

# rotulo de grupo para exibicao ("" ou "-" = sem grupo)
group_label() {
    if [ -z "$1" ] || [ "$1" = "-" ]; then
        printf '(sem grupo)'
    else
        printf '%s' "$1"
    fi
}

# existe conexao com esse nome? (sem diferenciar maiusculas de minusculas)
host_name_exists() {
    jq -e --arg n "$1" '
        any(.hosts[]?; ((.name // "") | ascii_downcase) == ($n | ascii_downcase))
    ' "$SSHM_CONFIG" >/dev/null 2>&1
}

list_hosts() {
    local group_filter="" tag_filter="" flat=0 rows=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -g) group_filter="${2:-}"; shift 2 ;;
            -t) tag_filter="${2:-}"; shift 2 ;;
            --flat|--sem-grupo) flat=1; shift ;;
            *) err "opcao desconhecida para list: $1" ;;
        esac
    done

    if [ "$(host_count)" -eq 0 ]; then
        echo "sshm: nenhum host cadastrado. Use 'sshm create' para adicionar uma conexao." >&2
        return 0
    fi

    rows="$(filtered_rows "$group_filter" "$tag_filter")"

    # sem grupos no resultado (ou com --flat): tabela unica, como antes
    if [ "$flat" -eq 1 ] || ! printf '%s\n' "$rows" | rows_have_group; then
        printf '%s\n' "$rows" \
        | awk -F'\t' 'BEGIN{printf "%-5s %-20s %-28s %-6s %-14s %s\n","ID","NOME","USER@HOST","PORTA","GRUPO","TAGS"}
                      {printf "%-5s %-20s %-28s %-6s %-14s %s\n",$1,$2,$3,$4,$5,$6}'
        return 0
    fi

    # agrupado: um bloco por grupo (hosts sem grupo no fim)
    local first=1 id="" name="" userhost="" port="" group="" tags="" last=""
    while IFS=$'\t' read -r id name userhost port group tags; do
        if [ "$group" != "$last" ]; then
            if [ "$first" -eq 0 ]; then printf '\n'; fi
            printf '[%s]\n' "$(group_label "$group")"
            printf '%-5s %-20s %-28s %-6s %s\n' "ID" "NOME" "USER@HOST" "PORTA" "TAGS"
            first=0
            last="$group"
        fi
        printf '%-5s %-20s %-28s %-6s %s\n' "$id" "$name" "$userhost" "$port" "$tags"
    done <<<"$rows"
}

# resolve um host por id numerico ou nome (case-insensitive); imprime o JSON compacto no stdout
resolve_entry() {
    local key="$1"
    if [[ "$key" =~ ^[0-9]+$ ]]; then
        jq -c --argjson id "$key" '.hosts[] | select(.id == $id)' "$SSHM_CONFIG"
    else
        jq -c --arg name "$key" '.hosts[] | select((.name|ascii_downcase) == ($name|ascii_downcase))' "$SSHM_CONFIG"
    fi
}

# --- senha guardada ---
#
# Um host pode ter a senha guardada de duas formas:
#   "password": "..."          texto puro no arquivo de configuracao
#   "password_secret": "<nome>"  no cofre do sistema (secret-tool/libsecret)
# A senha e entregue ao ssh por sshpass (fd 3, nao aparece na lista de
# processos) ou por SSH_ASKPASS quando o sshpass nao esta instalado.

# imprime a senha guardada de um host (vazio se nao houver)
entry_password() {
    local entry="$1" label="" secret=""
    label="$(jq -r '.password_secret // empty' <<<"$entry")"
    if [ -n "$label" ]; then
        if ! command -v secret-tool >/dev/null 2>&1; then
            echo "sshm: aviso: '$label' usa o cofre de senhas, mas o secret-tool nao esta instalado (sudo apt install libsecret-tools)." >&2
            return 0
        fi
        secret="$(secret-tool lookup service sshm label "$label" 2>/dev/null)" || secret=""
        if [ -z "$secret" ]; then
            echo "sshm: aviso: senha de '$label' nao encontrada no cofre; ela sera pedida no terminal." >&2
            return 0
        fi
        printf '%s\n' "$secret"
        return 0
    fi
    jq -r '.password // empty' <<<"$entry"
}

# cria o auxiliar do SSH_ASKPASS (imprime a senha de $SSHM_ASKPASS_PW)
# define ASKPASS_FILE; rc=1 se nao for possivel criar/executar
askpass_prepare() {
    local dir helper
    dir="${XDG_CACHE_HOME:-$HOME/.cache}/sshm"
    mkdir -p -- "$dir" 2>/dev/null || dir="${TMPDIR:-/tmp}"
    helper="$dir/askpass.sh"
    cat > "$helper" 2>/dev/null <<'SH' || return 1
#!/bin/sh
# gerado pelo sshm: entrega a senha guardada ao ssh via SSH_ASKPASS
printf '%s\n' "$SSHM_ASKPASS_PW"
SH
    chmod 700 -- "$helper" 2>/dev/null || true
    [ -x "$helper" ] || return 1
    ASKPASS_FILE="$helper"
    return 0
}

# le uma senha (pergunta duas vezes no terminal ou le uma linha do stdin); define REPLY_VALUE
read_password() {
    local name="$1" first="" again=""
    REPLY_VALUE=""
    if [ -t 0 ]; then
        printf 'Senha de %s (nao aparece na tela): ' "$name" >&2
        IFS= read -rs first || first=""
        printf '\n' >&2
        printf 'Repita a senha: ' >&2
        IFS= read -rs again || again=""
        printf '\n' >&2
        [ "$first" = "$again" ] || err "as senhas nao conferem."
    else
        IFS= read -r first || first=""
    fi
    REPLY_VALUE="$first"
}

# guarda a senha de $1: no cofre (secret-tool), se disponivel, senao em texto puro
# $2 = senha, $3 = 1 para forcar texto puro no arquivo
save_password() {
    local name="$1" password="$2" plain="${3:-0}" json=""

    if [ "$plain" -eq 0 ] && command -v secret-tool >/dev/null 2>&1; then
        printf '%s' "$password" | secret-tool store --label="sshm: $name" service sshm label "$name" \
            || err "falha ao gravar a senha no cofre (secret-tool)."
        json="$(jq --arg n "$name" '
            .hosts = ((.hosts // []) | map(
                if ((.name // "") == $n) then (.password_secret = $n | del(.password)) else . end))
        ' "$SSHM_CONFIG")"
        write_config "$json"
        echo "sshm: senha de '$name' guardada no cofre do sistema (secret-tool)." >&2
        return 0
    fi

    json="$(jq --arg n "$name" --arg p "$password" '
        .hosts = ((.hosts // []) | map(
            if ((.name // "") == $n) then (.password = $p | del(.password_secret)) else . end))
    ' "$SSHM_CONFIG")"
    write_config "$json"
    chmod 600 -- "$SSHM_CONFIG" 2>/dev/null || true
    echo "sshm: senha de '$name' guardada em texto puro em $SSHM_CONFIG (permissao 600)." >&2
    if ! command -v secret-tool >/dev/null 2>&1; then
        echo "sshm: para guardar cifrada, instale o libsecret-tools (sudo apt install libsecret-tools); ou use chave SSH (sshm create -i ...)." >&2
    fi
}

# apaga a senha guardada de $1 (arquivo e cofre)
remove_password() {
    local name="$1" json=""
    json="$(jq --arg n "$name" '
        .hosts = ((.hosts // []) | map(
            if ((.name // "") == $n) then del(.password, .password_secret) else . end))
    ' "$SSHM_CONFIG")"
    write_config "$json"
    if command -v secret-tool >/dev/null 2>&1; then
        secret-tool clear service sshm label "$name" 2>/dev/null || true
    fi
    echo "sshm: senha removida de '$name'." >&2
}

# conecta dado um JSON de host (stdin) + args extras do ssh
connect_entry() {
    local entry="$1"; shift
    local host user port identity group password name=""
    host="$(jq -r '.host' <<<"$entry")"
    user="$(jq -r '.user' <<<"$entry")"
    port="$(jq -r '.port // 22' <<<"$entry")"
    identity="$(jq -r '.identity_file // empty' <<<"$entry")"
    group="$(jq -r '.group // empty' <<<"$entry")"
    name="$(jq -r '.name // "?"' <<<"$entry")"
    password="$(entry_password "$entry")"

    local ssh_args=(-p "$port")
    if [ "${SSHM_STRICT_HOSTKEY:-0}" != "1" ]; then
        # 1a conexao num host novo: aceita a chave automaticamente (equivale a
        # responder "yes") e registra no known_hosts; chave que MUDOU continua
        # sendo recusada. Sem isso, o prompt de confirmacao do ssh iria para o
        # auxiliar de senha (SSH_ASKPASS) e a conexao falharia.
        ssh_args+=(-o StrictHostKeyChecking=accept-new)
    fi
    if [ -n "$identity" ]; then
        [ -f "$identity" ] || err "identity_file nao encontrado: $identity"
        ssh_args+=(-i "$identity")
    fi

    if [ -n "$group" ]; then
        echo "sshm: conectando em ${user}@${host}:${port} [grupo: ${group}]..." >&2
    else
        echo "sshm: conectando em ${user}@${host}:${port}..." >&2
    fi

    if [ -n "$password" ]; then
        echo "sshm: usando a senha guardada de '$name' (se estiver errada: sshm passwd '$name')" >&2
        if command -v sshpass >/dev/null 2>&1; then
            # a senha vai pelo fd 3 (nao aparece na lista de processos)
            exec 3<<<"$password"
            exec sshpass -d 3 ssh "${ssh_args[@]}" "${user}@${host}" "$@"
        fi
        if askpass_prepare; then
            # entrega a senha sem terminal (OpenSSH >= 8.4 le SSH_ASKPASS_REQUIRE)
            export SSH_ASKPASS="$ASKPASS_FILE" SSH_ASKPASS_REQUIRE=force SSHM_ASKPASS_PW="$password"
        else
            echo "sshm: aviso: nao consegui preparar o auxiliar de senha; ela sera pedida no terminal." >&2
        fi
    fi

    exec ssh "${ssh_args[@]}" "${user}@${host}" "$@"
}

# --- seletores de host ---
# Todos imprimem no stdout apenas o id escolhido (ou nada, se cancelado).

# seletor fuzzy via fzf
fuzzy_select() {
    local -a lines=("$@")
    local header picked
    command -v fzf >/dev/null 2>&1 || err "fzf nao esta instalado (sudo apt install fzf)."

    header="$(printf "%-5s %-20s %-28s %-6s %-14s %s" "ID" "NOME" "USER@HOST" "PORTA" "GRUPO" "TAGS")"
    picked="$(
        printf '%s\n' "${lines[@]}" \
        | awk -F'\t' '{printf "%-5s %-20s %-28s %-6s %-14s %s\n",$1,$2,$3,$4,$5,$6}' \
        | fzf --header="$header" --prompt="sshm> " --height=60% --border --no-multi
    )" || true

    if [ -z "$picked" ]; then
        echo "sshm: selecao cancelada." >&2
        return 0
    fi
    awk '{print $1}' <<<"$picked"
}

# menu numerado simples (usado quando a entrada nao e um terminal)
# $1 = 1 para agrupar por grupo; restantes = linhas tsv dos hosts
numbered_select() {
    local grouped="$1"; shift
    local -a lines=("$@")
    local -a ids=()
    local i=0 line id name userhost port group tags choice last=""
    local hfmt="" rfmt=""

    if ! printf '%s\n' "${lines[@]}" | rows_have_group; then
        grouped=0
    fi
    if [ "$grouped" -eq 1 ]; then
        hfmt='%3s  %-20s %-28s %-6s %s\n'
        rfmt='%3d) %-20s %-28s %-6s %s\n'
    else
        hfmt='%3s  %-20s %-28s %-6s %-14s %s\n'
        rfmt='%3d) %-20s %-28s %-6s %-14s %s\n'
    fi

    echo "Selecione um host:" >&2
    if [ "$grouped" -eq 1 ]; then
        printf "$hfmt" "#" "NOME" "USER@HOST" "PORTA" "TAGS" >&2
    else
        printf "$hfmt" "#" "NOME" "USER@HOST" "PORTA" "GRUPO" "TAGS" >&2
    fi

    for line in "${lines[@]}"; do
        IFS=$'\t' read -r id name userhost port group tags <<<"$line"
        if [ "$grouped" -eq 1 ] && [ "$group" != "$last" ]; then
            printf '[%s]\n' "$(group_label "$group")" >&2
            last="$group"
        fi
        i=$((i + 1))
        if [ "$grouped" -eq 1 ]; then
            printf "$rfmt" "$i" "$name" "$userhost" "$port" "$tags" >&2
        else
            printf "$rfmt" "$i" "$name" "$userhost" "$port" "$group" "$tags" >&2
        fi
        ids[$i]="$id"
    done

    read -rp "Numero (Enter para cancelar): " choice || choice=""
    if [ -z "$choice" ]; then
        echo "sshm: selecao cancelada." >&2
        return 0
    fi
    [[ "$choice" =~ ^[0-9]+$ ]] && [ -n "${ids[$choice]:-}" ] || err "opcao invalida."
    echo "${ids[$choice]}"
}

# --- menu navegavel (tela cheia, bash puro, sem dependencias) ---

# sai da tela alternativa e devolve o cursor (fd 3 = terminal de saida do menu)
menu_restore() {
    printf '\033[?25h\033[?1049l' >&3
}

# define MENU_ROWS e MENU_COLS com o tamanho atual do terminal
# (pty sem tamanho definido pode reportar 0 0; nesse caso usa LINES/COLUMNS ou 24x80)
menu_term_size() {
    local size=""
    size="$(stty size 2>/dev/null)" || size=""
    MENU_ROWS="${size%% *}"
    MENU_COLS="${size##* }"
    if ! [[ "$MENU_ROWS" =~ ^[0-9]+$ ]] || [ "$MENU_ROWS" -lt 4 ]; then
        MENU_ROWS="${LINES:-24}"
    fi
    if ! [[ "$MENU_COLS" =~ ^[0-9]+$ ]] || [ "$MENU_COLS" -lt 16 ]; then
        MENU_COLS="${COLUMNS:-80}"
    fi
    [[ "$MENU_ROWS" =~ ^[0-9]+$ ]] || MENU_ROWS=24
    [[ "$MENU_COLS" =~ ^[0-9]+$ ]] || MENU_COLS=80
}

# menu de tela cheia; imprime no stdout o id escolhido (ou nada, se cancelado)
# $1 = 1 para agrupar por grupo; restantes = linhas tsv dos hosts
navigable_select() {
    local grouped="$1"; shift
    local -a lines=("$@")
    local total_lines="${#lines[@]}"
    [ "$total_lines" -gt 0 ] || return 0
    [ -t 0 ] || err "o menu navegavel precisa de um terminal. Em scripts use SSHM_PICKER=number."

    if ! printf '%s\n' "${lines[@]}" | rows_have_group; then
        grouped=0
    fi

    local -a ids=() disp=() hno=()
    local line id name userhost port group tags i=0 last=""
    for line in "${lines[@]}"; do
        IFS=$'\t' read -r id name userhost port group tags <<<"$line"
        i=$((i + 1))
        if [ "$grouped" -eq 1 ] && [ "$group" != "$last" ]; then
            # cabecalho de grupo: nao e selecionavel (id vazio)
            disp+=("[$(group_label "$group")]")
            ids+=("")
            hno+=(0)
            last="$group"
        fi
        ids+=("$id")
        hno+=("$i")
        if [ "$grouped" -eq 1 ]; then
            disp+=("$(printf '%4d) %-18s %-26s %-5s %s' "$i" "$name" "$userhost" "$port" "$tags")")
        else
            disp+=("$(printf '%4d) %-18s %-24s %-5s %-12s %s' "$i" "$name" "$userhost" "$port" "$group" "$tags")")
        fi
    done

    local total="${#disp[@]}"
    local host_total="$i"
    local sel=0 offset=0 rows=1 width=80
    local key="" rest="" rest2="" rest3="" action="" msg="" buf="" footer="" text="" idx=0 j=0 n=0
    local target=0 dir=0 tries=0 found=0 pos=0

    # a selecao comeca no primeiro host (pode haver cabecalho de grupo antes)
    while [ "$sel" -lt "$total" ] && [ -z "${ids[$sel]}" ]; do sel=$((sel + 1)); done
    if [ "$sel" -ge "$total" ]; then sel=0; fi

    # a TUI nao pode escrever no stdout (capturado por quem chamou) e stderr pode
    # estar redirecionado; por isso desenha no terminal (fd 3), com fallback no stderr.
    # (o redirecionamento vai no bloco para nao afetar o stderr do proprio script)
    if ! { exec 3>/dev/tty; } 2>/dev/null; then
        exec 3>&2
    fi

    trap 'menu_restore' EXIT
    trap 'menu_restore; exec 3>&-; exit 130' INT TERM
    printf '\033[?1049h\033[?25l' >&3

    while : ; do
        menu_term_size
        rows=$((MENU_ROWS - 2))
        if [ "$rows" -lt 1 ]; then rows=1; fi
        if [ "$rows" -gt "$total" ]; then rows="$total"; fi
        if [ "$sel" -lt "$offset" ]; then offset="$sel"; fi
        if [ "$sel" -ge $((offset + rows)) ]; then offset=$((sel - rows + 1)); fi
        width=$((MENU_COLS - 1))
        if [ "$width" -lt 8 ]; then width=8; fi

        if [ -n "$msg" ]; then
            footer="$msg"
        elif [ -n "$buf" ]; then
            footer="ir para o host: ${buf}_   (Enter confirma, Backspace apaga)"
        else
            footer="setas/j k: mover   g/G: topo/fim   numero+Enter: ir   Enter: conectar   q: sair"
        fi
        footer=" ${hno[$sel]}/$host_total  $footer"

        {
            printf '\033[H'
            text="$(printf ' sshm - %d host(s)' "$host_total")"
            if [ "$grouped" -eq 1 ]; then
                text="$text (agrupado por grupo)"
            fi
            printf '\033[1;7m%-*s\033[0m\033[K\n' "$width" "${text:0:width}"
            for ((j = 0; j < rows; j++)); do
                idx=$((offset + j))
                if [ "$idx" -lt "$total" ]; then
                    text="${disp[$idx]}"
                    text="${text:0:width}"
                    if [ "$idx" -eq "$sel" ]; then
                        printf '\033[7m%-*s\033[0m\033[K\n' "$width" "$text"
                    elif [ -z "${ids[$idx]}" ]; then
                        printf '\033[1;36m%s\033[0m\033[K\n' "$text"
                    else
                        printf '%s\033[K\n' "$text"
                    fi
                else
                    printf '\033[K\n'
                fi
            done
            text="${footer:0:width}"
            printf '\033[7m%-*s\033[0m\033[K' "$width" "$text"
        } >&3

        IFS= read -rsn1 key || key=""
        action=""
        msg=""
        case "$key" in
            "") action="accept" ;;
            q|Q) action="cancel" ;;
            k|K) action="up" ;;
            j|J) action="down" ;;
            g) action="top" ;;
            G) action="bottom" ;;
            ' ') action="page-down" ;;
            [0-9]) if [ "${#buf}" -lt 5 ]; then buf+="$key"; fi; continue ;;
            $'\177'|$'\b') buf="${buf%?}"; continue ;;
            $'\033')
                # seta/home/end/pageup/pagedown, ou Esc sozinho (cancela)
                IFS= read -rsn1 -t 0.05 rest || rest=""
                if [ "$rest" = "[" ] || [ "$rest" = "O" ]; then
                    IFS= read -rsn1 -t 0.05 rest2 || rest2=""
                    case "$rest2" in
                        A) action="up" ;;
                        B) action="down" ;;
                        H) action="top" ;;
                        F) action="bottom" ;;
                        5) IFS= read -rsn1 -t 0.05 rest3 || rest3=""; action="page-up" ;;
                        6) IFS= read -rsn1 -t 0.05 rest3 || rest3=""; action="page-down" ;;
                        *) action="" ;;
                    esac
                elif [ -z "$rest" ]; then
                    action="cancel"
                fi
                ;;
        esac

        case "$action" in
            up) target=$(( (sel - 1 + total) % total )); dir=-1 ;;
            down) target=$(( (sel + 1) % total )); dir=1 ;;
            top) target=0; dir=1 ;;
            bottom) target=$((total - 1)); dir=-1 ;;
            page-up) target=$((sel - rows + 1)); if [ "$target" -lt 0 ]; then target=0; fi; dir=-1 ;;
            page-down) target=$((sel + rows - 1)); if [ "$target" -ge "$total" ]; then target=$((total - 1)); fi; dir=1 ;;
            accept)
                if [ -z "$buf" ]; then
                    menu_restore
                    trap - EXIT
                    echo "${ids[$sel]}"
                    return 0
                fi
                # numero digitado = numero do host na lista (nao a linha na tela)
                n=$((10#$buf))
                found=-1
                for ((pos = 0; pos < total; pos++)); do
                    if [ "${hno[$pos]}" = "$n" ]; then found="$pos"; break; fi
                done
                if [ "$found" -ge 0 ]; then
                    menu_restore
                    trap - EXIT
                    echo "${ids[$found]}"
                    return 0
                fi
                msg="host invalido: $n (use 1..$host_total)"
                buf=""
                target=-1
                ;;
            cancel)
                menu_restore
                trap - EXIT
                echo "sshm: selecao cancelada." >&2
                return 0
                ;;
            *) target=-1 ;;
        esac

        # a selecao sempre fica num host: pula os cabecalhos de grupo
        if [ "$target" -ge 0 ]; then
            tries=0
            while [ -z "${ids[$target]}" ] && [ "$tries" -lt "$total" ]; do
                target=$(( (target + dir + total) % total ))
                tries=$((tries + 1))
            done
            sel="$target"
        fi
    done
}

# pergunta um valor no terminal (EOF = Ctrl+D cancela o cadastro)
prompt_value() {
    local label="$1" default="${2:-}" answer=""
    [ -t 0 ] || err "sem terminal interativo: informe os dados por opcao (-n, -H, -u, -p, -i, -g, -t)."
    if [ -n "$default" ]; then
        read -rp "$label [$default]: " answer || exit 0
    else
        read -rp "$label: " answer || exit 0
    fi
    REPLY_VALUE="${answer:-$default}"
}

# cadastra uma nova conexao no arquivo de configuracao
# uso: sshm create [-n nome] [-H host] [-u usuario] [-p porta] [-i chave]
#                  [-g grupo] [-t tags] [-P|--password] [--plain]
create_host() {
    local name="" host="" user_in="" port="" identity="" group="" tags=""
    local answer="" json="" next_id="" line=""
    local ask_password=0 plain=0
    REPLY_VALUE=""

    while [ $# -gt 0 ]; do
        case "$1" in
            -n|--name) name="${2:-}"; shift 2 ;;
            -H|--host) host="${2:-}"; shift 2 ;;
            -u|--user) user_in="${2:-}"; shift 2 ;;
            -p|--port) port="${2:-}"; shift 2 ;;
            -i|--identity|--identity-file) identity="${2:-}"; shift 2 ;;
            -g|--group) group="${2:-}"; shift 2 ;;
            -t|--tags) tags="${2:-}"; shift 2 ;;
            -P|--password) ask_password=1; shift ;;
            --plain|--texto|--plaintext) plain=1; shift ;;
            -h|--help) usage; exit 0 ;;
            *) err "opcao desconhecida para create: $1" ;;
        esac
    done

    # nome: obrigatorio, sem espacos e sem repetir um ja cadastrado
    while : ; do
        if [ -z "$name" ]; then
            prompt_value "Nome/apelido (sem espacos)"
            name="${REPLY_VALUE// /}"
            if [ -z "$name" ]; then
                echo "sshm: o nome e obrigatorio." >&2
                continue
            fi
        fi
        if host_name_exists "$name"; then
            if [ -t 0 ]; then
                echo "sshm: ja existe uma conexao chamada '$name'; escolha outro nome." >&2
                name=""
                continue
            fi
            err "ja existe uma conexao chamada '$name'."
        fi
        break
    done

    if [ -z "$host" ]; then
        while [ -z "$host" ]; do
            prompt_value "Host/IP"
            host="$REPLY_VALUE"
            [ -n "$host" ] || echo "sshm: o host e obrigatorio." >&2
        done
    fi

    if [ -z "$user_in" ]; then
        if [ -t 0 ]; then
            prompt_value "Usuario" "$(id -un)"
            user_in="$REPLY_VALUE"
        else
            user_in="$(id -un)"
        fi
    fi

    if [ -z "$port" ]; then
        if [ -t 0 ]; then
            prompt_value "Porta" "22"
            port="$REPLY_VALUE"
        else
            port="22"
        fi
    fi
    case "$port" in
        ""|*[!0-9]*) err "porta invalida: '$port'" ;;
    esac
    if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        err "porta fora da faixa 1-65535: $port"
    fi

    if [ -z "$identity" ] && [ -t 0 ]; then
        prompt_value "Arquivo de chave (Enter para nenhum)"
        identity="$REPLY_VALUE"
    fi
    if [ -z "$group" ] && [ -t 0 ]; then
        prompt_value "Grupo (Enter para nenhum)"
        group="$REPLY_VALUE"
    fi
    if [ -z "$tags" ] && [ -t 0 ]; then
        prompt_value "Tags separadas por virgula (Enter para nenhuma)"
        tags="$REPLY_VALUE"
    fi

    if [ -n "$identity" ] && [ ! -f "$identity" ]; then
        echo "sshm: aviso: o arquivo de chave '$identity' nao existe (a conexao sera criada mesmo assim)." >&2
    fi

    tags="$(normalize_tags "$tags")"

    next_id="$(jq -r '([.hosts[]? | (.id // 0)] | max // 0) + 1' "$SSHM_CONFIG")"
    json="$(jq --argjson id "$next_id" --arg name "$name" --arg host "$host" \
        --arg user "$user_in" --argjson port "$port" --arg identity "$identity" \
        --arg group "$group" --arg tags "$tags" '
        .hosts = ((.hosts // []) + [(
            {id: $id, name: $name, host: $host, user: $user, port: $port}
            + (if $identity == "" then {} else {identity_file: $identity} end)
            + (if $group == "" then {} else {group: $group} end)
            + (if $tags == "" then {}
               else {tags: ($tags | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(length > 0)))}
               end)
        )])
    ' "$SSHM_CONFIG")"

    printf '%s\n' "$json" > "${SSHM_CONFIG}.tmp" \
        || err "nao foi possivel escrever em ${SSHM_CONFIG}.tmp"
    if ! jq -e . "${SSHM_CONFIG}.tmp" >/dev/null 2>&1; then
        rm -f -- "${SSHM_CONFIG}.tmp"
        err "falha ao gerar o JSON; o arquivo $SSHM_CONFIG nao foi alterado."
    fi
    mv -- "${SSHM_CONFIG}.tmp" "$SSHM_CONFIG" || err "nao foi possivel atualizar $SSHM_CONFIG"

    line="${user_in}@${host}:${port}"
    if [ -n "$group" ]; then line="$line  [grupo: $group]"; fi
    if [ -n "$tags" ]; then line="$line  [tags: $tags]"; fi
    echo "sshm: conexao '$name' criada (id $next_id): $line" >&2

    # senha: com -P pergunta sempre; sem -P, oferece no modo interativo
    if [ "$ask_password" -eq 0 ] && [ -t 0 ]; then
        read -rp "Guardar a senha para entrar direto (sem digitar toda vez)? [s/N]: " answer || answer=""
        case "$answer" in
            s|S|y|Y|sim|SIM|Sim) ask_password=1 ;;
        esac
    fi
    if [ "$ask_password" -eq 1 ]; then
        read_password "$name"
        if [ -z "$REPLY_VALUE" ]; then
            echo "sshm: nenhuma senha guardada (senha vazia)." >&2
        else
            save_password "$name" "$REPLY_VALUE" "$plain"
        fi
    fi

    if [ -t 0 ]; then
        read -rp "Conectar agora? [s/N]: " answer || answer=""
        case "$answer" in
            s|S|y|Y|sim|SIM|Sim) connect_entry "$(resolve_entry "$name")" ;;
            *) echo "sshm: use 'sshm $name' ou 'sshm menu' quando quiser conectar." >&2 ;;
        esac
    fi
}

# mostra quais conexoes tem senha guardada
password_status() {
    local rows="" id="" name="" status=""
    rows="$(jq -r '
        .hosts[]?
        | [ (.id // 0 | tostring),
            (.name // "?"),
            (if (.password_secret // "") != "" then "cofre (secret-tool)"
             elif (.password // "") != "" then "texto puro no arquivo"
             else "sem senha" end) ]
        | @tsv
    ' "$SSHM_CONFIG")"
    if [ -z "$rows" ]; then
        echo "sshm: nenhum host cadastrado. Use 'sshm create' para adicionar uma conexao." >&2
        return 0
    fi
    printf '%-6s %-22s %s\n' "ID" "NOME" "SENHA"
    while IFS=$'\t' read -r id name status; do
        printf '%-6s %-22s %s\n' "$id" "$name" "$status"
    done <<<"$rows"
    printf '\nGuardar ou trocar: sshm passwd <nome>\nApagar:             sshm passwd <nome> --remove\n'
}

# define, troca ou remove a senha de uma conexao
# uso: sshm passwd [<id|nome>] [--remove] [--plain]
set_password() {
    local key="" remove=0 plain=0 entry="" name=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --remove|--rm|--delete|--apagar) remove=1; shift ;;
            --plain|--texto|--plaintext) plain=1; shift ;;
            -h|--help) usage; exit 0 ;;
            -*) err "opcao desconhecida para passwd: $1" ;;
            *) if [ -n "$key" ]; then err "informe apenas um host por vez."; fi; key="$1"; shift ;;
        esac
    done

    if [ -z "$key" ]; then
        password_status
        return 0
    fi

    entry="$(resolve_entry "$key")"
    [ -n "$entry" ] || err "nenhum host encontrado para '$key'. Use 'sshm list' para ver os disponiveis."
    name="$(jq -r '.name' <<<"$entry")"

    if [ "$remove" -eq 1 ]; then
        remove_password "$name"
        return 0
    fi

    read_password "$name"
    if [ -z "$REPLY_VALUE" ]; then
        err "senha vazia; para apagar a senha use: sshm passwd '$name' --remove"
    fi
    save_password "$name" "$REPLY_VALUE" "$plain"
}

# define REPLY_ENTRY com o unico host que casa com $1 (aborta se nenhum ou varios)
resolve_unique() {
    local key="$1" count=""
    REPLY_ENTRY="$(resolve_entry "$key")"
    if [ -z "$REPLY_ENTRY" ]; then
        err "nenhum host encontrado para '$key'. Use 'sshm list' para ver os disponiveis."
    fi
    count="$(printf '%s\n' "$REPLY_ENTRY" | jq -s 'length')"
    if [ "$count" -gt 1 ]; then
        err "mais de um host casou com '$key'; use o id para escolher."
    fi
}

# indice (0-based) do host $1 (JSON compacto) dentro do arquivo
host_index() {
    local entry="$1"
    jq -r --argjson e "$entry" '(.hosts // []) | to_entries[] | select(.value == $e) | .key' "$SSHM_CONFIG"
}

# edita uma conexao existente
# uso: sshm edit <id|nome> [-n nome] [-H host] [-u usuario] [-p porta]
#                          [-i chave] [-g grupo] [-t tags]
edit_host() {
    local key="" name="" host="" user_in="" port="" identity="" group="" tags=""
    local has_name=0 has_host=0 has_user=0 has_port=0 has_ident=0 has_group=0 has_tags=0
    local entry="" idx="" json="" line="" secret_label="" secret="" moved=0
    local old_name="" old_host="" old_user="" old_port="" old_ident="" old_group="" old_tags=""
    REPLY_VALUE=""
    REPLY_ENTRY=""

    while [ $# -gt 0 ]; do
        case "$1" in
            -n|--name) name="${2:-}"; has_name=1; shift 2 ;;
            -H|--host) host="${2:-}"; has_host=1; shift 2 ;;
            -u|--user) user_in="${2:-}"; has_user=1; shift 2 ;;
            -p|--port) port="${2:-}"; has_port=1; shift 2 ;;
            -i|--identity|--identity-file) identity="${2:-}"; has_ident=1; shift 2 ;;
            -g|--group) group="${2:-}"; has_group=1; shift 2 ;;
            -t|--tags) tags="${2:-}"; has_tags=1; shift 2 ;;
            -h|--help) usage; exit 0 ;;
            -*) err "opcao desconhecida para edit: $1" ;;
            *) if [ -n "$key" ]; then err "informe apenas um host por vez."; fi; key="$1"; shift ;;
        esac
    done
    [ -n "$key" ] || err "uso: sshm edit <id|nome> [-n nome] [-H host] [-u usuario] [-p porta] [-i chave] [-g grupo] [-t tags]"

    resolve_unique "$key"
    entry="$REPLY_ENTRY"
    idx="$(host_index "$entry")"
    [ -n "$idx" ] || err "nao encontrei '$key' no arquivo $SSHM_CONFIG (ele mudou durante a edicao?)."

    old_name="$(jq -r '.name // ""' <<<"$entry")"
    old_host="$(jq -r '.host // ""' <<<"$entry")"
    old_user="$(jq -r '.user // ""' <<<"$entry")"
    old_port="$(jq -r '.port // 22' <<<"$entry")"
    old_ident="$(jq -r '.identity_file // ""' <<<"$entry")"
    old_group="$(jq -r '.group // ""' <<<"$entry")"
    old_tags="$(jq -r '(.tags // []) | join(",")' <<<"$entry")"

    if [ "$has_name$has_host$has_user$has_port$has_ident$has_group$has_tags" = "0000000" ]; then
        # sem opcoes: pergunta campo a campo mostrando o valor atual
        [ -t 0 ] || err "sem terminal interativo: use as opcoes (-n, -H, -u, -p, -i, -g, -t)."
        echo "Editando '$old_name' - Enter mantem o valor atual ('-' limpa os opcionais)" >&2
        prompt_value "Nome/apelido" "$old_name"; name="$REPLY_VALUE"
        prompt_value "Host/IP" "$old_host"; host="$REPLY_VALUE"
        prompt_value "Usuario" "$old_user"; user_in="$REPLY_VALUE"
        prompt_value "Porta" "$old_port"; port="$REPLY_VALUE"
        prompt_value "Arquivo de chave" "$old_ident"; identity="$REPLY_VALUE"
        prompt_value "Grupo" "$old_group"; group="$REPLY_VALUE"
        prompt_value "Tags (virgula)" "$old_tags"; tags="$REPLY_VALUE"
        if [ "$identity" = "-" ]; then identity=""; fi
        if [ "$group" = "-" ]; then group=""; fi
        if [ "$tags" = "-" ]; then tags=""; fi
    else
        # por opcoes: mantem o que nao foi informado
        if [ "$has_name" -eq 0 ]; then name="$old_name"; fi
        if [ "$has_host" -eq 0 ]; then host="$old_host"; fi
        if [ "$has_user" -eq 0 ]; then user_in="$old_user"; fi
        if [ "$has_port" -eq 0 ]; then port="$old_port"; fi
        if [ "$has_ident" -eq 0 ]; then identity="$old_ident"; fi
        if [ "$has_group" -eq 0 ]; then group="$old_group"; fi
        if [ "$has_tags" -eq 0 ]; then tags="$old_tags"; fi
    fi

    tags="$(normalize_tags "$tags")"
    name="${name// /}"
    [ -n "$name" ] || err "o nome nao pode ficar vazio."
    [ -n "$host" ] || err "o host nao pode ficar vazio."
    case "$port" in
        ""|*[!0-9]*) err "porta invalida: '$port'" ;;
    esac
    if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        err "porta fora da faixa 1-65535: $port"
    fi
    if [ "$name" != "$old_name" ] && host_name_exists "$name"; then
        err "ja existe uma conexao chamada '$name'."
    fi
    if [ -n "$identity" ] && [ ! -f "$identity" ]; then
        echo "sshm: aviso: o arquivo de chave '$identity' nao existe (a conexao sera salva mesmo assim)." >&2
    fi

    # senha no cofre: move para o nome novo quando a conexao e renomeada
    secret_label="$(jq -r '.password_secret // empty' <<<"$entry")"
    if [ -n "$secret_label" ] && [ "$name" != "$old_name" ] && command -v secret-tool >/dev/null 2>&1; then
        secret="$(secret-tool lookup service sshm label "$secret_label" 2>/dev/null)" || secret=""
        if [ -n "$secret" ]; then
            printf '%s' "$secret" | secret-tool store --label="sshm: $name" service sshm label "$name" 2>/dev/null || true
            secret-tool clear service sshm label "$secret_label" 2>/dev/null || true
            moved=1
        else
            echo "sshm: aviso: nao consegui ler a senha de '$secret_label' no cofre; rode 'sshm passwd $name'." >&2
        fi
    fi

    json="$(jq --argjson i "$idx" \
        --arg name "$name" --arg host "$host" --arg user "$user_in" \
        --argjson port "$port" --arg identity "$identity" --arg group "$group" \
        --arg tags "$tags" --argjson moved "$moved" --arg old "$old_name" '
        .hosts[$i] = (.hosts[$i]
            | .name = $name | .host = $host | .user = $user | .port = $port
            | (if $identity == "" then del(.identity_file) else .identity_file = $identity end)
            | (if $group == "" then del(.group) else .group = $group end)
            | (if ($tags | gsub("^\\s+|\\s+$"; "")) == "" then del(.tags)
               else .tags = ($tags | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(length > 0))) end)
            | (if $moved == 1 and ((.password_secret // "") == $old) then .password_secret = $name else . end))
    ' "$SSHM_CONFIG")"
    write_config "$json"

    line="${user_in}@${host}:${port}"
    if [ -n "$group" ]; then line="$line  [grupo: $group]"; fi
    if [ -n "$tags" ]; then line="$line  [tags: $tags]"; fi
    echo "sshm: conexao '$name' atualizada: $line" >&2
    if [ "$name" != "$old_name" ]; then
        echo "sshm: nome alterado de '$old_name' para '$name'." >&2
    fi
}

# apaga uma conexao
# uso: sshm delete <id|nome> [-y|--yes]
delete_host() {
    local key="" assume_yes=0 answer="" entry="" idx="" json=""
    local name="" host="" user_in="" port="" secret_label=""
    REPLY_ENTRY=""

    while [ $# -gt 0 ]; do
        case "$1" in
            -y|--yes|--force|-f) assume_yes=1; shift ;;
            -h|--help) usage; exit 0 ;;
            -*) err "opcao desconhecida para delete: $1" ;;
            *) if [ -n "$key" ]; then err "informe apenas um host por vez."; fi; key="$1"; shift ;;
        esac
    done
    [ -n "$key" ] || err "uso: sshm delete <id|nome> [-y]"

    resolve_unique "$key"
    entry="$REPLY_ENTRY"
    idx="$(host_index "$entry")"
    [ -n "$idx" ] || err "nao encontrei '$key' no arquivo $SSHM_CONFIG (ele mudou durante a edicao?)."

    name="$(jq -r '.name // "?"' <<<"$entry")"
    host="$(jq -r '.host // "?"' <<<"$entry")"
    user_in="$(jq -r '.user // "?"' <<<"$entry")"
    port="$(jq -r '.port // 22' <<<"$entry")"
    secret_label="$(jq -r '.password_secret // empty' <<<"$entry")"

    if [ "$assume_yes" -eq 0 ]; then
        if [ ! -t 0 ]; then
            err "sem terminal para confirmar; use: sshm delete '$key' -y"
        fi
        read -rp "Apagar a conexao '$name' (${user_in}@${host}:${port})? [s/N]: " answer || answer=""
        case "$answer" in
            s|S|y|Y|sim|SIM|Sim) : ;;
            *) echo "sshm: nada foi apagado." >&2; return 0 ;;
        esac
    fi

    json="$(jq --argjson i "$idx" '.hosts |= del(.[$i])' "$SSHM_CONFIG")"
    write_config "$json"
    if [ -n "$secret_label" ] && command -v secret-tool >/dev/null 2>&1; then
        secret-tool clear service sshm label "$secret_label" 2>/dev/null || true
    fi
    echo "sshm: conexao '$name' (${user_in}@${host}:${port}) apagada." >&2
    if [ "$(host_count)" -eq 0 ]; then
        echo "sshm: nao ha mais conexoes cadastradas; use 'sshm create' para adicionar." >&2
    fi
}

# escolhe o seletor (fzf/menu/numerado) e imprime o id escolhido
interactive_select() {
    local group_filter="" tag_filter="" grouped=1
    while [ $# -gt 0 ]; do
        case "$1" in
            -g) group_filter="${2:-}"; shift 2 ;;
            -t) tag_filter="${2:-}"; shift 2 ;;
            --flat|--sem-grupo) grouped=0; shift ;;
            -h|--help) usage; exit 0 ;;
            *) err "opcao desconhecida: $1" ;;
        esac
    done

    if [ "$(host_count)" -eq 0 ]; then
        err "nenhum host cadastrado. Use 'sshm create' para adicionar uma conexao."
    fi

    mapfile -t LINES < <(filtered_rows "$group_filter" "$tag_filter")
    [ ${#LINES[@]} -gt 0 ] || err "nenhum host encontrado para os filtros informados."

    case "${SSHM_PICKER:-auto}" in
        fzf) fuzzy_select "${LINES[@]}" ;;
        menu|tui) navigable_select "$grouped" "${LINES[@]}" ;;
        number|numerado) numbered_select "$grouped" "${LINES[@]}" ;;
        auto)
            if command -v fzf >/dev/null 2>&1; then
                fuzzy_select "${LINES[@]}"
            elif [ -t 0 ]; then
                navigable_select "$grouped" "${LINES[@]}"
            else
                numbered_select "$grouped" "${LINES[@]}"
            fi
            ;;
        *) err "SSHM_PICKER invalido: '${SSHM_PICKER}' (use fzf, menu ou number)." ;;
    esac
}

# --- dispatch ---

if [ $# -eq 0 ]; then
    SELECTED_ID="$(interactive_select)"
    [ -n "$SELECTED_ID" ] || exit 0
    ENTRY="$(resolve_entry "$SELECTED_ID")"
    connect_entry "$ENTRY"
fi

case "$1" in
    list)
        shift
        list_hosts "$@"
        exit 0
        ;;
    -l|ls)
        list_hosts
        exit 0
        ;;
    select|-s|menu|pick)
        shift
        SELECTED_ID="$(interactive_select "$@")"
        [ -n "$SELECTED_ID" ] || exit 0
        ENTRY="$(resolve_entry "$SELECTED_ID")"
        connect_entry "$ENTRY"
        ;;
    create|new|--create|-c)
        shift
        create_host "$@"
        exit 0
        ;;
    passwd|password|senha|pass)
        shift
        set_password "$@"
        exit 0
        ;;
    edit|editar|update|--edit)
        shift
        edit_host "$@"
        exit 0
        ;;
    delete|del|rm|remove|apagar|--delete)
        shift
        delete_host "$@"
        exit 0
        ;;
    -h|--help)
        usage
        exit 0
        ;;
esac

KEY="$1"
shift

ENTRY="$(resolve_entry "$KEY")"
[ -n "$ENTRY" ] || err "nenhum host encontrado para '$KEY'. Use 'sshm list' para ver os disponiveis."

MATCHES="$(printf '%s\n' "$ENTRY" | jq -s 'length')"
if [ "$MATCHES" -gt 1 ]; then
    err "mais de um host casou com '$KEY'. Ajuste o arquivo de configuracao (ids/nomes duplicados?)."
fi

connect_entry "$ENTRY" "$@"
