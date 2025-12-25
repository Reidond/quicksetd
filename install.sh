#!/usr/bin/env bash
# install.sh - Install ccdbind and ccdpin from GitHub releases
# SPDX-License-Identifier: MIT
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Reidond/ccdbind/main/install.sh | bash
#   VERSION=v1.0.0 bash install.sh
#
# This script is rootless and installs to ~/.local by default.

set -euo pipefail

readonly PROG_NAME="${0##*/}"
readonly REPO="Reidond/ccdbind"
readonly GITHUB_API="https://api.github.com"
readonly GITHUB_RELEASES="https://github.com/${REPO}/releases"

# Installation directories (XDG-compliant, rootless)
PREFIX="${PREFIX:-${HOME}/.local}"
BINDIR="${BINDIR:-${PREFIX}/bin}"
CONFIGDIR="${CONFIGDIR:-${XDG_CONFIG_HOME:-${HOME}/.config}/ccdbind}"
SYSTEMD_USER_DIR="${SYSTEMD_USER_DIR:-${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user}"

# Version to install (empty = latest)
VERSION="${VERSION:-}"

# Runtime options
DRY_RUN="${DRY_RUN:-0}"
SKIP_SERVICE="${SKIP_SERVICE:-0}"
FORCE="${FORCE:-0}"

# Temp directory for downloads
TMPDIR="${TMPDIR:-/tmp}"
WORK_DIR=""

# Colors (disabled if not a tty or NO_COLOR is set)
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

# Logging functions
info()  { printf '%s==>%s %s\n' "${BLUE}" "${NC}" "$*"; }
ok()    { printf '%s==>%s %s\n' "${GREEN}" "${NC}" "$*"; }
warn()  { printf '%s==> WARNING:%s %s\n' "${YELLOW}" "${NC}" "$*" >&2; }
die()   { printf '%s==> ERROR:%s %s\n' "${RED}" "${NC}" "$*" >&2; exit 1; }

usage() {
    cat <<EOF
${BOLD}Usage:${NC} ${PROG_NAME} [OPTIONS]

Install ccdbind and ccdpin binaries from GitHub releases.

${BOLD}Options:${NC}
    -h, --help          Show this help message
    -V, --version VER   Install specific version (default: latest)
    -n, --dry-run       Print actions without executing
    -f, --force         Overwrite existing files without prompting
    -S, --skip-service  Skip systemd service setup
    --prefix=PATH       Install prefix (default: ~/.local)
    --bindir=PATH       Binary directory (default: PREFIX/bin)
    --configdir=PATH    Config directory (default: ~/.config/ccdbind)

${BOLD}Environment:${NC}
    VERSION             Version to install (e.g., v1.0.0)
    PREFIX              Install prefix
    BINDIR              Binary directory
    CONFIGDIR           Config directory
    NO_COLOR            Disable colored output

${BOLD}Examples:${NC}
    ${PROG_NAME}                          # Install latest version
    ${PROG_NAME} -V v1.0.0                # Install specific version
    VERSION=v1.0.0 ${PROG_NAME}           # Install specific version (env)
    ${PROG_NAME} --prefix=/opt/ccdbind    # Custom install location
    ${PROG_NAME} --dry-run                # Preview installation
EOF
    exit 0
}

# Parse --key=value style arguments
parse_arg() {
    printf '%s' "${1#*=}"
}

# Check if a command exists
has_cmd() {
    command -v "$1" >/dev/null 2>&1
}

