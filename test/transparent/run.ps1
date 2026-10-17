# Проверка прозрачного режима на стенде (см. docker-compose.yml рядом).
$ErrorActionPreference = "Continue"
$api = "http://localhost:2019/api"
$compose = @("compose", "-f", "$PSScriptRoot\docker-compose.yml")
$failed = 0

function Check($name, $ok, $detail = "") {
    if ($ok) { Write-Host "[OK]   $name" -ForegroundColor Green }
    else { Write-Host "[FAIL] $name $detail" -ForegroundColor Red; $script:failed++ }
}
function Client([string]$cmd) { (docker @compose exec -T client sh -c $cmd 2>&1 | Out-String).Trim() }
function Gateway([string]$cmd) { (docker @compose exec -T gateway sh -c $cmd 2>&1 | Out-String).Trim() }
function Curl204 { Client "curl -s -m 8 -o /dev/null -w '%{http_code}' https://www.gstatic.com/generate_204" }
function Put($path, $body) { Invoke-RestMethod -WebSession $script:sess -Method Put "$api$path" -ContentType "application/json; charset=utf-8" -Body ([Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Depth 5))) }

# вход (пароль стенда задан в docker-compose.yml)
$script:sess = $null
for ($i = 0; $i -lt 30; $i++) {
    try { Invoke-RestMethod -Method Post "$api/auth/login" -ContentType "application/json" -Body '{"password":"lab-test-password"}' -SessionVariable sess | Out-Null; $script:sess = $sess; break }
    catch { Start-Sleep -Seconds 1 }
}
Check "вход в API по паролю" ($null -ne $script:sess)

# ждём, пока клиент поставит утилиты и сменит маршрут
for ($i = 0; $i -lt 60; $i++) {
    if ((Client "ip route | grep default") -match "172.30.0.2") { break }
    Start-Sleep -Seconds 1
}
Check "клиент ходит через шлюз 172.30.0.2" ((Client "ip route | grep default") -match "172.30.0.2")
Check "без NetPilot интернет у клиента работает (пересылка)" ((Curl204) -eq "204")

