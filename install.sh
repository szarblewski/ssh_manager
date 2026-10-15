#!/usr/bin/env bash
#
# [LEGADO] O sshm agora e um binario unico em Go (Windows, Linux e macOS).
# No Windows use install.ps1; no Linux/macOS use 'go install' ou o pacote da
# release (veja README.md e docs/migracao.md). Este instalador continua
# funcionando para quem quiser manter a versao em bash.
#
# install.sh - instala o sshm.sh como o comando "sshm", disponivel de
# qualquer pasta do sistema.
#
# Uso:
#   ./install.sh                  instala em ~/.local/bin (sem sudo)
#   ./install.sh --system         instala em /usr/local/bin (com sudo)
#   ./install.sh --symlink        cria um link para o sshm.sh deste repositorio
#   curl -fsSL <url>/install.sh | bash
#
# Opcoes:
#   --prefix DIR       instala em DIR/bin (padrao: ~/.local)
#   --bin-dir DIR      instala direto em DIR (vence --prefix/--system)
#   --system           equivale a --prefix /usr/local
#   --symlink          aponta para o sshm.sh local em vez de copiar (a
#                      configuracao continua junto do sshm.sh)
#   --from ORIGEM      usa ORIGEM (arquivo local ou URL) como fonte do sshm.sh
#   --config ARQUIVO   usa ARQUIVO como hosts.json (grava SSHM_CONFIG no shell)
#   --no-path          nao altera os arquivos de inicializacao do shell
#   --no-config        nao cria/copia o hosts.json
#   --with-deps        instala dependencias que faltarem (jq)
#   --uninstall        remove o comando e as linhas que o instalador criou
#   --purge-config     com --uninstall, apaga tambem ~/.config/sshm
#   -f, --force        sobrescreve um arquivo que nao pareca ser o sshm
#   -h, --help         mostra esta ajuda
#
# Variaveis de ambiente:
#   SSHM_INSTALL_URL   URL do sshm.sh usada quando nao existe uma copia local
#                      (padrao: raw.githubusercontent.com/szarblewski/ssh_manager)
#
# O que o instalador faz:
#   1. copia (ou linka) o sshm.sh para <bin-dir>/sshm, com permissao de execucao;
#   2. garante um hosts.json em ~/.config/sshm/hosts.json, copiando o hosts.json
#      local quando existir - e um dos caminhos que o sshm procura, o que faz o
#      comando funcionar de qualquer pasta;
#   3. adiciona <bin-dir> ao PATH do shell (bloco com marcadores, idempotente);
#   4. confere o jq (obrigatorio) e testa o comando instalado.
#
# Nada disso e feito as escondidas: o instalador diz o que escreveu e onde, e
# 'install.sh --uninstall' desfaz as alteracoes nos arquivos do shell.

# o script usa recursos do bash (local -a, BASH_SOURCE, pipefail): avisa quem
# tentar rodar com 'sh' ou 'zsh' em vez de deixar um erro obscuro
if [ -z "${BASH_VERSION:-}" ]; then
    printf 'install.sh: rode com o bash (ex.: ./install.sh ou bash install.sh)\n' >&2
    exit 1
fi

set -euo pipefail

readonly SCRIPT_NAME="sshm"
readonly MARK_PREFIX="sshm install"
readonly DEFAULT_URL="https://raw.githubusercontent.com/szarblewski/ssh_manager/main/sshm.sh"

die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
info() { printf 'install.sh: %s\n' "$*"; }
warn() { printf 'install.sh: aviso: %s\n' "$*" >&2; }

