<#
.SYNOPSIS
    Instala o sshm (gerenciador de conexoes SSH) no Windows.

.DESCRIPTION
    Instalador equivalente ao install.sh do Linux, sem sudo:

      1. obtem o sshm.exe (release do GitHub ou build local com -Source);
      2. copia para %LOCALAPPDATA%\Programs\sshm (ou -BinDir);
      3. confere o SHA256 do pacote baixado;
      4. coloca a pasta no PATH do usuario (sem mexer no PATH da maquina);
      5. garante um hosts.json em %APPDATA%\sshm\hosts.json;
      6. testa o comando instalado.

    Rodando de dentro do repositorio (com o Go instalado), o instalador compila
    daquele checkout em vez de exigir uma release publicada. Se o repositorio
    ainda nao tiver releases, o script explica as alternativas.

    Nada e feito as escondidas: o instalador diz o que escreveu e onde, e
    'install.ps1 -Uninstall' desfaz as alteracoes.

.PARAMETER Version
    Versao da release (ex.: 2.0.0) ou 'latest' (padrao).

.PARAMETER Source
    Em vez de baixar: uma pasta do repositorio (faz o build com o Go) ou um
    caminho para um sshm.exe ja compilado.

.PARAMETER BinDir
    Pasta de destino do sshm.exe (padrao: %LOCALAPPDATA%\Programs\sshm).

.PARAMETER Prefix
    Alternativa ao -BinDir: instala em <Prefix>\bin.

.PARAMETER Config
    Usa um hosts.json especifico, gravando SSHM_CONFIG no ambiente do usuario.

.PARAMETER NoPath
    Nao altera o PATH do usuario.

.PARAMETER NoConfig
    Nao cria nem mexe na configuracao.

.PARAMETER Force
    Sobrescreve o sshm.exe existente sem perguntar.

.PARAMETER Uninstall
    Remove o sshm.exe e a entrada do PATH criada pelo instalador.

.PARAMETER PurgeConfig
    Com -Uninstall, apaga tambem a configuracao (%APPDATA%\sshm e ~/.config/sshm).

.EXAMPLE
    irm https://raw.githubusercontent.com/szarblewski/ssh_manager/main/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Source . -BinDir "$env:TEMP\sshm"
.EXAMPLE
    .\install.ps1 -Uninstall -PurgeConfig
#>
[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$Source = '',
    [string]$BinDir = '',
    [string]$Prefix = '',
    [string]$Config = '',
    [string]$Repository = 'szarblewski/ssh_manager',
    [switch]$NoPath,
    [switch]$NoConfig,
    [switch]$Force,
    [switch]$Uninstall,
    [switch]$PurgeConfig,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# guarda se o usuario pediu uma versao especifica (para nao trocar por build local)
$VersionWasGiven = $PSBoundParameters.ContainsKey('Version')

# 'irm ... | iex' nao tem arquivo por tras: nesse caso nao podemos usar 'exit'
# (fecharia o terminal do usuario), entao abortamos com throw
$InvokedFromSession = [string]::IsNullOrEmpty($PSCommandPath)
$global:SSHMInstallStopped = ''

# TLS 1.2 e obrigatorio para o GitHub nas versoes antigas do PowerShell
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    Write-Verbose "Nao foi possivel ajustar o TLS: $_"
}

function Write-Info { param([string]$Message) Write-Host "sshm: $Message" }
function Write-Warn { param([string]$Message) Write-Warning "sshm: $Message" }
function Stop-Install {
    param([string]$Message)
    Write-Host "sshm: $Message" -ForegroundColor Red
    # escopo global: no modo 'irm | iex' o $script: nao e o mesmo do topo
    $global:SSHMInstallStopped = $Message
    if ($InvokedFromSession) {
        throw $Message
    }
    exit 1
}

# encerra o script devolvendo o codigo de saida (sem fechar a sessao no iex)
function Complete-Script {
    param([int]$Code = 0)
    if ($InvokedFromSession) { return }
    exit $Code
}

# codigo HTTP de um erro do Invoke-WebRequest/Invoke-RestMethod (0 se nao houver)
function Get-HttpStatusCode {
    param($ErrorRecord)
    $response = $ErrorRecord.Exception.Response
    if ($null -eq $response) { return 0 }
    try { return [int]$response.StatusCode } catch { return 0 }
}

