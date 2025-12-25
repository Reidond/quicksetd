#!/usr/bin/env bash
# uninstall.sh - Uninstall ccdbind and ccdpin
# SPDX-License-Identifier: MIT

set -euo pipefail

readonly PROG_NAME="${0##*/}"

PREFIX="${PREFIX:-${HOME}/.local}"
BINDIR="${BINDIR:-${PREFIX}/bin}"
CONFIGDIR="${CONFIGDIR:-${XDG_CONFIG_HOME:-${HOME}/.config}/ccdbind}"
STATEDIR="${STATEDIR:-${XDG_STATE_HOME:-${HOME}/.local/state}/ccdbind}"
STATEDIR_PIN="${STATEDIR_PIN:-${XDG_STATE_HOME:-${HOME}/.local/state}/ccdpin}"
SYSTEMD_USER_DIR="${SYSTEMD_USER_DIR:-${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user}"

DRY_RUN="${DRY_RUN:-0}"
PURGE="${PURGE:-0}"
FORCE="${FORCE:-0}"

setup_colors() {
    if [[ -t 1 ]] && [[ -z "${NO_COLOR:-}" ]]; then
        readonly RED=$'\033[0;31m'
        readonly GREEN=$'\033[0;32m'
        readonly YELLOW=$'\033[0;33m'
        readonly BLUE=$'\033[0;34m'
        readonly BOLD=$'\033[1m'
        readonly NC=$'\033[0m'
    else
        readonly RED='' GREEN='' YELLOW='' BLUE='' BOLD='' NC=''
    fi
}

info()  { printf '%s==>%s %s\n' "${BLUE}" "${NC}" "$*"; }
ok()    { printf '%s==>%s %s\n' "${GREEN}" "${NC}" "$*"; }
warn()  { printf '%s==> WARNING:%s %s\n' "${YELLOW}" "${NC}" "$*" >&2; }
die()   { printf '%s==> ERROR:%s %s\n' "${RED}" "${NC}" "$*" >&2; exit 1; }

usage() {
    cat <<EOF
${BOLD}Usage:${NC} ${PROG_NAME} [OPTIONS]

Uninstall ccdbind and ccdpin binaries and systemd user units.

${BOLD}Options:${NC}
    -h, --help          Show this help message
    -n, --dry-run       Print actions without executing
    -p, --purge         Also remove configuration and state files
    -f, --force         Don't prompt for confirmation
    --prefix=PATH       Install prefix (default: ~/.local)
    --bindir=PATH       Binary directory (default: PREFIX/bin)
    --configdir=PATH    Config directory (default: ~/.config/ccdbind)
    --statedir=PATH     State directory (default: ~/.local/state/ccdbind)

${BOLD}Environment:${NC}
    PREFIX              Install prefix
    BINDIR              Binary directory
    CONFIGDIR           Config directory
    STATEDIR            State directory
    NO_COLOR            Disable colored output

${BOLD}Examples:${NC}
    ${PROG_NAME}                  # Uninstall, keep config
    ${PROG_NAME} --purge          # Uninstall and remove all data
    ${PROG_NAME} --dry-run        # Preview what would be removed
    ${PROG_NAME} -f --purge       # Force purge without confirmation
EOF
    exit 0
}

parse_arg() {
    printf '%s' "${1#*=}"
}

has_cmd() {
    command -v "$1" >/dev/null 2>&1
}

run() {
    if [[ "$DRY_RUN" == "1" ]]; then
        printf '%s\n' "$*"
    else
        "$@"
    fi
}

rm_file() {
    local file="$1"
    if [[ -f "$file" ]] || [[ -L "$file" ]]; then
        run rm -f "$file"
        return 0
    fi
    return 1
}

rm_dir_if_empty() {
    local dir="$1"
    if [[ -d "$dir" ]]; then
        if [[ -z "$(ls -A "$dir" 2>/dev/null)" ]]; then
            run rmdir "$dir"
        else
            warn "Directory not empty, not removing: ${dir}"
        fi
    fi
}

rm_dir() {
    local dir="$1"
    if [[ -d "$dir" ]]; then
        run rm -rf "$dir"
    fi
}

confirm() {
    local prompt="$1"
    if [[ "$FORCE" == "1" ]]; then
        return 0
    fi
    printf '%s [y/N] ' "$prompt"
    read -r answer
    case "$answer" in
        [Yy]|[Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

stop_service() {
    if [[ "$DRY_RUN" == "1" ]]; then
        echo "systemctl --user stop ccdbind.service"
        echo "systemctl --user disable ccdbind.service"
        return 0
    fi

    if ! has_cmd systemctl; then
        return 0
    fi

    if systemctl --user is-active --quiet ccdbind.service 2>/dev/null; then
        info "Stopping ccdbind.service..."
        systemctl --user stop ccdbind.service || true
    fi

    if systemctl --user is-enabled --quiet ccdbind.service 2>/dev/null; then
        info "Disabling ccdbind.service..."
        systemctl --user disable ccdbind.service || true
    fi
}

reload_systemd() {
    if [[ "$DRY_RUN" == "1" ]]; then
        echo "systemctl --user daemon-reload"
        return 0
    fi

    if has_cmd systemctl; then
        info "Reloading systemd user daemon..."
        systemctl --user daemon-reload || true
    fi
}

main() {
    setup_colors

    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)      usage ;;
            -n|--dry-run)   DRY_RUN=1 ;;
            -p|--purge)     PURGE=1 ;;
            -f|--force)     FORCE=1 ;;
            --prefix=*)     PREFIX="$(parse_arg "$1")"; BINDIR="${PREFIX}/bin" ;;
            --bindir=*)     BINDIR="$(parse_arg "$1")" ;;
            --configdir=*)  CONFIGDIR="$(parse_arg "$1")" ;;
            --statedir=*)   STATEDIR="$(parse_arg "$1")" ;;
            -*)             die "Unknown option: $1" ;;
            *)              die "Unexpected argument: $1" ;;
        esac
        shift
    done

    info "Uninstalling ccdbind and ccdpin"
    info "  BINDIR:       ${BINDIR}"
    info "  CONFIGDIR:    ${CONFIGDIR}"
    info "  STATEDIR:     ${STATEDIR}"
    info "  SYSTEMD_USER: ${SYSTEMD_USER_DIR}"

    if [[ "$PURGE" == "1" ]]; then
        warn "Purge mode enabled - config and state will be removed"
    fi

    if ! confirm "Proceed with uninstall?"; then
        info "Aborted."
        exit 0
    fi

    stop_service

    info "Removing systemd user units..."
    rm_file "${SYSTEMD_USER_DIR}/ccdbind.service" && info "  Removed ccdbind.service"
    rm_file "${SYSTEMD_USER_DIR}/game.slice" && info "  Removed game.slice"

    reload_systemd

    info "Removing binaries..."
    rm_file "${BINDIR}/ccdbind" && info "  Removed ccdbind"
    rm_file "${BINDIR}/ccdpin" && info "  Removed ccdpin"

    if [[ "$PURGE" == "1" ]]; then
        info "Removing configuration..."
        rm_dir "$CONFIGDIR" && info "  Removed ${CONFIGDIR}"

        info "Removing state..."
        rm_dir "$STATEDIR" && info "  Removed ${STATEDIR}"
        rm_dir "$STATEDIR_PIN" && info "  Removed ${STATEDIR_PIN}"
    else
        info "Config and state preserved (use --purge to remove)"
        info "  Config: ${CONFIGDIR}"
        info "  State:  ${STATEDIR}"
    fi

    echo
    ok "Uninstall complete!"
}

main "$@"