usage() {
    cat <<EOF
Uso: install.sh [opcoes]

  --prefix DIR       instala em DIR/bin (padrao: ~/.local)
  --bin-dir DIR      instala direto em DIR (vence --prefix/--system)
  --system           equivale a --prefix /usr/local (normalmente com sudo)
  --symlink          aponta para o sshm.sh local em vez de copiar
  --from ORIGEM      fonte do sshm.sh: arquivo local ou URL
  --config ARQUIVO   usa ARQUIVO como hosts.json (grava SSHM_CONFIG no shell)
  --no-path          nao altera os arquivos de inicializacao do shell
  --no-config        nao cria/copia o hosts.json
  --with-deps        instala dependencias que faltarem (jq)
  --uninstall        remove o comando e as linhas que o instalador criou
  --purge-config     com --uninstall, apaga tambem ~/.config/sshm
  -f, --force        sobrescreve um arquivo que nao pareca ser o sshm
  -h, --help         mostra esta ajuda

Exemplos:
  ./install.sh                        # ~/.local/bin/sshm
  sudo ./install.sh --system          # /usr/local/bin/sshm
  ./install.sh --symlink              # usa o sshm.sh deste repositorio
  ./install.sh --config ~/meu.json    # mantem o hosts.json em outro lugar
  ./install.sh --uninstall            # desfaz tudo, mantendo o hosts.json
EOF
}

# --- usuario dono da instalacao -------------------------------------------
# com sudo, $HOME e /root: a configuracao e o PATH devem continuar sendo do
# usuario que chamou o sudo.
TARGET_USER="$(id -un)"
TARGET_HOME="${HOME:-}"
if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    TARGET_USER="$SUDO_USER"
    if command -v getent >/dev/null 2>&1; then
        TARGET_HOME="$(getent passwd "$SUDO_USER" | cut -d: -f6)"
    fi
    [ -n "$TARGET_HOME" ] || TARGET_HOME="$HOME"
fi

# --- opcoes ----------------------------------------------------------------

PREFIX="$TARGET_HOME/.local"
BIN_DIR=""
BIN_DIR_SET=0
FROM="${SSHM_INSTALL_URL:-}"
CONFIG_OVERRIDE=""
DO_PATH=1
DO_CONFIG=1
WITH_DEPS=0
UNINSTALL=0
PURGE_CONFIG=0
FORCE=0
USE_SYMLINK=0

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix)
            [ $# -ge 2 ] || die "--prefix exige um diretorio"
            PREFIX="$2"
            shift 2
            ;;
        --prefix=*)
            PREFIX="${1#*=}"
            shift
            ;;
        --bin-dir)
            [ $# -ge 2 ] || die "--bin-dir exige um diretorio"
            BIN_DIR="$2"
            BIN_DIR_SET=1
            shift 2
            ;;
        --bin-dir=*)
            BIN_DIR="${1#*=}"
            BIN_DIR_SET=1
            shift
            ;;
        --system)
            PREFIX="/usr/local"
            shift
            ;;
        --symlink)
            USE_SYMLINK=1
            shift
            ;;
        --from)
            [ $# -ge 2 ] || die "--from exige um arquivo ou URL"
            FROM="$2"
            shift 2
            ;;
        --from=*)
            FROM="${1#*=}"
            shift
            ;;
        --config)
            [ $# -ge 2 ] || die "--config exige um arquivo"
            CONFIG_OVERRIDE="$2"
            shift 2
            ;;
        --config=*)
            CONFIG_OVERRIDE="${1#*=}"
            shift
            ;;
        --no-path)
            DO_PATH=0
            shift
            ;;
        --no-config)
            DO_CONFIG=0
            shift
            ;;
        --with-deps)
            WITH_DEPS=1
            shift
            ;;
        --uninstall)
            UNINSTALL=1
            shift
            ;;
        --purge-config)
            PURGE_CONFIG=1
            shift
            ;;
        -f|--force)
            FORCE=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "opcao desconhecida: $1 (use --help)"
            ;;
    esac
done

[ -n "$BIN_DIR" ] || BIN_DIR="$PREFIX/bin"

# diretorio temporario para o download e para os arquivos auxiliares
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sshm-install.XXXXXX")" || die "nao consegui criar um diretorio temporario"
trap 'rm -rf -- "$TMP_DIR"' EXIT

# --- utilitarios -----------------------------------------------------------

# 0 (verdadeiro) quando precisamos de sudo para escrever em $1
need_sudo() {
    local dir="$1"
    while [ ! -e "$dir" ] && [ "$dir" != "/" ]; do
        dir="$(dirname -- "$dir")"
    done
    [ ! -w "$dir" ]
}