function Show-Usage {
    @'
Uso: install.ps1 [opcoes]

  -Version N        versao da release (ex.: 2.0.0) ou 'latest' (padrao)
  -Source CAMINHO   pasta do repositorio (build com Go) ou sshm.exe local
                    (usado automaticamente quando o script esta no repositorio)
  -BinDir DIR       instala em DIR (padrao: %LOCALAPPDATA%\Programs\sshm)
  -Prefix DIR       instala em DIR\bin
  -Config ARQUIVO   usa ARQUIVO como hosts.json (grava SSHM_CONFIG)
  -NoPath           nao altera o PATH do usuario
  -NoConfig         nao cria/copia o hosts.json
  -Force            sobrescreve o sshm.exe existente
  -Uninstall        remove o sshm.exe e a entrada do PATH
  -PurgeConfig      com -Uninstall, apaga tambem a configuracao
  -Help             mostra esta ajuda

Exemplos:
  .\install.ps1
  .\install.ps1 -Source . -BinDir "$env:TEMP\sshm"
  .\install.ps1 -Version 2.0.0
  .\install.ps1 -Uninstall -PurgeConfig
'@ | Write-Host
}

function Resolve-Architecture {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        'ARM64' { return 'arm64' }
        default { return 'amd64' }
    }
}

function Get-DefaultBinDir {
    if ($Prefix) { return (Join-Path $Prefix 'bin') }
    return (Join-Path $env:LOCALAPPDATA 'Programs\sshm')
}

function Get-ConfigFile {
    if ($Config) { return $Config }
    return (Join-Path $env:APPDATA 'sshm\hosts.json')
}

# --- PATH do usuario (registro, sem setx: nao trunca em 1024 caracteres) ---

