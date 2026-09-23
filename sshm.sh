#!/usr/bin/env bash
#
# sshm - gerenciador de conexoes SSH
#
# Uso:
#   sshm                              (abre selecao interativa)
#   sshm select [-g <grupo>] [-t <tag>]   (idem, com filtro; atalhos: -s, menu)
#   sshm menu                         (forca a lista navegavel em tela cheia)
#   sshm <numero|nome> [args extras repassados para o ssh]
#   sshm list [-g <grupo>] [-t <tag>]
#   sshm ls | -l           (atalho para list sem filtro)
#   sshm -h | --help
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
# Arquivo de configuracao (JSON), procurado nesta ordem:
#   1) $SSHM_CONFIG, se definida;
#   2) $HOME/.config/sshm/hosts.json;
#   3) hosts.json na mesma pasta deste script;
#   4) hosts.json na pasta atual.
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
    candidates+=("$HOME/.config/sshm/hosts.json" "$(script_dir)/hosts.json" "$PWD/hosts.json")
    for c in "${candidates[@]}"; do
        case " $seen " in
            *" $c "*) continue ;;
        esac
        seen="$seen $c"
        printf '%s\n' "$c"
    done
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

# (a atribuicao fica fora do teste: SSHM_CONFIG ainda precisa estar visivel
# na mensagem de erro quando o caminho indicado por ela nao existe)
CONFIG_FILE="$(locate_config)" \
    || err "$(printf 'arquivo de configuracao nao encontrado. Procurado em:\n%s\nCrie o arquivo (veja hosts.json.example) ou defina SSHM_CONFIG=/caminho/para/hosts.json' "$(config_candidates | sed 's/^/  - /')")"
SSHM_CONFIG="$CONFIG_FILE"

usage() {
    cat <<EOF
Uso: sshm                              (selecao interativa)
     sshm select [-g <grupo>] [-t <tag>]   (atalhos: -s, menu)
     sshm menu                              (lista navegavel: setas/j k, Enter conecta, q sai)
     sshm <numero|nome> [args extras para o ssh]
     sshm list [-g <grupo>] [-t <tag>]
     sshm ls | -l

Config atual: $SSHM_CONFIG
Procura nesta ordem: \$SSHM_CONFIG, \$HOME/.config/sshm/hosts.json,
hosts.json na pasta do script, hosts.json na pasta atual.
SSHM_PICKER=fzf|menu|number forca o seletor da selecao interativa.
EOF
}