# deixa o arquivo pertencendo ao usuario que chamou o install.sh
chown_target() {
    if [ "$(id -u)" -eq 0 ] && [ "$TARGET_USER" != "root" ]; then
        chown -- "$TARGET_USER" "$1" 2>/dev/null || true
    fi
}

absolute_path() {
    case "$1" in
        /*) printf '%s\n' "$1" ;;
        *) printf '%s\n' "$PWD/${1#./}" ;;
    esac
}

is_url() {
    case "$1" in
        http://*|https://*) return 0 ;;
        *) return 1 ;;
    esac
}

download() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$dest" -- "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" -- "$url"
    else
        return 1
    fi
}

# --- arquivos de inicializacao do shell ------------------------------------

RC_FILE=""
RC_KIND="posix"

rc_for_shell() {
    local sh="${SHELL:-}"
    sh="${sh##*/}"
    RC_KIND="posix"
    case "$sh" in
        fish)
            RC_KIND="fish"
            RC_FILE="$TARGET_HOME/.config/fish/config.fish"
            ;;
        zsh)
            RC_FILE="$TARGET_HOME/.zshrc"
            ;;
        bash)
            if [ -f "$TARGET_HOME/.bash_profile" ] && [ "$(uname -s 2>/dev/null)" = "Darwin" ]; then
                RC_FILE="$TARGET_HOME/.bash_profile"
            elif [ -f "$TARGET_HOME/.bashrc" ]; then
                RC_FILE="$TARGET_HOME/.bashrc"
            elif [ -f "$TARGET_HOME/.bash_profile" ]; then
                RC_FILE="$TARGET_HOME/.bash_profile"
            else
                RC_FILE="$TARGET_HOME/.bashrc"
            fi
            ;;
        *)
            RC_FILE="$TARGET_HOME/.profile"
            ;;
    esac
}

mark_begin() { printf '# >>> %s (%s) >>>' "$MARK_PREFIX" "$1"; }
mark_end() { printf '# <<< %s (%s) <<<' "$MARK_PREFIX" "$1"; }

# remove o bloco do instalador com o rotulo $2 do arquivo $1
strip_block() {
    local file="$1" id="$2" begin end tmp
    [ -f "$file" ] || return 0
    begin="$(mark_begin "$id")"
    end="$(mark_end "$id")"
    grep -Fq -- "$begin" "$file" || return 0
    tmp="$TMP_DIR/strip.$$"
    awk -v b="$begin" -v e="$end" '
        $0 == b { skip = 1; next }
        $0 == e { skip = 0; next }
        skip != 1 { print }
    ' "$file" > "$tmp" || return 0
    cat -- "$tmp" > "$file" 2>/dev/null || return 0
    chown_target "$file"
    return 0
}

# escreve (ou reescreve) o bloco do instalador com o rotulo $2 no arquivo $1
write_block() {
    local file="$1" id="$2" content="$3" dir
    dir="$(dirname -- "$file")"
    [ -d "$dir" ] || mkdir -p -- "$dir" 2>/dev/null || true
    if ! touch -- "$file" 2>/dev/null; then
        warn "nao consegui escrever em $file; adicione manualmente:"
        printf '%s\n' "$content" >&2
        return 0
    fi
    strip_block "$file" "$id"
    {
        mark_begin "$id"
        printf '\n'
        printf '%s\n' "$content"
        mark_end "$id"
        printf '\n'
    } >> "$file" 2>/dev/null || warn "nao consegui atualizar $file"
    chown_target "$file"
    return 0
}

# --- desinstalacao ---------------------------------------------------------