# Check required dependencies
check_deps() {
    local missing=()

    for cmd in curl tar; do
        has_cmd "$cmd" || missing+=("$cmd")
    done

    if [[ ${#missing[@]} -gt 0 ]]; then
        die "Missing required commands: ${missing[*]}"
    fi

    # Check for either wget or curl for downloads
    if ! has_cmd curl && ! has_cmd wget; then
        die "Either 'curl' or 'wget' is required for downloads"
    fi
}

# Detect system architecture
detect_arch() {
    local arch
    arch="$(uname -m)"

    case "${arch}" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              die "Unsupported architecture: ${arch}" ;;
    esac
}

# Detect operating system
detect_os() {
    local os
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"

    case "${os}" in
        linux)  echo "linux" ;;
        *)      die "Unsupported operating system: ${os}" ;;
    esac
}

# Fetch data from URL
fetch() {
    local url="$1"

    if has_cmd curl; then
        curl -fsSL "$url"
    elif has_cmd wget; then
        wget -qO- "$url"
    fi
}

# Download file to path
download() {
    local url="$1"
    local dest="$2"

    info "Downloading: ${url##*/}"

    if has_cmd curl; then
        curl -fsSL -o "$dest" "$url"
    elif has_cmd wget; then
        wget -q -O "$dest" "$url"
    fi
}

# Get latest release version from GitHub API
get_latest_version() {
    local api_url="${GITHUB_API}/repos/${REPO}/releases/latest"
    local response

    response="$(fetch "$api_url" 2>/dev/null)" || {
        die "Failed to fetch latest version from GitHub API"
    }

    # Extract tag_name using grep/sed (avoid jq dependency)
    printf '%s' "$response" | grep -oP '"tag_name":\s*"\K[^"]+' | head -1
}

# Verify checksum of downloaded file
verify_checksum() {
    local file="$1"
    local checksum_file="$2"

    if [[ ! -f "$checksum_file" ]]; then
        warn "Checksum file not found, skipping verification"
        return 0
    fi

    info "Verifying checksum..."

    local expected
    expected="$(cut -d' ' -f1 < "$checksum_file")"
    local actual
    actual="$(sha256sum "$file" | cut -d' ' -f1)"

    if [[ "$expected" != "$actual" ]]; then
        die "Checksum verification failed!
  Expected: ${expected}
  Got:      ${actual}"
    fi

    ok "Checksum verified"
}

# Run command or print if dry-run
run() {
    if [[ "$DRY_RUN" == "1" ]]; then
        printf '%s\n' "$*"
    else
        "$@"
    fi
}

# Create directory if it doesn't exist
ensure_dir() {
    if [[ ! -d "$1" ]]; then
        run mkdir -p "$1"
    fi
}

# Install file with mode
install_file() {
    local mode="$1"
    local src="$2"
    local dst="$3"

    ensure_dir "$(dirname "$dst")"

    if [[ -f "$dst" ]] && [[ "$FORCE" != "1" ]]; then
        if [[ "$DRY_RUN" != "1" ]]; then
            warn "File exists: ${dst} (use --force to overwrite)"
            return 0
        fi
    fi

    run install -m "$mode" "$src" "$dst"
}

# Cleanup temporary files
cleanup() {
    if [[ -n "${WORK_DIR:-}" ]] && [[ -d "$WORK_DIR" ]]; then
        rm -rf "$WORK_DIR"
    fi
}

main() {
    setup_colors

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)          usage ;;
            -V|--version)       VERSION="$2"; shift ;;
            -n|--dry-run)       DRY_RUN=1 ;;
            -f|--force)         FORCE=1 ;;
            -S|--skip-service)  SKIP_SERVICE=1 ;;
            --prefix=*)         PREFIX="$(parse_arg "$1")"; BINDIR="${PREFIX}/bin" ;;
            --bindir=*)         BINDIR="$(parse_arg "$1")" ;;
            --configdir=*)      CONFIGDIR="$(parse_arg "$1")" ;;
            -*)                 die "Unknown option: $1" ;;
            *)                  die "Unexpected argument: $1" ;;
        esac
        shift
    done

    # Validate environment
    check_deps

    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"

    # Determine version to install
    if [[ -z "$VERSION" ]]; then
        info "Fetching latest version..."
        VERSION="$(get_latest_version)"
    fi

    [[ -n "$VERSION" ]] || die "Could not determine version to install"

    # Ensure version starts with 'v'
    [[ "$VERSION" == v* ]] || VERSION="v${VERSION}"

    info "Installing ccdbind ${BOLD}${VERSION}${NC} (${os}/${arch})"
    info "  BINDIR:       ${BINDIR}"
    info "  CONFIGDIR:    ${CONFIGDIR}"
    info "  SYSTEMD_USER: ${SYSTEMD_USER_DIR}"
    echo

    # Setup cleanup trap
    trap cleanup EXIT

    # Create working directory
    WORK_DIR="$(mktemp -d "${TMPDIR}/ccdbind-install.XXXXXX")"

    # Download release archive
    local archive_name="ccdbind-${VERSION}-${os}-${arch}.tar.gz"
    local archive_url="${GITHUB_RELEASES}/download/${VERSION}/${archive_name}"
    local checksum_url="${archive_url}.sha256"

    download "$archive_url" "${WORK_DIR}/${archive_name}"
    download "$checksum_url" "${WORK_DIR}/${archive_name}.sha256" 2>/dev/null || true

    # Verify checksum
    verify_checksum "${WORK_DIR}/${archive_name}" "${WORK_DIR}/${archive_name}.sha256"

    # Extract archive
    info "Extracting archive..."
    tar -xzf "${WORK_DIR}/${archive_name}" -C "${WORK_DIR}"

    local extract_dir="${WORK_DIR}/ccdbind-${VERSION}-${os}-${arch}"
    [[ -d "$extract_dir" ]] || die "Extraction failed: expected directory ${extract_dir}"

    # Install binaries
    info "Installing binaries..."
    install_file 755 "${extract_dir}/ccdbind" "${BINDIR}/ccdbind"
    install_file 755 "${extract_dir}/ccdpin" "${BINDIR}/ccdpin"

    # Install systemd units
    info "Installing systemd user units..."
    install_file 644 "${extract_dir}/systemd/user/ccdbind.service" "${SYSTEMD_USER_DIR}/ccdbind.service"
    install_file 644 "${extract_dir}/systemd/user/game.slice" "${SYSTEMD_USER_DIR}/game.slice"

    # Install config if not exists
    if [[ ! -f "${CONFIGDIR}/config.toml" ]]; then
        info "Installing default configuration..."
        install_file 644 "${extract_dir}/config.example.toml" "${CONFIGDIR}/config.toml"
    else
        warn "Config file exists, not overwriting: ${CONFIGDIR}/config.toml"
    fi

    # Setup systemd service
    if [[ "$SKIP_SERVICE" != "1" ]] && [[ "$DRY_RUN" != "1" ]]; then
        if has_cmd systemctl; then
            info "Reloading systemd user daemon..."
            systemctl --user daemon-reload

            info "Enabling and starting ccdbind.service..."
            systemctl --user enable --now ccdbind.service
        else
            warn "systemctl not found, skipping service setup"
        fi
    elif [[ "$SKIP_SERVICE" == "1" ]]; then
        info "Skipping systemd service setup (--skip-service)"
    fi

    echo
    ok "Installation complete!"
    echo
    info "Verify with:"
    echo "    systemctl --user status ccdbind.service"
    echo "    ccdbind status"
    echo
    info "Configuration:"
    echo "    ${CONFIGDIR}/config.toml"
    echo

    # Check if BINDIR is in PATH
    if [[ ":${PATH}:" != *":${BINDIR}:"* ]]; then
        warn "BINDIR is not in PATH. Add this to your shell profile:"
        echo "    export PATH=\"\${PATH}:${BINDIR}\""
    fi
}

main "$@"