# imprime as linhas (tsv) dos hosts que casam com os filtros de grupo/tag
filtered_rows() {
    local group_filter="$1" tag_filter="$2"
    jq -r --arg g "$group_filter" --arg t "$tag_filter" '
        .hosts[]
        | select($g == "" or (.group // "") == $g)
        | select($t == "" or ((.tags // []) | index($t)) != null)
        | [
            (.id|tostring),
            .name,
            (.user + "@" + .host),
            ((.port // 22)|tostring),
            (.group // "-"),
            ((.tags // []) | join(","))
          ]
        | @tsv
    ' "$SSHM_CONFIG"
}

list_hosts() {
    local group_filter="" tag_filter=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -g) group_filter="${2:-}"; shift 2 ;;
            -t) tag_filter="${2:-}"; shift 2 ;;
            *) err "opcao desconhecida para list: $1" ;;
        esac
    done

    filtered_rows "$group_filter" "$tag_filter" \
    | awk -F'\t' 'BEGIN{printf "%-5s %-20s %-28s %-6s %-14s %s\n","ID","NOME","USER@HOST","PORTA","GRUPO","TAGS"}
                  {printf "%-5s %-20s %-28s %-6s %-14s %s\n",$1,$2,$3,$4,$5,$6}'
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

# conecta dado um JSON de host (stdin) + args extras do ssh
connect_entry() {
    local entry="$1"; shift
    local host user port identity group
    host="$(jq -r '.host' <<<"$entry")"
    user="$(jq -r '.user' <<<"$entry")"
    port="$(jq -r '.port // 22' <<<"$entry")"
    identity="$(jq -r '.identity_file // empty' <<<"$entry")"
    group="$(jq -r '.group // empty' <<<"$entry")"

    local ssh_args=(-p "$port")
    if [ -n "$identity" ]; then
        [ -f "$identity" ] || err "identity_file nao encontrado: $identity"
        ssh_args+=(-i "$identity")
    fi

    if [ -n "$group" ]; then
        echo "sshm: conectando em ${user}@${host}:${port} [grupo: ${group}]..." >&2
    else
        echo "sshm: conectando em ${user}@${host}:${port}..." >&2
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
numbered_select() {
    local -a lines=("$@")
    local -a ids=()
    local i=1 line id name userhost port group tags choice

    echo "Selecione um host:" >&2
    printf "%3s  %-20s %-28s %-6s %-14s %s\n" "#" "NOME" "USER@HOST" "PORTA" "GRUPO" "TAGS" >&2
    for line in "${lines[@]}"; do
        IFS=$'\t' read -r id name userhost port group tags <<<"$line"
        printf "%3d) %-20s %-28s %-6s %-14s %s\n" "$i" "$name" "$userhost" "$port" "$group" "$tags" >&2
        ids[$i]="$id"
        i=$((i + 1))
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
navigable_select() {
    local -a lines=("$@")
    local total="${#lines[@]}"
    [ "$total" -gt 0 ] || return 0
    [ -t 0 ] || err "o menu navegavel precisa de um terminal. Em scripts use SSHM_PICKER=number."

    local -a ids=() disp=()
    local line id name userhost port group tags i=0
    for line in "${lines[@]}"; do
        IFS=$'\t' read -r id name userhost port group tags <<<"$line"
        ids+=("$id")
        i=$((i + 1))
        disp+=("$(printf '%4d) %-18s %-24s %-5s %-12s %s' "$i" "$name" "$userhost" "$port" "$group" "$tags")")
    done

    local sel=0 offset=0 rows=1 width=80
    local key="" rest="" rest2="" rest3="" action="" msg="" buf="" footer="" text="" idx=0 j=0 n=0

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
            footer="ir para a linha: ${buf}_   (Enter confirma, Backspace apaga)"
        else
            footer="setas/j k: mover   g/G: topo/fim   numero+Enter: ir   Enter: conectar   q: sair"
        fi
        footer=" $((sel + 1))/$total  $footer"

        {
            printf '\033[H'
            text="$(printf ' sshm - %d host(s)' "$total")"
            printf '\033[1;7m%-*s\033[0m\033[K\n' "$width" "${text:0:width}"
            for ((j = 0; j < rows; j++)); do
                idx=$((offset + j))
                if [ "$idx" -lt "$total" ]; then
                    text="${disp[$idx]}"
                    text="${text:0:width}"
                    if [ "$idx" -eq "$sel" ]; then
                        printf '\033[7m%-*s\033[0m\033[K\n' "$width" "$text"
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
            up) if [ "$sel" -gt 0 ]; then sel=$((sel - 1)); else sel=$((total - 1)); fi ;;
            down) sel=$(( (sel + 1) % total )) ;;
            top) sel=0 ;;
            bottom) sel=$((total - 1)) ;;
            page-up) sel=$((sel - rows + 1)); if [ "$sel" -lt 0 ]; then sel=0; fi ;;
            page-down) sel=$((sel + rows - 1)); if [ "$sel" -ge "$total" ]; then sel=$((total - 1)); fi ;;
            accept)
                if [ -n "$buf" ]; then
                    n=$((10#$buf))
                    if [ "$n" -ge 1 ] && [ "$n" -le "$total" ]; then
                        menu_restore
                        trap - EXIT
                        echo "${ids[$((n - 1))]}"
                        return 0
                    fi
                    msg="linha invalida: $n (use 1..$total)"
                    buf=""
                else
                    menu_restore
                    trap - EXIT
                    echo "${ids[$sel]}"
                    return 0
                fi
                ;;
            cancel)
                menu_restore
                trap - EXIT
                echo "sshm: selecao cancelada." >&2
                return 0
                ;;
        esac
    done
}

# escolhe o seletor (fzf/menu/numerado) e imprime o id escolhido
interactive_select() {
    local group_filter="" tag_filter=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -g) group_filter="${2:-}"; shift 2 ;;
            -t) tag_filter="${2:-}"; shift 2 ;;
            *) err "opcao desconhecida: $1" ;;
        esac
    done

    mapfile -t LINES < <(filtered_rows "$group_filter" "$tag_filter")
    [ ${#LINES[@]} -gt 0 ] || err "nenhum host encontrado para os filtros informados."

    case "${SSHM_PICKER:-auto}" in
        fzf) fuzzy_select "${LINES[@]}" ;;
        menu|tui) navigable_select "${LINES[@]}" ;;
        number|numerado) numbered_select "${LINES[@]}" ;;
        auto)
            if command -v fzf >/dev/null 2>&1; then
                fuzzy_select "${LINES[@]}"
            elif [ -t 0 ]; then
                navigable_select "${LINES[@]}"
            else
                numbered_select "${LINES[@]}"
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