do_uninstall() {
    local dest="$BIN_DIR/$SCRIPT_NAME" sudo_cmd="" rc_file=""

    if [ ! -e "$dest" ] && [ ! -L "$dest" ] && [ "$BIN_DIR_SET" -eq 0 ]; then
        if found="$(command -v "$SCRIPT_NAME" 2>/dev/null)"; then
            dest="$found"
        fi
    fi

    if [ -e "$dest" ] || [ -L "$dest" ]; then
        if [ -d "$dest" ] && [ ! -L "$dest" ]; then
            die "$dest e um diretorio; nao vou remover."
        fi
        need_sudo "$dest" && sudo_cmd="sudo"
        if [ -n "$sudo_cmd" ]; then
            command -v sudo >/dev/null 2>&1 || die "sem permissao para remover $dest (use sudo)"
            sudo rm -f -- "$dest" || die "nao consegui remover $dest"
        else
            rm -f -- "$dest" || die "nao consegui remover $dest"
        fi
        info "removido $dest"
    else
        info "nada instalado em $dest"
    fi

    rc_for_shell
    rc_file="$RC_FILE"
    if [ -f "$rc_file" ]; then
        strip_block "$rc_file" path
        strip_block "$rc_file" config
        info "linhas do instalador removidas de $rc_file (se existiam)"
    fi

    if [ "$PURGE_CONFIG" -eq 1 ]; then
        local config_dir="$TARGET_HOME/.config/sshm"
        if [ -d "$config_dir" ]; then
            rm -rf -- "$config_dir" || warn "nao consegui apagar $config_dir"
            info "apagado $config_dir"
        fi
    else
        info "configuracao preservada em $TARGET_HOME/.config/sshm/hosts.json"
    fi

    echo
    echo "sshm desinstalado. Abra um novo terminal para o PATH ser recarregado."
}

# --- dependencias ----------------------------------------------------------

install_deps() {
    command -v jq >/dev/null 2>&1 && { info "jq ja esta instalado"; return 0; }
    local -a cmd=()
    if command -v apt-get >/dev/null 2>&1; then
        cmd=(apt-get install -y jq)
    elif command -v dnf >/dev/null 2>&1; then
        cmd=(dnf install -y jq)
    elif command -v yum >/dev/null 2>&1; then
        cmd=(yum install -y jq)
    elif command -v pacman >/dev/null 2>&1; then
        cmd=(pacman -S --noconfirm jq)
    elif command -v apk >/dev/null 2>&1; then
        cmd=(apk add jq)
    elif command -v brew >/dev/null 2>&1; then
        cmd=(brew install jq)
    else
        warn "nao sei instalar o jq neste sistema; instale manualmente."
        return 0
    fi
    if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
        cmd=(sudo "${cmd[@]}")
    fi
    info "instalando o jq (${cmd[*]})..."
    if "${cmd[@]}"; then
        info "jq instalado"
    else
        warn "falha ao instalar o jq; instale manualmente."
    fi
    return 0
}

# --- instalacao ------------------------------------------------------------

SRC_FILE=""
SELF_DIR=""

resolve_source() {
    local self_path="${BASH_SOURCE[0]:-}"

    if [ -n "$FROM" ]; then
        if is_url "$FROM"; then
            info "baixando o sshm.sh de $FROM"
            download "$FROM" "$TMP_DIR/sshm.sh" \
                || die "nao consegui baixar $FROM (curl/wget instalados?)"
            SRC_FILE="$TMP_DIR/sshm.sh"
            return 0
        fi
        [ -f "$FROM" ] || die "arquivo nao encontrado: $FROM"
        SRC_FILE="$(absolute_path "$FROM")"
        return 0
    fi

    if [ -f "$self_path" ]; then
        SELF_DIR="$(cd -- "$(dirname -- "$self_path")" && pwd)"
        if [ -f "$SELF_DIR/sshm.sh" ]; then
            SRC_FILE="$SELF_DIR/sshm.sh"
            return 0
        fi
    fi

    if [ -f "$PWD/sshm.sh" ]; then
        SRC_FILE="$PWD/sshm.sh"
        return 0
    fi

    info "nenhum sshm.sh local; baixando de $DEFAULT_URL"
    download "$DEFAULT_URL" "$TMP_DIR/sshm.sh" \
        || die "nao consegui baixar o sshm.sh. Baixe o repositorio e rode ./install.sh de dentro dele."
    SRC_FILE="$TMP_DIR/sshm.sh"
}