# включаем прозрачный режим
$s = Invoke-RestMethod -WebSession $script:sess "$api/settings"
$s.mode = "transparent"; $s.transparentScope = "all"; $s.hijackDns = $true
Put "/settings" $s | Out-Null
try { Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/start" | Out-Null } catch { Write-Host $_.ErrorDetails.Message }
$st = Invoke-RestMethod -WebSession $script:sess "$api/status"
Check "ядро запущено в прозрачном режиме" $st.running $st.error
Check "таблица nftables создана" ((Gateway "nft list table ip netpilot") -match "tproxy to :7893")
Check "правило маршрутизации по метке" ((Gateway "ip rule") -match "0x2017")

Check "клиент выходит в интернет через перехват" ((Curl204) -eq "204")
Start-Sleep -Seconds 3
$c = Invoke-RestMethod -WebSession $script:sess "$api/connections"
$seen = @($c.connections | Where-Object { $_.sourceIp -eq "172.30.0.10" })
$log = (Invoke-RestMethod -WebSession $script:sess "$api/core/log").log
Check "ядро видит трафик клиента (журнал: tproxy от 172.30.0.10)" ($log -match "inbound/tproxy\[tproxy-in\]: inbound connection from 172\.30\.0\.10")

$dns = Client "nslookup -timeout=5 example.com 8.8.8.8"
Check "DNS клиента перехвачен и отвечает" ($dns -match "Address: \d+\.\d+\.\d+\.\d+" -and $dns -notmatch "timed out") $dns
$log = (Invoke-RestMethod -WebSession $script:sess "$api/core/log").log
Check "DNS-запрос прошёл через ядро" ($log -match "dns: exchanged A example\.com")

# «Без интернета»
$dev = Invoke-RestMethod -WebSession $script:sess -Method Post "$api/devices" -ContentType "application/json; charset=utf-8" -Body ([Text.Encoding]::UTF8.GetBytes('{"name":"Клиент","type":"phone","ip":"172.30.0.10","mac":"","tags":[],"route":"block"}'))
Check "в nftables появился блок для 172.30.0.10" ((Gateway "nft list set ip netpilot block") -match "172.30.0.10")
Check "«Без интернета»: интернет недоступен" ((Curl204) -ne "204")
Check "«Без интернета»: шлюз в локальной сети доступен" ((Client "ping -c1 -W2 172.30.0.2") -match "1 packets received")

# «Напрямую» — мимо ядра
$dev.route = "direct"; Put "/devices/$($dev.id)" $dev | Out-Null
Check "в nftables устройство перенесено в direct" ((Gateway "nft list set ip netpilot direct") -match "172.30.0.10")
$before = ((Invoke-RestMethod -WebSession $script:sess "$api/core/log").log -split "`n").Count
Check "«Напрямую»: интернет работает" ((Curl204) -eq "204")
$after = ((Invoke-RestMethod -WebSession $script:sess "$api/core/log").log -split "`n") | Select-Object -Skip $before
Check "«Напрямую»: трафик идёт мимо ядра" (-not (($after -join "`n") -match "from 172\.30\.0\.10"))

# ---- применение конфигурации без отказов ----
$dev.route = ""; Put "/devices/$($dev.id)" $dev | Out-Null
Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/apply" | Out-Null
docker @compose exec -T -d client sh -c "rm -f /tmp/r; for i in `$(seq 1 40); do curl -s -m 3 -o /dev/null -w '%{http_code}\n' https://www.gstatic.com/generate_204 >> /tmp/r; sleep 0.1; done; echo done >> /tmp/r" | Out-Null
Start-Sleep -Seconds 1
for ($i = 0; $i -lt 3; $i++) { Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/apply" | Out-Null; Start-Sleep -Milliseconds 700 }
for ($i = 0; $i -lt 30 -and -not ((Client "cat /tmp/r") -match "done"); $i++) { Start-Sleep -Seconds 1 }
$codes = (Client "cat /tmp/r") -split "\r?\n" | Where-Object { $_ -match '^\d+$' }
$bad = @($codes | Where-Object { $_ -ne "204" }).Count
Check "три применения конфигурации во время запросов: ни одного отказа ($($codes.Count) запросов)" ($codes.Count -ge 30 -and $bad -eq 0) "неудачных: $bad"
Check "журнал: конфигурация применялась без остановки" ((docker @compose logs gateway 2>&1 | Out-String) -match "применена без остановки")

# сломанная конфигурация не должна ломать сеть
$s = Invoke-RestMethod -WebSession $script:sess "$api/settings"; $good = $s.dnsRemote
$s.dnsRemote = "bad://x"; Put "/settings" $s | Out-Null
$rejected = $false
try { Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/apply" | Out-Null } catch { $rejected = $true }
Check "сломанная конфигурация отвергнута" $rejected
Check "при этом интернет у клиента работает на прежней" ((Curl204) -eq "204")
$s.dnsRemote = $good; Put "/settings" $s | Out-Null
Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/apply" | Out-Null

# ---- IPv6 ----
# 2001:db8::1 — документационный адрес: «интернет», до которого в стенде не достучаться.
function Curl6 { Client "curl -6 -s -m 4 -o /dev/null -w '%{http_code} %{time_total}' http://[2001:db8::1]/; echo ' exit='`$?" }
$dev.route = ""; Put "/devices/$($dev.id)" $dev | Out-Null
$r = Curl6
Check "IPv6 «блокировать»: мгновенный отказ (устройство сразу перейдёт на IPv4)" ($r -match "exit=7" -and [double](($r -split " ")[1]) -lt 2) $r
Check "таблица nftables для IPv6 создана" ((Gateway "nft list table ip6 netpilot6") -match "reject with tcp reset")

$s = Invoke-RestMethod -WebSession $script:sess "$api/settings"; $s.ipv6 = "intercept"; Put "/settings" $s | Out-Null
Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/apply" | Out-Null
Check "IPv6 «перехватывать»: правило маршрутизации IPv6 по метке" ((Gateway "ip -6 rule") -match "0x2017")
$null = Curl6
Start-Sleep -Seconds 1
Check "IPv6 «перехватывать»: ядро видит IPv6-трафик клиента" ((Invoke-RestMethod -WebSession $script:sess "$api/core/log").log -match "fd00:30::10")

# маршрут «Напрямую» действует и на IPv6-адрес устройства (найден по MAC)
$mac = Client "cat /sys/class/net/eth0/address"
$dev.mac = $mac; $dev.route = "direct"; Put "/devices/$($dev.id)" $dev | Out-Null
Invoke-RestMethod -WebSession $script:sess -Method Post "$api/network/scan" | Out-Null
Check "IPv6-адрес устройства найден по MAC и попал в direct" ((Gateway "nft list set ip6 netpilot6 direct") -match "fd00:30::10") (Gateway "ip -6 neigh")
$dev.route = ""; Put "/devices/$($dev.id)" $dev | Out-Null

# остановка
Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/stop" | Out-Null
Check "после остановки таблица IPv6 удалена" (-not ((Gateway "nft list tables") -match "netpilot6"))
Check "после остановки таблица nftables удалена" (-not ((Gateway "nft list tables") -match "netpilot"))
Check "после остановки правило маршрутизации удалено" (-not ((Gateway "ip rule") -match "0x2017"))
Check "после остановки интернет у клиента работает" ((Curl204) -eq "204")

# аварийное завершение: правила остались, программа при старте должна их убрать
$dev.route = ""; Put "/devices/$($dev.id)" $dev | Out-Null
Invoke-RestMethod -WebSession $script:sess -Method Post "$api/core/start" | Out-Null
Check "перед сбоем перехват включён" ((Gateway "nft list tables") -match "netpilot")
docker @compose kill -s KILL gateway | Out-Null
docker @compose start gateway | Out-Null
for ($i = 0; $i -lt 30; $i++) { try { Invoke-RestMethod -WebSession $script:sess "$api/status" | Out-Null; break } catch { Start-Sleep -Seconds 1 } }
Check "после аварийного перезапуска остатки перехвата удалены" (-not ((Gateway "nft list tables") -match "netpilot"))
Check "и интернет у клиента работает" ((Curl204) -eq "204")

Write-Host ""
if ($failed -eq 0) { Write-Host "Все проверки пройдены" -ForegroundColor Green } else { Write-Host "Не пройдено: $failed" -ForegroundColor Red; exit 1 }