function Get-UserPath {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
    if (-not $key) { return '' }
    try {
        return [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    } finally {
        $key.Close()
    }
}

function Set-UserPath {
    param([string]$Value)
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if (-not $key) { Stop-Install 'nao consegui abrir HKCU\Environment' }
    try {
        $key.SetValue('Path', $Value, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    } finally {
        $key.Close()
    }
}

# adiciona a pasta ao PATH do usuario se ela ainda nao estiver la
function Add-ToUserPath {
    param([string]$Directory)
    $current = Get-UserPath
    foreach ($part in ($current -split ';')) {
        $trimmed = $part.Trim()
        if ($trimmed -and ($trimmed.TrimEnd('\') -ieq $Directory.TrimEnd('\'))) {
            Write-Info "$Directory ja esta no PATH do usuario"
            return
        }
    }
    $newValue = if ([string]::IsNullOrWhiteSpace($current)) { $Directory } else { "$current;$Directory" }
    Set-UserPath $newValue
    Write-Info "adicionei $Directory ao PATH do usuario (abra um terminal novo para valer)"
}

function Remove-FromUserPath {
    param([string]$Directory)
    $current = Get-UserPath
    if ([string]::IsNullOrWhiteSpace($current)) { return $false }
    $kept = @()
    $removed = $false
    foreach ($part in ($current -split ';')) {
        $trimmed = $part.Trim()
        if (-not $trimmed) { continue }
        if ($trimmed.TrimEnd('\') -ieq $Directory.TrimEnd('\')) {
            $removed = $true
            continue
        }
        $kept += $trimmed
    }
    if (-not $removed) { return $false }
    Set-UserPath ($kept -join ';')
    Write-Info "removi $Directory do PATH do usuario"
    return $true
}

function Set-UserEnvironmentVariable {
    param([string]$Name, [string]$Value)
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if (-not $key) { Stop-Install 'nao consegui abrir HKCU\Environment' }
    try {
        $key.SetValue($Name, $Value, [Microsoft.Win32.RegistryValueKind]::String)
    } finally {
        $key.Close()
    }
    Write-Info "defini a variavel de ambiente $Name = $Value"
}

function Remove-UserEnvironmentVariable {
    param([string]$Name)
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if (-not $key) { return }
    try {
        if ($null -ne $key.GetValue($Name)) {
            $key.DeleteValue($Name, $false)
            Write-Info "removi a variavel de ambiente $Name"
        }
    } finally {
        $key.Close()
    }
}

# --- obtencao do binario ---

function Get-ArchiveName {
    param([string]$ResolvedVersion, [string]$Arch)
    return "sshm_${ResolvedVersion}_windows_${Arch}.zip"
}

# le tag/draft/prerelease de uma release sem quebrar no StrictMode quando o
# campo nao existe (a API pode devolver lista vazia)
function Get-ReleaseInfo {
    param($Release)
    $tag = ''
    $draft = $false
    $prerelease = $false
    if ($null -ne $Release) {
        if ($Release.PSObject.Properties['tag_name']) { $tag = [string]$Release.tag_name }
        if ($Release.PSObject.Properties['draft']) { $draft = [bool]$Release.draft }
        if ($Release.PSObject.Properties['prerelease']) { $prerelease = [bool]$Release.prerelease }
    }
    return [pscustomobject]@{ Tag = $tag; Draft = $draft; Prerelease = $prerelease }
}

function Resolve-LatestVersion {
    $headers = @{ 'User-Agent' = 'sshm-install' }
    $api = "https://api.github.com/repos/$Repository"

    try {
        $release = Invoke-RestMethod -Uri "$api/releases/latest" -Headers $headers
        $info = Get-ReleaseInfo $release
        if ($info.Tag) { return $info.Tag.TrimStart('v') }
    } catch {
        $status = Get-HttpStatusCode $_
        if ($status -ne 404) {
            Stop-Install "nao consegui consultar as releases de $Repository ($($_.Exception.Message))"
        }
    }

    # sem release "latest" (repositorio novo ou so com pre-releases): olha a lista
    $all = @()
    try {
        $response = Invoke-RestMethod -Uri "$api/releases" -Headers $headers
        if ($null -ne $response) { $all = @($response) }
    } catch {
        $all = @()
    }

    $info = @()
    foreach ($release in $all) { $info += Get-ReleaseInfo $release }

    foreach ($entry in $info) {
        if ($entry.Tag -and -not $entry.Draft -and -not $entry.Prerelease) {
            return $entry.Tag.TrimStart('v')
        }
    }
    foreach ($entry in $info) {
        if ($entry.Tag) { return $entry.Tag.TrimStart('v') }
    }

    Show-NoReleaseHelp
    Stop-Install 'nenhuma release disponivel'
}

# explica as alternativas quando o repositorio ainda nao tem release publicada
function Show-NoReleaseHelp {
    Write-Host "sshm: nao ha releases publicadas em $Repository." -ForegroundColor Yellow
    Write-Host '  Alternativas:'
    Write-Host '    .\install.ps1 -Source .        compila do repositorio local (precisa do Go instalado)'
    Write-Host '    .\install.ps1 -Version 2.0.0   instala uma release especifica'
    Write-Host '  Para publicar a primeira release (o workflow cuida do pacote):'
    Write-Host '    git tag v2.0.0'
    Write-Host '    git push origin v2.0.0'
}

# localiza o go: primeiro no PATH, depois nos lugares onde o instalador do Go
# (ou o pacote .zip) costuma deixar o toolchain
function Resolve-GoCommand {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }

    $candidates = @()
    if ($env:LOCALAPPDATA) {
        $programs = Join-Path $env:LOCALAPPDATA 'Programs'
        if (Test-Path $programs) {
            $candidates += Get-ChildItem -Path $programs -Directory -ErrorAction SilentlyContinue |
                Where-Object { $_.Name -like 'go*' } |
                ForEach-Object { Join-Path $_.FullName 'go\bin\go.exe' }
        }
    }
    $candidates += 'C:\Program Files\Go\bin\go.exe'
    $candidates += 'C:\Go\bin\go.exe'
    if ($env:USERPROFILE) {
        $candidates += (Join-Path $env:USERPROFILE 'scoop\apps\go\current\bin\go.exe')
        $candidates += (Join-Path $env:USERPROFILE 'go\bin\go.exe')
    }
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path $candidate)) { return $candidate }
    }
    return ''
}

function Install-FromSource {
    param([string]$Path, [string]$Destination)
    if (Test-Path -PathType Container $Path) {
        $go = Resolve-GoCommand
        if (-not $go) {
            Stop-Install "para compilar de $Path instale o Go (https://go.dev/dl) ou passe um sshm.exe pronto em -Source"
        }
        if (-not (Test-Path (Join-Path $Path 'cmd\sshm'))) {
            Stop-Install "$Path nao parece ser o repositorio do sshm (faltou cmd\sshm)"
        }
        Write-Info "compilando de $Path com $go ..."
        Push-Location $Path
        try {
            & $go build -o $Destination ./cmd/sshm
            if ($LASTEXITCODE -ne 0) { Stop-Install "o build falhou (codigo $LASTEXITCODE)" }
        } finally {
            Pop-Location
        }
        return
    }

    if (Test-Path -PathType Leaf $Path) {
        Write-Info "copiando $Path"
        Copy-Item -Path $Path -Destination $Destination -Force
        return
    }
    Stop-Install "origem invalida: $Path"
}

function Install-FromRelease {
    param([string]$ResolvedVersion, [string]$Arch, [string]$Destination)

    $archive = Get-ArchiveName -ResolvedVersion $ResolvedVersion -Arch $Arch
    $base = "https://github.com/$Repository/releases/download/v$ResolvedVersion"
    $zipPath = Join-Path ([IO.Path]::GetTempPath()) $archive
    $sumsPath = Join-Path ([IO.Path]::GetTempPath()) "sshm-$ResolvedVersion-checksums.txt"

    Write-Info "baixando $archive ..."
    try {
        Invoke-WebRequest -Uri "$base/$archive" -OutFile $zipPath -UseBasicParsing
    } catch {
        if ((Get-HttpStatusCode $_) -eq 404) {
            Stop-Install "nao encontrei $archive na release v$ResolvedVersion (veja https://github.com/$Repository/releases)"
        }
        Stop-Install "falha ao baixar ${archive}: $($_.Exception.Message)"
    }

    # confere o SHA256 quando o checksums.txt estiver disponivel
    try {
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $sumsPath -UseBasicParsing
        $expected = (Get-Content $sumsPath | Where-Object { $_ -match [regex]::Escape($archive) } | Select-Object -First 1)
        if ($expected) {
            $expectedHash = ($expected -split '\s+')[0].ToLower()
            $actualHash = (Get-FileHash -Path $zipPath -Algorithm SHA256).Hash.ToLower()
            if ($expectedHash -ne $actualHash) {
                Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
                Stop-Install "o SHA256 de $archive nao confere (esperado $expectedHash, obtido $actualHash)"
            }
            Write-Info "SHA256 conferido"
        } else {
            Write-Warn "checksums.txt nao lista $archive; seguindo sem conferir"
        }
    } catch {
        Write-Warn "nao consegui baixar o checksums.txt ($_); seguindo sem conferir"
    } finally {
        Remove-Item $sumsPath -Force -ErrorAction SilentlyContinue
    }

    $extractDir = Join-Path ([IO.Path]::GetTempPath()) "sshm-extract-$([guid]::NewGuid().ToString('N'))"
    New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
    try {
        Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force
        $exe = Get-ChildItem -Path $extractDir -Filter 'sshm.exe' -Recurse | Select-Object -First 1
        if (-not $exe) { Stop-Install "nao encontrei sshm.exe dentro de $archive" }
        Copy-Item -Path $exe.FullName -Destination $Destination -Force
    } finally {
        Remove-Item $extractDir -Recurse -Force -ErrorAction SilentlyContinue
        Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
    }
}

function Initialize-Config {
    param([string]$ConfigFile)

    # nunca criar uma configuracao nova por cima de uma que ja existe em outro
    # lugar (ex.: ~/.config/sshm/hosts.json vindo do Linux), senao o arquivo
    # vazio passaria a ter prioridade e as conexoes sumiriam
    $existing = @()
    if ($env:SSHM_CONFIG) { $existing += $env:SSHM_CONFIG }
    $existing += (Join-Path $env:APPDATA 'sshm\hosts.json')
    $existing += (Join-Path $env:USERPROFILE '.config\sshm\hosts.json')
    foreach ($candidate in $existing) {
        if ($candidate -and (Test-Path $candidate)) {
            Write-Info "configuracao existente mantida: $candidate"
            return
        }
    }

    $dir = Split-Path -Parent $ConfigFile
    if ($dir -and -not (Test-Path $dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
    # UTF-8 SEM BOM: o 'Set-Content -Encoding UTF8' do PowerShell 5.1 grava BOM
    # e o JSON fica invalido para o sshm (e para a maioria dos parsers)
    $template = "{`n  `"hosts`": []`n}`n"
    [IO.File]::WriteAllText($ConfigFile, $template, (New-Object System.Text.UTF8Encoding($false)))
    Write-Info "configuracao criada em $ConfigFile (adicione conexoes com 'sshm create')"
}

# --- desinstalacao ---

function Invoke-Uninstall {
    $dir = $BinDir
    Write-Info "desinstalando de $dir"

    if (-not $NoPath) {
        Remove-FromUserPath $dir | Out-Null
    }

    $exe = Join-Path $dir 'sshm.exe'
    if (Test-Path $exe) {
        Remove-Item $exe -Force
        Write-Info "removi $exe"
    } else {
        Write-Warn "nao encontrei $exe"
    }

    if (Test-Path $dir) {
        $remaining = Get-ChildItem -Path $dir -Force -ErrorAction SilentlyContinue
        if (-not $remaining) {
            Remove-Item $dir -Force
            Write-Info "removi a pasta $dir"
        }
    }

    if ($PurgeConfig) {
        Remove-UserEnvironmentVariable 'SSHM_CONFIG'
        $targets = @((Join-Path $env:APPDATA 'sshm'), (Join-Path $env:USERPROFILE '.config\sshm'))
        foreach ($target in $targets) {
            if (Test-Path $target) {
                Remove-Item $target -Recurse -Force
                Write-Info "configuracao apagada: $target"
            }
        }
    } else {
        Write-Info "a configuracao foi mantida (use -PurgeConfig para apagar)"
    }
}

# --- instalacao ---

function Invoke-Install {
    if (-not $BinDir) { $BinDir = Get-DefaultBinDir }
    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    }
    Write-Info "destino: $BinDir"

    # rodando de dentro do repositorio: compila daqui em vez de exigir release
    if (-not $Source -and -not $VersionWasGiven -and $PSScriptRoot) {
        if (Test-Path (Join-Path $PSScriptRoot 'cmd\sshm')) {
            Write-Info "repositorio local detectado em $PSScriptRoot; compilando daqui (use -Version para baixar uma release)"
            $Source = $PSScriptRoot
        }
    }

    $target = Join-Path $BinDir 'sshm.exe'
    if ((Test-Path $target) -and -not $Force) {
        $running = $false
        try {
            $running = @(Get-Process sshm -ErrorAction SilentlyContinue).Count -gt 0
        } catch {
            $running = $false
        }
        if ($running) { Stop-Install 'o sshm esta em execucao; feche-o e tente de novo' }
        Write-Info "sobrescrevendo $target"
    }

    if ($Source) {
        Install-FromSource -Path $Source -Destination $target
    } else {
        $arch = Resolve-Architecture
        # aceita 'latest', '2.0.0' e 'v2.0.0' (o 'v' e do prefixo da tag)
        $resolved = $Version.TrimStart('v')
        if ($resolved -ieq 'latest') { $resolved = Resolve-LatestVersion }
        Install-FromRelease -ResolvedVersion $resolved -Arch $arch -Destination $target
    }
    Write-Info "instalado: $target"

    if (-not $NoPath) {
        Add-ToUserPath $BinDir
    }

    if (-not $NoConfig) {
        $configFile = Get-ConfigFile
        Initialize-Config -ConfigFile $configFile
        if ($Config) {
            Set-UserEnvironmentVariable -Name 'SSHM_CONFIG' -Value $Config
        }
    }

    # o comando funciona ja nesta sessao?
    $env:Path = "$BinDir;$env:Path"
    Write-Info "testando o comando..."
    & $target version

    Write-Info "pronto. Comece com: sshm create   (ou sshm --help)"
    if (-not $NoPath) {
        Write-Info "em um terminal novo o comando aparece sem ajustar nada"
    }
}

if ($Help) {
    Show-Usage
    Complete-Script 0
    return
}

if ($Uninstall) {
    if (-not $BinDir) { $BinDir = Get-DefaultBinDir }
    Invoke-Uninstall
    Complete-Script 0
    return
}

try {
    Invoke-Install
    Complete-Script 0
} catch {
    if ($global:SSHMInstallStopped) {
        # a causa ja foi explicada por Stop-Install
        Complete-Script 1
    } else {
        Write-Host "sshm: a instalacao falhou: $($_.Exception.Message)" -ForegroundColor Red
        Complete-Script 1
    }
}