check_source() {
    head -n 1 -- "$SRC_FILE" | grep -q 'bash' \
        || die "$SRC_FILE nao parece um script bash."
    grep -q 'SSHM_CONFIG' -- "$SRC_FILE" \
        || die "$SRC_FILE nao parece ser o sshm.sh."
}

setup_config() {
    local config_file="$TARGET_HOME/.config/sshm/hosts.json" src="" candidate

    if [ -n "$CONFIG_OVERRIDE" ]; then
        config_file="$(absolute_path "$CONFIG_OVERRIDE")"
        if [ ! -f "$config_file" ]; then
            warn "$config_file ainda nao existe; o sshm vai cria-lo no primeiro uso."
        fi
        CONFIG_FILE_FINAL="$config_file"
        return 0
    fi

    if [ -f "$config_file" ]; then
        info "configuracao mantida em $config_file"
        CONFIG_FILE_FINAL="$config_file"
        return 0
    fi

    # com --symlink o sshm resolve o link e continua usando o hosts.json que
    # esta junto do sshm.sh: copiar outro para ~/.config so criaria confusao
    # (dois arquivos, um deles sempre ignorado).
    if [ "$USE_SYMLINK" -eq 1 ]; then
        local near="$(dirname -- "$SRC_FILE")/hosts.json"
        if [ -f "$near" ]; then
            info "configuracao mantida em $near (fica junto do sshm.sh)"
        else
            info "configuracao: $near (criada no primeiro uso, junto do sshm.sh)"
        fi
        CONFIG_FILE_FINAL="$near"
        return 0
    fi

    for candidate in "$SELF_DIR/hosts.json" "$PWD/hosts.json"; do
        [ -n "$candidate" ] || continue
        if [ -f "$candidate" ] && [ "$candidate" != "$config_file" ]; then
            src="$candidate"
            break
        fi
    done

    mkdir -p -m 700 -- "$(dirname -- "$config_file")" 2>/dev/null \
        || die "nao consegui criar $(dirname -- "$config_file")"
    if [ -n "$src" ]; then
        cp -f -- "$src" "$config_file" || die "nao consegui copiar $src para $config_file"
        info "hosts.json de $src copiado para $config_file (o original foi mantido)"
    else
        printf '{\n  "hosts": []\n}\n' > "$config_file" || die "nao consegui criar $config_file"
        info "criado $config_file (adicione conexoes com 'sshm create')"
    fi
    chmod 600 -- "$config_file" 2>/dev/null || true
    chown_target "$config_file"
    chown_target "$(dirname -- "$config_file")"
    CONFIG_FILE_FINAL="$config_file"
}

setup_path() {
    local dest="$BIN_DIR" content=""

    case ":$PATH:" in
        *":$dest:"*)
            info "$dest ja esta no PATH"
            return 0
            ;;
    esac

    if [ "$DO_PATH" -eq 0 ]; then
        warn "$dest nao esta no PATH; adicione manualmente (instalador rodou com --no-path)"
        return 0
    fi

    rc_for_shell
    case "$RC_KIND" in
        fish) content="set -gx PATH \"$dest\" \$PATH" ;;
        *) content="export PATH=\"$dest:\$PATH\"" ;;
    esac
    write_block "$RC_FILE" path "$content"
    info "PATH atualizado em $RC_FILE"
    info "abra um novo terminal (ou rode: source $RC_FILE) para usar 'sshm'"
}

setup_config_env() {
    [ -n "$CONFIG_OVERRIDE" ] || return 0
    [ "$DO_PATH" -eq 1 ] || return 0
    local content=""
    rc_for_shell
    case "$RC_KIND" in
        fish) content="set -gx SSHM_CONFIG \"$CONFIG_FILE_FINAL\"" ;;
        *) content="export SSHM_CONFIG=\"$CONFIG_FILE_FINAL\"" ;;
    esac
    write_block "$RC_FILE" config "$content"
    info "SSHM_CONFIG=$CONFIG_FILE_FINAL definido em $RC_FILE"
}

