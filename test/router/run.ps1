# Проверка режима роутера на стенде (см. docker-compose.yml рядом).
$ErrorActionPreference = "Continue"
$api = "http://localhost:2020/api"
$compose = @("compose", "-f", "$PSScriptRoot\docker-compose.yml")
$script:failed = 0

function Check($name, $ok, $detail = "") {
    if ($ok) { Write-Host "[OK]   $name" -ForegroundColor Green }
    else { Write-Host "[FAIL] $name $detail" -ForegroundColor Red; $script:failed++ }
}
function In([string]$svc, [string]$cmd) { (docker @compose exec -T $svc sh -c $cmd 2>&1 | Out-String).Trim() }
function Api($method, $path, $body) {
    $p = @{ Method = $method; Uri = "$api$path"; WebSession = $script:sess }
    if ($null -ne $body) { $p.ContentType = "application/json; charset=utf-8"; $p.Body = [Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Depth 6)) }
    Invoke-RestMethod @p
}
function WaitState($want, $seconds) {
    for ($i = 0; $i -lt $seconds; $i++) {
        $st = (Api GET "/router").status
        if ($st.state -eq $want) { return $st }
        Start-Sleep -Seconds 1
    }
    return (Api GET "/router").status
}
function ClientDHCP { In client "ip addr flush dev eth0; ip route flush dev eth0 2>/dev/null; udhcpc -i eth0 -n -q -t 8 -T 2 >/dev/null 2>&1; ip -4 -o addr show dev eth0; ip route show default" }
function Curl204 { In client "curl -s -m 10 -o /dev/null -w '%{http_code}' https://www.gstatic.com/generate_204" }

# вход
$script:sess = $null
for ($i = 0; $i -lt 40 -and -not $script:sess; $i++) {
    try { Invoke-RestMethod -Method Post "$api/auth/login" -ContentType "application/json" -Body '{"password":"lab-test-password"}' -SessionVariable s | Out-Null; $script:sess = $s }
    catch { Start-Sleep -Seconds 1 }
}
Check "вход в API" ($null -ne $script:sess)
for ($i = 0; $i -lt 60 -and -not ((In client "ls /ready") -match "ready"); $i++) { Start-Sleep -Seconds 1 }

$v = Api GET "/router"
Check "режим роутера доступен на этой системе" $v.support.available ($v.support.problems -join "; ")
$wanIf = ($v.interfaces | Where-Object { $_.addrs -match "^10\.99\.0\.3/" }).name
$lanIf = ($v.interfaces | Where-Object { $_.addrs -match "^10\.88\.0\.2/" }).name
Check "порты найдены: провайдер=$wanIf, сеть=$lanIf" ($wanIf -and $lanIf)

$cfg = @{
    enabled = $true
    wan     = @{ interface = $wanIf; proto = "pppoe"; username = "lab-user"; password = "wrong-pass"; dns = @() }
    lan     = @{ interface = $lanIf; address = "10.88.0.2/24"; dhcp = $true; rangeStart = "10.88.0.100"; rangeEnd = "10.88.0.150"; leaseHours = 1 }
}

# ---- PPPoE: неверный пароль ----
Api PUT "/router" $cfg | Out-Null
Start-Sleep -Seconds 12
$st = (Api GET "/router").status
Check "неверный пароль: понятная ошибка" ($st.state -ne "up" -and $st.message -match "пароль") "$($st.state): $($st.message)"
Api POST "/router/confirm" | Out-Null

# ---- PPPoE: верный пароль ----
$cfg.wan.password = "lab-pass"
$r = Api PUT "/router" $cfg
Check "настройки применены на пробу со сроком подтверждения" ($r.deadline -gt 0)
$st = WaitState "up" 30
Check "PPPoE подключено" ($st.state -eq "up" -and $st.ip -match "^10\.200\.0\.") "$($st.state) $($st.ip) $($st.message)"
Check "шлюз и DNS провайдера получены" ($st.gateway -eq "10.200.0.1" -and ($st.dns -contains "1.1.1.1")) "$($st.gateway) / $($st.dns)"
Api POST "/router/confirm" | Out-Null
$v = Api GET "/router"
Check "пароль провайдера не отдаётся в интерфейс" ((-not $v.config.wan.password) -and $v.passwordSet)

