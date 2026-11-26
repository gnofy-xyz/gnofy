# Instalador oficial de gnofy (https://gnofy.xyz) para Windows (PowerShell 5.1 / 7+).
# Uso:
#   irm https://gnofy.xyz/install.ps1 | iex
#
# Variables de entorno opcionales:
#   $env:GNOFY_VERSION      Versión a instalar (por defecto: "latest", ej. "v0.4.0")
#   $env:GNOFY_BASE_URL     URL base de releases (por defecto: "https://gnofy.xyz/releases")
#   $env:GNOFY_INSTALL_DIR  Directorio de destino (por defecto: "$HOME\.gnofy\bin")

$ErrorActionPreference = "Stop"

$BaseUrl    = if ($env:GNOFY_BASE_URL)    { $env:GNOFY_BASE_URL.TrimEnd("/") } else { "https://gnofy.xyz/releases" }
$Version    = if ($env:GNOFY_VERSION)     { $env:GNOFY_VERSION }               else { "latest" }
$InstallDir = if ($env:GNOFY_INSTALL_DIR) { $env:GNOFY_INSTALL_DIR }           else { Join-Path $HOME ".gnofy\bin" }
$BinName    = "gnofy.exe"

function Write-Info([string]$Message) {
    Write-Host "[gnofy-install] $Message"
}

function Get-Architecture {
    try {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    } catch {
        $arch = $env:PROCESSOR_ARCHITECTURE.ToLowerInvariant()
    }
    switch ($arch) {
        "x64"   { return "amd64" }
        "amd64" { return "amd64" }
        "arm64" { return "arm64" }
        default { throw "Arquitectura Windows no soportada: $arch" }
    }
}

function Verify-Sha256([string]$ArchivePath, [string]$ArchiveName, [string]$ChecksumsPath) {
    $expected = $null
    foreach ($line in Get-Content -Path $ChecksumsPath) {
        $trimmed = $line.Trim()
        if (-not $trimmed) { continue }
        $parts = $trimmed -split "\s+"
        if ($parts.Length -ge 2) {
            $file = $parts[1].TrimStart("*")
            if ($file -eq $ArchiveName) {
                $expected = $parts[0].ToLowerInvariant()
                break
            }
        }
    }
    if (-not $expected) {
        throw "No se encontró checksum SHA-256 para $ArchiveName en checksums.txt"
    }

    $actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum SHA-256 inválido para $ArchiveName (esperado: $expected, obtenido: $actual)"
    }
    Write-Info "sha256 verificado ($ArchiveName)"
}

function Add-ToUserPath([string]$Dir) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @()
    if ($userPath) {
        $entries = $userPath -split ";" | Where-Object { $_ -ne "" }
    }
    if ($entries -notcontains $Dir) {
        $newPath = ($entries + $Dir) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        Write-Info "añadido $Dir al PATH del usuario"
    }
    if (($env:Path -split ";") -notcontains $Dir) {
        $env:Path = "$Dir;$env:Path"
    }
}

$Arch = Get-Architecture
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("gnofy-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    if ($Version -eq "latest") {
        Write-Info "consultando última versión en $BaseUrl/latest.txt"
        $Version = (Invoke-RestMethod -Uri "$BaseUrl/latest.txt" -UseBasicParsing).ToString().Trim()
        if (-not $Version) {
            throw "latest.txt vacío"
        }
    }

    $ArchiveName  = "gnofy_windows_${Arch}.zip"
    $ArchiveUrl   = "$BaseUrl/$Version/$ArchiveName"
    $ChecksumsUrl = "$BaseUrl/$Version/checksums.txt"
    $ArchivePath  = Join-Path $TmpDir $ArchiveName
    $ChecksumsPath = Join-Path $TmpDir "checksums.txt"

    Write-Info "instalando gnofy $Version (windows/$Arch) en $InstallDir"
    Write-Info "descargando $ArchiveUrl"
    Invoke-WebRequest -Uri $ArchiveUrl -OutFile $ArchivePath -UseBasicParsing

    Write-Info "descargando checksums.txt"
    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $ChecksumsPath -UseBasicParsing
    Verify-Sha256 -ArchivePath $ArchivePath -ArchiveName $ArchiveName -ChecksumsPath $ChecksumsPath

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Expand-Archive -Path $ArchivePath -DestinationPath $TmpDir -Force

    $ExtractedBin = Join-Path $TmpDir $BinName
    if (-not (Test-Path $ExtractedBin)) {
        throw "El archivo descargado no contiene '$BinName'"
    }

    $TargetBin = Join-Path $InstallDir $BinName
    Move-Item -Path $ExtractedBin -Destination $TargetBin -Force
    Add-ToUserPath -Dir $InstallDir

    Write-Info "instalado en $TargetBin"
    & $TargetBin version

    Write-Host ""
    Write-Host "Primeros pasos (reinicia tu terminal si PATH no se actualiza automáticamente):"
    Write-Host "  gnofy analyze .\contracts                  Auditar migrabilidad Solidity -> Gno"
    Write-Host "  gnofy convert --idiomatic .\Token.sol      Transpilar a Gno idiomático (GRC-20 / Render)"
    Write-Host "  gnofy verify .\out                         Validar con gno test"
    Write-Host "  https://gnofy.xyz"
    Write-Host ""
} finally {
    if (Test-Path $TmpDir) {
        Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
