# Сборка NetPilot на Windows.
#   .\build.ps1                 — собрать под Windows (dist\netpilot.exe)
#   .\build.ps1 -Targets all    — Windows + Linux (amd64, arm64, armv7, mipsle)
#   .\build.ps1 -Run            — собрать и запустить
#   .\build.ps1 -Docker         — собрать Docker-образ netpilot:latest
#   .\build.ps1 -Targets linux -Compress — сжать Linux-сборки UPX (для роутеров)
# Если Go не установлен, скрипт скачает официальный портативный Go в .tools\go.
param(
    [string]$Targets = "windows",
    [switch]$Run,
    [switch]$Docker,
    [switch]$Compress
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$Tools = Join-Path $Root ".tools"

# Теги sing-box: QUIC (hysteria2/tuic), uTLS (reality), clash API (статистика и соединения).
$Tags = "with_quic,with_utls,with_clash_api"

function Get-Go {
    $sys = Get-Command go -ErrorAction SilentlyContinue
    if ($sys) { return $sys.Source }

    $local = Join-Path $Tools "go\bin\go.exe"
    if (Test-Path $local) { return $local }

    Write-Host "Go не найден, скачиваю портативный Go с go.dev..." -ForegroundColor Yellow
    $rel = (Invoke-RestMethod -Uri "https://go.dev/dl/?mode=json")[0]
    $file = $rel.files | Where-Object { $_.os -eq "windows" -and $_.arch -eq "amd64" -and $_.kind -eq "archive" }
    New-Item -ItemType Directory -Force $Tools | Out-Null
    $zip = Join-Path $Tools $file.filename
    $ProgressPreference = "SilentlyContinue"
    Invoke-WebRequest -Uri "https://go.dev/dl/$($file.filename)" -OutFile $zip

    $hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
    if ($hash -ne $file.sha256) {
        Remove-Item $zip
        throw "Контрольная сумма Go не совпала: $hash"
    }
    Expand-Archive -Path $zip -DestinationPath $Tools -Force
    Remove-Item $zip
    Write-Host "Установлен $($rel.version) в $Tools\go" -ForegroundColor Green
    return $local
}

function Get-Upx {
    $local = Join-Path $Tools "upx\upx.exe"
    if (Test-Path $local) { return $local }
    Write-Host "Скачиваю UPX с GitHub (upx/upx)..." -ForegroundColor Yellow
    $rel = Invoke-RestMethod "https://api.github.com/repos/upx/upx/releases/latest" -Headers @{ "User-Agent" = "NetPilot" }
    $asset = $rel.assets | Where-Object { $_.name -match "win64\.zip$" } | Select-Object -First 1
    $zip = Join-Path $Tools $asset.name
    $ProgressPreference = "SilentlyContinue"
    Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip
    $tmp = Join-Path $Tools "upx-tmp"
    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Force (Join-Path $Tools "upx") | Out-Null
    Copy-Item (Get-ChildItem $tmp -Recurse -Filter upx.exe | Select-Object -First 1).FullName $local
    Remove-Item $zip; Remove-Item $tmp -Recurse
    return $local
}

if ($Docker) {
    docker build -t netpilot:latest $Root
    if ($LASTEXITCODE -ne 0) { throw "docker build завершился с ошибкой" }
    Write-Host "Готово. Запуск: docker compose up -d" -ForegroundColor Green
    return
}

$Go = Get-Go
# Ровно та версия Go, что указана в go.mod: портативный Go в .tools может быть
# старше, а "local" тогда отказывался собирать. Нужную версию go скачает сам
# (с проверкой контрольной суммы), сборка остаётся воспроизводимой.
$goVer = (Select-String -Path (Join-Path $Root "go.mod") -Pattern '^go (\S+)').Matches[0].Groups[1].Value
$env:GOTOOLCHAIN = "go$goVer"
$env:CGO_ENABLED = "0"
$Version = (Get-Content (Join-Path $Root "VERSION") -Raw).Trim()
$LdFlags = "-s -w -X netpilot/internal/app.Version=$Version"
# Открытый ключ подписи релизов: программа будет ставить только обновления,
# подписанные парным закрытым ключом (tools\releasekey). Без release.pub —
# сборка разработчика, интерфейс предупредит, что подпись не проверяется.
$PubFile = Join-Path $Root "release.pub"
if (Test-Path $PubFile) {
    $Pub = (Get-Content $PubFile -Raw).Trim()
    if ($Pub -notmatch '^[A-Za-z0-9+/]{43}=$') { throw "release.pub: это не открытый ключ ed25519 в base64" }
    $LdFlags += " -X netpilot/internal/update.PublicKey=$Pub"
} else {
    Write-Host "release.pub не найден — сборка без проверки подписи обновлений" -ForegroundColor Yellow
}

$matrix = @{
    "windows" = @(@{ GOOS = "windows"; GOARCH = "amd64"; Out = "netpilot.exe" })
    "linux"   = @(
        @{ GOOS = "linux"; GOARCH = "amd64"; Out = "netpilot-linux-amd64" },
        @{ GOOS = "linux"; GOARCH = "arm64"; Out = "netpilot-linux-arm64" },
        @{ GOOS = "linux"; GOARCH = "arm"; GOARM = "7"; Out = "netpilot-linux-armv7" },
        @{ GOOS = "linux"; GOARCH = "mipsle"; GOMIPS = "softfloat"; Out = "netpilot-linux-mipsle" }
    )
}
$list = if ($Targets -eq "all") { $matrix["windows"] + $matrix["linux"] } else { $matrix[$Targets] }
if (-not $list) { throw "Неизвестная цель: $Targets (windows | linux | all)" }

Push-Location $Root
# Ошибки go проверяем по коду возврата; с "Stop" PowerShell прятал бы их текст.
$ErrorActionPreference = "Continue"
try {
    & $Go mod download
    if ($LASTEXITCODE -ne 0) { throw "go mod download завершился с ошибкой" }
    foreach ($t in $list) {
        $env:GOOS = $t.GOOS; $env:GOARCH = $t.GOARCH
        $env:GOARM = $t.GOARM; $env:GOMIPS = $t.GOMIPS
        $out = Join-Path "dist" $t.Out
        Write-Host "-> $($t.GOOS)/$($t.GOARCH) $out"
        & $Go build -trimpath -tags $Tags -ldflags $LdFlags -o $out ./cmd/netpilot
        if ($LASTEXITCODE -ne 0) { throw "Сборка $($t.GOOS)/$($t.GOARCH) не удалась" }
        # Сжимаем только Linux: сжатые .exe на Windows часто ложно считаются вирусами.
        if ($Compress -and $t.GOOS -eq "linux") {
            $upx = Get-Upx
            & $upx --best -q $out | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "UPX не смог сжать $out" }
        }
    }
} finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:GOARM, Env:GOMIPS -ErrorAction SilentlyContinue
    Pop-Location
}

Write-Host "Готово: $Root\dist" -ForegroundColor Green
if ($Run) { & (Join-Path $Root "dist\netpilot.exe") -data (Join-Path $Root "data") }
