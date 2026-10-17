# Выпуск новой версии NetPilot в GitHub Releases.
#   .\release.ps1 -Version 0.1.1 -Notes "Что изменилось"
#   .\release.ps1 -Version 0.1.1 -NotesFile notes.md
#   .\release.ps1 -Version 0.1.1 -DryRun    — только собрать и подготовить файлы
#   .\release.ps1 -Version 0.1.1 -KeyFile D:\keys\release.key — другой закрытый ключ
#
# Собирает все платформы, кладёт в dist\release сборки с именами
# netpilot-<система>-<архитектура>, SHA256SUMS и его подпись SHA256SUMS.sig
# (ключ ed25519, по умолчанию %USERPROFILE%\.netpilot\release.key; открытый
# ключ release.pub из корня репозитория встраивается в сборки), публикует релиз v<версия>
# через GitHub CLI (gh). Устройства увидят обновление при следующей проверке,
# а установится оно только после нажатия «Обновить» в интерфейсе.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Notes = "",
    [string]$NotesFile = "",
    [string]$Repo = "iKriu/netpilot",
    [switch]$DryRun,
    [switch]$SkipM3, # не пересобирать интерфейс Material 3 (взять готовый из web\static\m3)
    [string]$KeyFile = (Join-Path $env:USERPROFILE ".netpilot\release.key")
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$Version = $Version.TrimStart("v")
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "Версия указывается как 0.1.1" }

# Ключи проверяем до долгой сборки. Релиз без подписи или сборки без
# встроенного ключа не выпускаем: первые не установятся, вторые ставили бы что угодно.
$PubFile = Join-Path $Root "release.pub"
$Sign = Test-Path $KeyFile
$howTo = "Создайте ключ: go run ./tools/releasekey -gen (закрытый ключ ляжет в $env:USERPROFILE\.netpilot, его не коммитят), затем скопируйте release.pub оттуда в корень репозитория и закоммитьте."
if (-not (Test-Path $PubFile)) {
    if (-not $DryRun) { throw "Нет release.pub в корне репозитория — сборки не смогут проверять подпись обновлений. $howTo" }
    Write-Host "Нет release.pub — пробная сборка без проверки подписи" -ForegroundColor Yellow
}
if (-not $Sign) {
    if (-not $DryRun) { throw "Не найден закрытый ключ подписи $KeyFile (укажите другой через -KeyFile). $howTo" }
    Write-Host "Нет ключа $KeyFile — SHA256SUMS не будет подписан" -ForegroundColor Yellow
}

if (-not $DryRun) {
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw "Нужен GitHub CLI (gh)" }
    # Ошибки gh проверяем по коду возврата: PowerShell 5.1 считает любой вывод в stderr исключением.
    $ErrorActionPreference = "Continue"
    gh repo view $Repo --json name 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Репозиторий $Repo не найден или нет доступа (gh auth status)" }
    gh release view "v$Version" --repo $Repo 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { throw "Релиз v$Version уже существует — укажите следующую версию" }
    $ErrorActionPreference = "Stop"
}

# Интерфейс Material 3 Expressive (Compose Multiplatform) → web\static\m3,
# откуда его встраивает Go-сборка. Нужен JDK 17+.
$m3 = Join-Path $Root "web-m3"
if ((Test-Path $m3) -and -not $SkipM3) {
    if (-not $env:JAVA_HOME) {
        $jdk = Get-ChildItem "C:\Program Files\Java" -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -match '^jdk-(1[7-9]|2\d)' } | Sort-Object Name | Select-Object -First 1
        if ($jdk) { $env:JAVA_HOME = $jdk.FullName }
    }
    Write-Host "-> интерфейс Material 3 Expressive"
    $ErrorActionPreference = "Continue"
    & (Join-Path $m3 "gradlew.bat") -p $m3 publishM3 --console=plain -q
    $code = $LASTEXITCODE
    $ErrorActionPreference = "Stop"
    if ($code -ne 0) { throw "Сборка интерфейса Material 3 не удалась" }
}

# Версия попадает в программу при сборке.
[IO.File]::WriteAllText((Join-Path $Root "VERSION"), $Version)
& (Join-Path $Root "build.ps1") -Targets all
if (-not $?) { throw "Сборка не удалась" }

$out = Join-Path $Root "dist\release"
if (Test-Path $out) { Remove-Item $out -Recurse -Force }
New-Item -ItemType Directory $out | Out-Null
$names = @{
    "netpilot.exe"          = "netpilot-windows-amd64.exe"
    "netpilot-linux-amd64"  = "netpilot-linux-amd64"
    "netpilot-linux-arm64"  = "netpilot-linux-arm64"
    "netpilot-linux-armv7"  = "netpilot-linux-armv7"
    "netpilot-linux-mipsle" = "netpilot-linux-mipsle"
}
$sums = @()
foreach ($src in $names.Keys | Sort-Object) {
    $dst = Join-Path $out $names[$src]
    Copy-Item (Join-Path $Root "dist\$src") $dst
    $hash = (Get-FileHash $dst -Algorithm SHA256).Hash.ToLower()
    $sums += "$hash  $($names[$src])"
}
# Окончания строк LF: файл читают устройства на Linux.
[IO.File]::WriteAllText((Join-Path $out "SHA256SUMS"), ($sums -join "`n") + "`n")

# Подпись SHA256SUMS и сразу проверка ключом, встроенным в сборки: если
# release.pub и закрытый ключ не пара, релиз не установится ни на одном устройстве.
if ($Sign) {
    $Go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $Go) { $Go = Join-Path $Root ".tools\go\bin\go.exe" }
    $sumsFile = Join-Path $out "SHA256SUMS"
    Push-Location $Root
    $ErrorActionPreference = "Continue"
    try {
        & $Go run ./tools/releasekey -sign -key $KeyFile $sumsFile
        if ($LASTEXITCODE -ne 0) { throw "Не удалось подписать SHA256SUMS" }
        if (Test-Path $PubFile) {
            & $Go run ./tools/releasekey -verify -pub $PubFile $sumsFile
            if ($LASTEXITCODE -ne 0) { throw "Ключ $KeyFile не пара к release.pub — устройства отвергнут такой релиз" }
        }
    } finally {
        $ErrorActionPreference = "Stop"
        Pop-Location
    }
}

# Проверка: Windows-сборка сообщает ту версию, которую выпускаем.
$reported = (& (Join-Path $out "netpilot-windows-amd64.exe") -version).Trim()
if ($reported -ne $Version) { throw "Сборка сообщает версию '$reported' вместо $Version" }

Write-Host "Файлы релиза: $out" -ForegroundColor Green
Get-ChildItem $out | ForEach-Object { "{0,-30} {1,8:N1} МБ" -f $_.Name, ($_.Length / 1MB) }
if ($DryRun) { return }

$ghArgs = @("release", "create", "v$Version", "--repo", $Repo, "--title", "NetPilot $Version")
if ($NotesFile) { $ghArgs += @("--notes-file", $NotesFile) }
else { $ghArgs += @("--notes", $(if ($Notes) { $Notes } else { "NetPilot $Version" })) }
$ghArgs += (Get-ChildItem $out).FullName
$ErrorActionPreference = "Continue"
gh @ghArgs
if ($LASTEXITCODE -ne 0) { throw "Публикация не удалась" }
Write-Host "Опубликовано: https://github.com/$Repo/releases/tag/v$Version" -ForegroundColor Green