# ---- клиент локальной сети ----
$dhcp = ClientDHCP
Check "клиент получил адрес по DHCP от роутера" ($dhcp -match "inet 10\.88\.0\.1[0-4]\d/24") $dhcp
Check "шлюз клиента — роутер" ($dhcp -match "default via 10\.88\.0\.2") $dhcp
$tx0 = (Api GET "/router").status.txBytes
Check "клиент выходит в интернет через PPPoE" ((Curl204) -eq "204")
Check "трафик идёт через PPPoE-подключение" ((Api GET "/router").status.txBytes -gt $tx0)
Check "DNS роутера отвечает клиенту" ((In client "nslookup -timeout=5 example.com 10.88.0.2") -match "Address: \d+\.\d+\.\d+\.\d+")

# ---- закреплённый адрес ----
$mac = In client "cat /sys/class/net/eth0/address"
$dev = Api POST "/devices" @{ name = "Клиент"; type = "laptop"; ip = "10.88.0.77"; mac = $mac; tags = @(); pinned = $true; route = "" }
Check "закрепление записано в DHCP роутера" ((In router "cat /data/router/dhcp-hosts") -match "10\.88\.0\.77")
$dhcp = ClientDHCP
Check "клиент получил закреплённый адрес 10.88.0.77" ($dhcp -match "inet 10\.88\.0\.77/") $dhcp

# ---- защита роутера со стороны провайдера ----
$pppIP = ((Api GET "/router").status.ip -split "/")[0]
$probe = In isp "nc -z -w 3 $pppIP 2017 && echo OPEN || echo CLOSED"
Check "панель роутера не видна из интернета" ($probe -match "CLOSED") $probe

# ---- переподключение ----
Api POST "/router/reconnect" | Out-Null
$st = WaitState "up" 30
Check "переподключение PPPoE" ($st.state -eq "up") "$($st.state) $($st.message)"

# ---- откат без подтверждения ----
$cfg.lan.rangeEnd = "10.88.0.140"
Api PUT "/router" $cfg | Out-Null
Check "изменение ждёт подтверждения" ((Api GET "/router").pending -gt 0)
Start-Sleep -Seconds 18
$v = Api GET "/router"
Check "без подтверждения настройки откатились" ((-not $v.pending) -and $v.config.lan.rangeEnd -eq "10.88.0.150") "pending=$($v.pending) end=$($v.config.lan.rangeEnd)"
$st = WaitState "up" 30
Check "после отката интернет снова работает" ($st.state -eq "up" -and (Curl204) -eq "204")

# ---- динамический IP (DHCP провайдера) ----
$cfg.lan.rangeEnd = "10.88.0.150"
$cfg.wan.proto = "dhcp"
Api PUT "/router" $cfg | Out-Null
$st = WaitState "up" 30
Check "DHCP провайдера: адрес получен" ($st.ip -match "^10\.99\.0\.1[0-4]\d/" -and $st.gateway -eq "10.99.0.2") "$($st.state) $($st.ip) gw=$($st.gateway)"
Api POST "/router/confirm" | Out-Null
Check "клиент выходит в интернет через DHCP-подключение" ((Curl204) -eq "204")

# ---- выключение ----
$cfg.enabled = $false
Api PUT "/router" $cfg | Out-Null
Api POST "/router/confirm" | Out-Null
Check "режим роутера выключен: NAT снят" (-not ((In router "nft list tables") -match "netpilot_nat"))
Check "режим роутера выключен: DHCP-сервер остановлен" (-not ((In router "ps") -match "dnsmasq"))

Write-Host ""
if ($script:failed -eq 0) { Write-Host "Все проверки пройдены" -ForegroundColor Green } else { Write-Host "Не пройдено: $script:failed" -ForegroundColor Red; exit 1 }
