#!/usr/bin/env bash
set -euo pipefail

REPO="pockyHM/conan"
CONAN_HOME="${CONAN_HOME:-$HOME/.conan}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

info()  { printf "${CYAN}➜${RESET} %s\n" "$*"; }
ok()    { printf "${GREEN}✔${RESET} %s\n" "$*"; }
warn()  { printf "${YELLOW}!${RESET} %s\n" "$*"; }
die()   { printf "${RED}✖${RESET} %s\n" "$*" >&2; exit 1; }

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       die "Unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             die "Unsupported architecture: $(uname -m)" ;;
    esac
}

get_latest_tag() {
    if command -v git &>/dev/null && git ls-remote --tags "https://github.com/${REPO}.git" &>/dev/null 2>&1; then
        git ls-remote --tags "https://github.com/${REPO}.git" \
            | sed 's|.*/v||; s|\^{}||' \
            | sort -t. -k1,1n -k2,2n -k3,3n \
            | tail -1
    else
        curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
            | grep '"tag_name"' \
            | sed 's/.*"v\([^"]*\)".*/\1/'
    fi
}

download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fSL -o "$dest" "$url"
    elif command -v wget &>/dev/null; then
        wget -q -O "$dest" "$url"
    else
        die "curl or wget is required"
    fi
}

# --- Main ---

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    info "Detecting latest version..."
    VERSION=$(get_latest_tag)
    [ -z "$VERSION" ] && die "Could not determine latest version"
fi

OS=$(detect_os)
ARCH=$(detect_arch)

printf "\n${BOLD}Conan Installer${RESET}\n"
printf "  Version : v%s\n" "$VERSION"
printf "  OS/Arch : %s/%s\n" "$OS" "$ARCH"
printf "  Conan   : /usr/local/bin/conan\n"

if [ "$OS" = "linux" ]; then
    printf "  Agent   : %s/agent/%s/conan-agent\n" "$CONAN_HOME" "$ARCH"
fi
printf "\n"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"

info "Downloading conan v${VERSION}..."
download "${BASE_URL}/conan-${OS}-${ARCH}" "${TMPDIR}/conan"
chmod +x "${TMPDIR}/conan"
ok "conan downloaded"

info "Installing conan to /usr/local/bin..."
if [ -w /usr/local/bin ]; then
    cp "${TMPDIR}/conan" /usr/local/bin/conan
else
    sudo cp "${TMPDIR}/conan" /usr/local/bin/conan
fi
ok "conan installed"

CONAN_VERSION=$(/usr/local/bin/conan --version 2>&1 || true)
printf "  %s\n" "$CONAN_VERSION"

# Agent is Linux-only
if [ "$OS" = "linux" ]; then
    AGENT_DIR="${CONAN_HOME}/agent/${ARCH}"
    info "Downloading conan-agent v${VERSION}..."
    download "${BASE_URL}/conan-agent-${OS}-${ARCH}" "${TMPDIR}/conan-agent"
    chmod +x "${TMPDIR}/conan-agent"

    mkdir -p "$AGENT_DIR"
    cp "${TMPDIR}/conan-agent" "${AGENT_DIR}/conan-agent"
    ok "conan-agent installed to ${AGENT_DIR}/conan-agent"
fi

mkdir -p "$CONAN_HOME"

printf "\n"
info "Launching model configuration..."
exec /usr/local/bin/conan model add
