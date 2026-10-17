package netpilot

// Интернет: как NetPilot стоит в сети — шлюзом за роутером или сам роутер.

import androidx.compose.animation.*
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

private class NetData(val router: J, val tp: J?, val net: J?, val settings: J)

@Composable
fun InternetPage() {
    val app = LocalApp.current
    val data by rememberLoad { app.refreshStatus(); NetData(Api.obj("/router"), Api.objOrNull("/transparent"), Api.objOrNull("/network"), Api.obj("/settings")) }
    val d = data ?: run { Loading(); return }
    var v by remember(d) { mutableStateOf(d.router) }
    val fresh = remember(d) { (d.router.o("config") ?: jobj()).let { c -> c.withIn("wan", "password" to "", "dns" to (c.o("wan")?.sl("dns") ?: emptyList())) } }
    var c by remember(d) { mutableStateOf(fresh) }
    var saved by remember(d) { mutableStateOf(fresh.toString()) }
    DisposableEffect(d) {
        app.form = FormGuard(isDirty = { c.toString() != saved }) {
            val wan = c.o("wan")!!
            val lan = c.o("lan")!!
            val body = c.with("wan" to wan.with("mtu" to (wan.s("mtu").toIntOrNull() ?: wan.i("mtu"))), "lan" to lan.with("leaseHours" to (lan.s("leaseHours").toIntOrNull() ?: lan.i("leaseHours").takeIf { it > 0 } ?: 12)))
            Api.put("/router", body)
            c = c.withIn("wan", "password" to "")
            saved = c.toString()
            if (c.b("enabled")) "Настройки применены на пробу — проверьте интернет и подтвердите" else "Сохранено"
        }
        onDispose { if (app.form != null) app.form = null }
    }
    LaunchedEffect(d) {
        while (true) {
            delay(4000)
            if (c.b("enabled")) Api.objOrNull("/router")?.let { v = it }
        }
    }
    val s = app.status ?: jobj()
    val ip = s.sl("lanIps").firstOrNull() ?: "—"
    val gw = d.net?.a("devices")?.firstOrNull { it.b("gateway") }
    val clients = d.net?.a("devices")?.count { isClient(it) } ?: 0
    val savedEnabled = remember(saved) { (JsonFmt.parseToJsonElement(saved) as J).b("enabled") }
    val compact = LocalCompact.current

    PageList {
        block("head") { PageHeader("Интернет", "Как NetPilot стоит в сети: шлюзом за роутером или сам роутер") }
        block("mode") {
            val a: @Composable (Modifier) -> Unit = { m -> ChoiceCard(!c.b("enabled"), "Шлюз за роутером", "Интернет приходит на ваш роутер. В его DHCP укажите NetPilot основным шлюзом и DNS: $ip" + if (!savedEnabled) " · так сейчас" else "", Icons.Rounded.Router, m) { c = c.with("enabled" to false) } }
            val b: @Composable (Modifier) -> Unit = { m -> ChoiceCard(c.b("enabled"), "NetPilot — роутер", "Кабель провайдера включён прямо в это устройство: NetPilot подключается к провайдеру и раздаёт адреса сам." + if (savedEnabled) " · так сейчас" else "", Icons.Rounded.Hub, m) { c = c.with("enabled" to true) } }
            if (compact) Column(verticalArrangement = Arrangement.spacedBy(8.dp)) { a(Modifier); b(Modifier) } else Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) { a(Modifier.weight(1f)); b(Modifier.weight(1f)) }
        }
        if (!c.b("enabled")) {
            block("scheme") {
                SectionCard(title = "Схема сети", action = { if (s.b("running") && d.settings.s("mode") == "transparent") Tag("всё работает", Tone.Ok, Icons.Rounded.Check) else Tag("проверьте", Tone.Warn) }) {
                    val nodes = listOf(
                        Triple(Icons.Rounded.CellTower to MaterialShapes.Sunny, "Провайдер", "интернет"),
                        Triple(Icons.Rounded.Router to MaterialShapes.Cookie4Sided, "Роутер", gw?.s("ip") ?: "—"),
                        Triple(Icons.Rounded.Hub to MaterialShapes.Cookie12Sided, "NetPilot", ip),
                        Triple(Icons.Rounded.Devices to MaterialShapes.Clover4Leaf, plural(clients, "устройство", "устройства", "устройств"), "шлюз и DNS — NetPilot"),
                    )
                    val ok = listOf(true, true, s.b("running"))
                    if (compact) Column {
                        nodes.forEachIndexed { i, (icsh, t, sub) ->
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                                ShapeIcon(icsh.first, icsh.second, size = 52.dp, active = i == 2, container = if (i == 2) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.secondaryContainer, content = if (i == 2) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSecondaryContainer)
                                Column { Text(t, style = MaterialTheme.typography.titleSmall); Text(sub, style = MaterialTheme.typography.bodySmall, fontFamily = LocalMono.current) }
                            }
                            if (i < 3) Box(Modifier.padding(start = 24.dp, top = 4.dp, bottom = 4.dp).width(4.dp).height(20.dp).clip(RoundedCornerShape(2.dp)).background(if (ok[i]) LocalExtra.current.ok else MaterialTheme.colorScheme.outlineVariant))
                        }
                    } else Row(verticalAlignment = Alignment.Top) {
                        nodes.forEachIndexed { i, (icsh, t, sub) ->
                            Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                ShapeIcon(icsh.first, icsh.second, size = 64.dp, active = i == 2, container = if (i == 2) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.secondaryContainer, content = if (i == 2) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSecondaryContainer)
                                Text(t, style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)
                                Text(sub, style = MaterialTheme.typography.bodySmall, fontFamily = LocalMono.current, textAlign = TextAlign.Center)
                            }
                            if (i < 3) Box(Modifier.padding(top = 30.dp).width(40.dp).height(4.dp).clip(RoundedCornerShape(2.dp)).background(if (ok[i]) LocalExtra.current.ok else MaterialTheme.colorScheme.outlineVariant))
                        }
                    }
                }
            }
            block("checks") {
                SectionCard(title = "Проверка") {
                    val rows = buildList {
                        if (d.settings.s("mode") == "transparent") {
                            add(Triple("Перехват трафика", d.tp?.b("active") == true, if (d.tp?.b("active") == true) "включён" else if (d.tp?.b("supported") == true) "включится при запуске" else "недоступен"))
                            add(Triple("Перехват DNS", d.settings.b("hijackDns"), if (d.settings.b("hijackDns")) "включён" else "выключен"))
                            add(Triple("IPv6", d.settings.s("ipv6") != "off", when (d.settings.s("ipv6")) { "intercept" -> "перехватывается"; "off" -> "не трогается"; else -> "заблокирован" }))
                        } else add(Triple("Режим", false, "прокси-сервер: шлюзом не работает"))
                        add(Triple("Шлюз", s.b("running"), if (s.b("running")) "работает" else "остановлен"))
                    }
                    SegGroup { rows.forEachIndexed { i, (k, ok, txt) -> SegItem(i, rows.size, headline = { Text(k) }, trailing = { Tag(txt, if (ok) Tone.Ok else Tone.Warn) }) } }
                    d.tp?.sl("problems")?.forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = LocalExtra.current.warn) }
                }
            }
            block("router") {
                SectionCard(title = "Что указать в роутере", action = { FilledTonalButton(onClick = { copyText(ip); app.toast("Адрес скопирован", Kind.Ok) }, shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.ContentCopy, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Скопировать") } }) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(32.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        listOf("Основной шлюз" to ip, "DNS-сервер" to ip, "Где искать" to "Настройки роутера → LAN → DHCP-сервер").forEach { (k, vv) ->
                            Column { Text(k, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant); Text(vv, style = MaterialTheme.typography.titleLarge, fontFamily = if (vv == ip) LocalMono.current else null) }
                        }
                    }
                }
            }
        } else {
            val sup = v.o("support") ?: jobj()
            if (!sup.b("available")) block("na") { Notice("Режим роутера недоступен на этой системе: " + sup.sl("problems").joinToString("; "), Tone.Err, Icons.Rounded.Error) }
            else routerBlocks(v, c, { c = it })
        }
        block("log") { RouterLog() }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.routerBlocks(v: J, c: J, set: (J) -> Unit) {
    val w = c.o("wan") ?: jobj()
    val l = c.o("lan") ?: jobj()
    val st = v.o("status") ?: jobj()
    if (v.l("pending") > 0) block("pending") {
        val app = LocalApp.current
        Notice("Настройки применены на пробу. Если интернет и панель работают — подтвердите. Иначе через ${maxOf(0, v.l("pending") - nowSec())} с вернутся прежние.", Tone.Warn, Icons.Rounded.Timer) {
            BusyButton("Подтвердить", Icons.Rounded.Check) { Api.post("/router/confirm"); app.toast("Настройки сохранены", Kind.Ok); app.refreshPage() }
        }
    }
    block("wanstate") {
        val app = LocalApp.current
        val (tone, label) = when (st.s("state")) { "up" -> Tone.Ok to "Подключено"; "connecting" -> Tone.Warn to "Подключение…"; "error" -> Tone.Err to "Ошибка"; "down" -> Tone.Err to "Нет подключения"; "disabled" -> Tone.Neutral to "Выключено"; else -> Tone.Neutral to st.s("state").ifEmpty { "—" } }
        SectionCard(title = "Подключение к провайдеру", action = { if (st.s("state").let { it.isNotEmpty() && it != "disabled" }) BusyButton("Переподключить", Icons.Rounded.Refresh, style = "tonal") { Api.post("/router/reconnect"); app.refreshPage() } }) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Tag(label, tone)
                if (st.l("since") > 0) Text(uptime(st.l("since")), style = MaterialTheme.typography.bodySmall)
            }
            if (st.s("message").isNotEmpty()) Notice(st.s("message"), if (st.s("state") == "error") Tone.Err else Tone.Warn)
            if (st.s("state").let { it.isNotEmpty() && it != "disabled" }) {
                val rows = listOf("Интерфейс" to st.s("device"), "IP-адрес" to st.s("ip"), "Шлюз провайдера" to st.s("gateway"), "DNS" to st.sl("dns").joinToString(", "), "Получено / отправлено" to "${bytes(st.l("rxBytes"))} / ${bytes(st.l("txBytes"))}")
                SegGroup { rows.forEachIndexed { i, (k, vv) -> SegItem(i, rows.size, headline = { Text(vv.ifEmpty { "—" }, fontFamily = LocalMono.current) }, supporting = { Text(k) }) } }
            }
        }
    }
    block("wan") {
        val ifaces = listOf("" to "— выберите порт —") + v.a("interfaces").map { it.name to it.name + " · " + it.s("mac") + if (it.b("carrier")) "" else " · кабель не подключён" }
        SectionCard(title = "Провайдер (WAN)") {
            Select("Порт провайдера", ifaces, w.s("interface"), { set(c.withIn("wan", "interface" to it)) }, hint = "Порт, в который включён кабель провайдера")
            Text("Тип подключения", style = MaterialTheme.typography.labelLarge)
            Connected(listOf("pppoe" to "PPPoE", "dhcp" to "Динамический IP", "static" to "Статический IP"), w.s("proto").ifEmpty { "pppoe" }, { set(c.withIn("wan", "proto" to it)) }, fill = true)
            AnimatedContent(w.s("proto").ifEmpty { "pppoe" }) { proto ->
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    when (proto) {
                        "pppoe" -> {
                            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                Field("Логин", w.s("username"), { set(c.withIn("wan", "username" to it)) }, Modifier.weight(1f))
                                Field("Пароль", w.s("password"), { set(c.withIn("wan", "password" to it)) }, Modifier.weight(1f), password = true, placeholder = if (v.b("passwordSet")) "сохранён" else null, hint = if (v.b("passwordSet")) "Пусто — не менять" else null)
                            }
                            Field("Имя сервиса", w.s("service"), { set(c.withIn("wan", "service" to it)) }, hint = "Обычно не нужно")
                        }
                        "static" -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            Field("IP-адрес с маской", w.s("address"), { set(c.withIn("wan", "address" to it.trim())) }, Modifier.weight(1f), mono = true, placeholder = "203.0.113.10/24")
                            Field("Шлюз провайдера", w.s("gateway"), { set(c.withIn("wan", "gateway" to it.trim())) }, Modifier.weight(1f), mono = true)
                        }
                        else -> Text("Адрес, шлюз и DNS выдаёт провайдер автоматически.", style = MaterialTheme.typography.bodyMedium)
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Field("DNS-серверы", w.sl("dns").joinToString(", "), { set(c.withIn("wan", "dns" to lines(it))) }, Modifier.weight(1f), mono = true, placeholder = "провайдера")
                Field("MTU", (w.s("mtu").ifEmpty { w.i("mtu").toString() }), { set(c.withIn("wan", "mtu" to (it.toIntOrNull() ?: 0))) }, Modifier.weight(1f), number = true, hint = "0 — автоматически")
                Field("Клонировать MAC", w.s("mac"), { set(c.withIn("wan", "mac" to it.trim())) }, Modifier.weight(1f), mono = true, placeholder = "aa:bb:cc:dd:ee:ff")
            }
        }
    }
    block("lan") {
        val ifaces = listOf("" to "— выберите порт —") + v.a("interfaces").map { it.name to it.name + " · " + it.s("mac") }
        SectionCard(title = "Локальная сеть (LAN)") {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Select("Порт локальной сети", ifaces, l.s("interface"), { set(c.withIn("lan", "interface" to it)) }, Modifier.weight(1f))
                Field("Адрес роутера", l.s("address"), { set(c.withIn("lan", "address" to it.trim())) }, Modifier.weight(1f), mono = true, hint = "С маской, например 192.168.10.1/24")
            }
            SegGroup { SwitchRow(0, 1, "Раздавать адреса (DHCP)", null, Icons.Rounded.SettingsEthernet, l.b("dhcp")) { set(c.withIn("lan", "dhcp" to it)) } }
            AnimatedVisibility(l.b("dhcp")) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Field("Раздавать с", l.s("rangeStart"), { set(c.withIn("lan", "rangeStart" to it.trim())) }, Modifier.weight(1f), mono = true)
                    Field("по", l.s("rangeEnd"), { set(c.withIn("lan", "rangeEnd" to it.trim())) }, Modifier.weight(1f), mono = true)
                    Field("Аренда, часов", l.i("leaseHours").toString(), { set(c.withIn("lan", "leaseHours" to (it.toIntOrNull() ?: 0))) }, Modifier.weight(1f), number = true)
                }
            }
        }
    }
    block("hint") { Text("Новые настройки применяются на пробу: если за 90 секунд их не подтвердить, вернутся прежние — доступ к панели не потеряется.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
}

@Composable
private fun RouterLog() {
    var log by remember { mutableStateOf<String?>(null) }
    SectionCard(title = "Журнал подключения", action = { BusyButton("Показать", style = "tonal") { log = (Api.obj("/router/log")).s("log").ifEmpty { "Журнал пуст" } } }) {
        AnimatedVisibility(log != null) {
            Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLow) {
                Text(log ?: "", Modifier.padding(16.dp).heightIn(max = 320.dp).verticalScrollable(), fontFamily = LocalMono.current, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}
