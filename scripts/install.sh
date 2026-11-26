#!/bin/sh
# Instalador oficial de gnofy (https://gnofy.xyz) para Linux y macOS.
# Uso:
#   curl -sSfL https://gnofy.xyz/install.sh | sh
#
# Variables opcionales:
#   GNOFY_VERSION      Versión a instalar (por defecto: "latest", ej. "v0.4.0")
#   GNOFY_BASE_URL     URL base de releases (por defecto: "https://gnofy.xyz/releases")
#   GNOFY_INSTALL_DIR  Directorio de destino (por defecto: "$HOME/.gnofy/bin")

set -eu

BASE_URL="${GNOFY_BASE_URL:-https://gnofy.xyz/releases}"
VERSION="${GNOFY_VERSION:-latest}"
INSTALL_DIR="${GNOFY_INSTALL_DIR:-$HOME/.gnofy/bin}"
BIN_NAME="gnofy"

say() {
    printf '[gnofy-install] %s\n' "$1"
}

fail() {
    printf '[gnofy-install] ERROR: %s\n' "$1" >&2
    exit 1
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || fail "se requiere '$1' para continuar"
}

download() {
    url="$1"
    dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 "$url" -o "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" "$url"
    else
        fail "no se encontró 'curl' ni 'wget' en el PATH"
    fi
}

detect_os() {
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        linux*)  printf 'linux' ;;
        darwin*) printf 'darwin' ;;
        *)       fail "sistema operativo no soportado por este script: $os (en Windows usa https://gnofy.xyz/install.ps1)" ;;
    esac
}

detect_arch() {
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)  printf 'amd64' ;;
        arm64|aarch64) printf 'arm64' ;;
        *)             fail "arquitectura no soportada: $arch" ;;
    esac
}

verify_sha256() {
    archive_file="$1"
    archive_name="$2"
    checksums_file="$3"

    expected="$(awk -v f="$archive_name" '$2 == f || $2 == "*"f { print $1; exit }' "$checksums_file")"
    if [ -z "$expected" ]; then
        fail "no se encontró checksum SHA-256 para $archive_name en checksums.txt"
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$archive_file" | awk '{ print $1 }')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$archive_file" | awk '{ print $1 }')"
    else
        say "aviso: ni sha256sum ni shasum disponibles; omitiendo verificación SHA-256"
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        fail "checksum SHA-256 inválido para $archive_name (esperado: $expected, obtenido: $actual)"
    fi
    say "sha256 verificado ($archive_name)"
}

main() {
    need_cmd tar

    OS="$(detect_os)"
    ARCH="$(detect_arch)"

    TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t gnofy-install)"
    trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

    if [ "$VERSION" = "latest" ]; then
        say "consultando última versión en ${BASE_URL}/latest.txt"
        download "${BASE_URL}/latest.txt" "${TMP_DIR}/latest.txt"
        VERSION="$(tr -d ' \t\r\n' < "${TMP_DIR}/latest.txt")"
        [ -n "$VERSION" ] || fail "latest.txt vacío"
    fi

    ARCHIVE="gnofy_${OS}_${ARCH}.tar.gz"
    ARCHIVE_URL="${BASE_URL}/${VERSION}/${ARCHIVE}"
    CHECKSUMS_URL="${BASE_URL}/${VERSION}/checksums.txt"

    say "instalando gnofy ${VERSION} (${OS}/${ARCH}) en ${INSTALL_DIR}"
    say "descargando ${ARCHIVE_URL}"
    download "$ARCHIVE_URL" "${TMP_DIR}/${ARCHIVE}"

    say "descargando checksums.txt"
    download "$CHECKSUMS_URL" "${TMP_DIR}/checksums.txt"
    verify_sha256 "${TMP_DIR}/${ARCHIVE}" "$ARCHIVE" "${TMP_DIR}/checksums.txt"

    mkdir -p "$INSTALL_DIR"
    tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}"

    [ -f "${TMP_DIR}/${BIN_NAME}" ] || fail "el archivo descargado no contiene el binario '${BIN_NAME}'"
    mv "${TMP_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
    chmod +x "${INSTALL_DIR}/${BIN_NAME}"

    say "instalado en ${INSTALL_DIR}/${BIN_NAME}"
    if "${INSTALL_DIR}/${BIN_NAME}" version >/dev/null 2>&1; then
        "${INSTALL_DIR}/${BIN_NAME}" version
    fi

    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            printf '\nAñade gnofy a tu PATH:\n'
            printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
            ;;
    esac

    printf '\nPrimeros pasos:\n'
    printf '  gnofy analyze ./contracts                  Auditar migrabilidad Solidity -> Gno\n'
    printf '  gnofy convert --idiomatic ./Token.sol      Transpilar a Gno idiomático (GRC-20 / Render)\n'
    printf '  gnofy verify ./out                         Validar con gno test\n'
    printf '  https://gnofy.xyz\n\n'
}

main "$@"
