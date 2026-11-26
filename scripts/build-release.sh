#!/usr/bin/env bash
# Compila y empaqueta los binarios multiplataforma de gnofy listos para subir
# al hosting estático de https://gnofy.xyz
#
# Uso:
#   ./scripts/build-release.sh v0.4.0
#
# Estructura generada en ./dist/ (lista para subir a la raíz de gnofy.xyz):
#   dist/
#   ├── install.sh
#   ├── install.ps1
#   └── releases/
#       ├── latest.txt
#       └── v0.4.0/
#           ├── gnofy_linux_amd64.tar.gz
#           ├── gnofy_linux_arm64.tar.gz
#           ├── gnofy_darwin_amd64.tar.gz
#           ├── gnofy_darwin_arm64.tar.gz
#           ├── gnofy_windows_amd64.zip
#           ├── gnofy_windows_arm64.zip
#           └── checksums.txt

set -euo pipefail

VERSION="${1:-v0.4.0}"
GO_BIN="${GO_BIN:-go}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
REL_DIR="${DIST_DIR}/releases/${VERSION}"

rm -rf "$REL_DIR"
mkdir -p "$REL_DIR"

cp "${ROOT_DIR}/scripts/install.sh" "${DIST_DIR}/install.sh"
cp "${ROOT_DIR}/scripts/install.ps1" "${DIST_DIR}/install.ps1"
printf '%s\n' "$VERSION" > "${DIST_DIR}/releases/latest.txt"

TARGETS=(
    "linux amd64 tar.gz"
    "linux arm64 tar.gz"
    "darwin amd64 tar.gz"
    "darwin arm64 tar.gz"
    "windows amd64 zip"
    "windows arm64 zip"
)

for entry in "${TARGETS[@]}"; do
    read -r GOOS GOARCH EXT <<< "$entry"
    BIN="gnofy"
    if [ "$GOOS" = "windows" ]; then
        BIN="gnofy.exe"
    fi

    STAGE="$(mktemp -d)"
    echo "[build-release] compilando ${GOOS}/${GOARCH} (${VERSION})..."
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
        "$GO_BIN" build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
        -o "${STAGE}/${BIN}" "${ROOT_DIR}/cmd/gnofy"

    ARCHIVE="gnofy_${GOOS}_${GOARCH}.${EXT}"
    if [ "$EXT" = "tar.gz" ]; then
        tar -C "$STAGE" -czf "${REL_DIR}/${ARCHIVE}" "$BIN"
    else
        if command -v zip >/dev/null 2>&1; then
            (cd "$STAGE" && zip -q "${REL_DIR}/${ARCHIVE}" "$BIN")
        else
            python3 -c "import zipfile, sys; z=zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED); z.write(sys.argv[2], sys.argv[3]); z.close()" \
                "${REL_DIR}/${ARCHIVE}" "${STAGE}/${BIN}" "$BIN"
        fi
    fi
    rm -rf "$STAGE"
done

(
    cd "$REL_DIR"
    sha256sum gnofy_*.tar.gz gnofy_*.zip > checksums.txt
)

echo "[build-release] listo en ${DIST_DIR}:"
ls -lh "$REL_DIR"