verify_install() {
    local out=""

    if ! command -v jq >/dev/null 2>&1; then
        warn "o jq nao esta instalado e o sshm nao funciona sem ele."
        warn "instale com 'sudo apt install jq' (ou rode: ./install.sh --with-deps)"
        return 0
    fi

    if out="$("$DEST" -h 2>&1 >/dev/null)"; then
        info "teste ok: '$DEST -h' respondeu como esperado"
    else
        warn "nao consegui executar '$DEST -h':"
        [ -n "$out" ] && printf '%s\n' "$out" >&2
    fi
    return 0
}

do_install() {
    local src_real="" sudo_cmd=""

    # um link aponta para um arquivo local: nao faz sentido baixar para /tmp
    if [ "$USE_SYMLINK" -eq 1 ] && is_url "$FROM"; then
        die "--symlink exige uma fonte local; nao combine com uma URL em --from"
    fi

    CONFIG_FILE_FINAL=""
    resolve_source
    check_source
    DEST="$BIN_DIR/$SCRIPT_NAME"

    info "fonte: $SRC_FILE"
    info "destino: $DEST"

    if [ "$USE_SYMLINK" -eq 1 ]; then
        case "$SRC_FILE" in
            "$TMP_DIR"/*)
                die "com --symlink a fonte precisa ser local; baixe o repositorio e rode ./install.sh de dentro dele."
                ;;
        esac
        src_real="$(cd -- "$(dirname -- "$SRC_FILE")" && pwd)/$(basename -- "$SRC_FILE")"
        [ -f "$src_real" ] || src_real="$SRC_FILE"
    fi

    need_sudo "$BIN_DIR" && sudo_cmd="sudo"
    if [ -n "$sudo_cmd" ]; then
        command -v sudo >/dev/null 2>&1 \
            || die "sem permissao de escrita em $BIN_DIR (use --prefix '$TARGET_HOME/.local' ou instale o sudo)"
    fi
    [ -d "$BIN_DIR" ] || $sudo_cmd mkdir -p -- "$BIN_DIR" \
        || die "nao consegui criar $BIN_DIR"

    if [ -e "$DEST" ] || [ -L "$DEST" ]; then
        if [ -d "$DEST" ] && [ ! -L "$DEST" ]; then
            die "$DEST e um diretorio; nao vou sobrescrever."
        fi
        if [ "$FORCE" -eq 0 ] && [ ! -L "$DEST" ] && ! grep -q 'SSHM_CONFIG' -- "$DEST" 2>/dev/null; then
            die "$DEST existe e nao parece ser o sshm (use --force para sobrescrever)"
        fi
    fi

    if [ "$USE_SYMLINK" -eq 1 ]; then
        $sudo_cmd ln -sf -- "$src_real" "$DEST"
        info "link criado: $DEST -> $src_real"
    else
        if [ -f "$DEST" ] && [ ! -L "$DEST" ] && cmp -s -- "$SRC_FILE" "$DEST" 2>/dev/null; then
            info "$DEST ja estava atualizado"
        else
            $sudo_cmd cp -f -- "$SRC_FILE" "$DEST" || die "nao consegui copiar para $DEST"
        fi
        $sudo_cmd chmod 0755 -- "$DEST" || die "nao consegui dar permissao de execucao a $DEST"
    fi

    # 'sudo ./install.sh' sem --system instala dentro do home do usuario:
    # o arquivo deve ficar com o dono certo, nao com o root.
    case "$DEST" in
        "$TARGET_HOME"/*) chown_target "$DEST" ;;
    esac

    if [ "$DO_CONFIG" -eq 1 ]; then
        setup_config
    else
        CONFIG_FILE_FINAL="$TARGET_HOME/.config/sshm/hosts.json"
    fi
    setup_path
    setup_config_env
    install_deps
    verify_install

    echo
    echo "sshm instalado em $DEST"
    echo "configuracao: $CONFIG_FILE_FINAL"
    echo
    echo "Use em qualquer pasta:"
    echo "  sshm           # selecao interativa"
    echo "  sshm list      # lista as conexoes"
    echo "  sshm --help    # ajuda completa"
}

# --- main ------------------------------------------------------------------

if [ "$UNINSTALL" -eq 1 ]; then
    do_uninstall
    exit 0
fi

do_install
