package netpilot

// Соединения: что сейчас открывают устройства, куда это идёт и почему.

import androidx.compose.animation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

@Composable
fun ConnectionsPage() {
    val app = LocalApp.current
    var data by remember { mutableStateOf<J?>(null) }
    var paused by remember { mutableStateOf(false) }
    var device by remember { mutableStateOf("") }
    var kind by remember { mutableStateOf("all") }
    var q by remember { mutableStateOf("") }
    var confirmAll by remember { mutableStateOf(false) }
    suspend fun reload() { Api.objOrNull("/connections")?.let { data = it } }
    LaunchedEffect(app.reload) {
        reload()
        while (true) {
            delay(2000)
            if (!paused) reload()
        }
    }
    val d = data ?: run { Loading(); return }
    val all = d.a("connections")
    val isDirect = { c: J -> c.s("outbound") == "Напрямую" }
    val isBlock = { c: J -> c.s("outbound") == "Заблокировано" }
    val qq = q.lowercase()
    val list = all.filter { c ->
        (device.isEmpty() || c.s("sourceIp") == device) &&
            (kind == "all" || (if (kind == "direct") isDirect(c) else !isDirect(c) && !isBlock(c))) &&
            (qq.isEmpty() || listOf("host", "destination", "deviceName", "sourceIp", "outbound", "rule").any { c.s(it).lowercase().contains(qq) })
    }.sortedByDescending { it.l("down") + it.l("up") }
    val sources = all.map { it.s("sourceIp") }.distinct().sortedBy { ipKey(it) }
    val srcName = { ip: String -> all.firstOrNull { it.s("sourceIp") == ip }?.s("deviceName")?.ifEmpty { null } ?: ip }
    val compact = LocalCompact.current

    PageList {
        block("head") {
            PageHeader("Соединения", "Что сейчас открывают устройства и куда это идёт. Обновляется каждые 2 секунды.") {
                FilledTonalButton(onClick = { paused = !paused }, shapes = ButtonDefaults.shapes()) {
                    AnimatedContent(paused) { p -> Icon(if (p) Icons.Rounded.PlayArrow else Icons.Rounded.Pause, null, Modifier.size(18.dp)) }
                    Spacer(Modifier.width(6.dp)); Text(if (paused) "Продолжить" else "Пауза")
                }
                OutlinedButton(onClick = { confirmAll = true }, enabled = all.isNotEmpty(), shapes = ButtonDefaults.shapes(), colors = ButtonDefaults.outlinedButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Закрыть все") }
            }
        }
        block("kpi") {
            val tiles: List<@Composable (Modifier) -> Unit> = listOf(
                { m -> StatTile("Загрузка", if (d.has("totals")) mbps(d.d("downRate")) + " МБ/с" else "—", m) },
                { m -> StatTile("Отдача", if (d.has("totals")) mbps(d.d("upRate")) + " МБ/с" else "—", m) },
                { m -> StatTile("Соединений", "${all.size}", m) },
                { m -> StatTile("Через прокси", "${all.count { !isDirect(it) && !isBlock(it) }}", m, tone = Tone.Primary) },
            )
            if (compact) Column(verticalArrangement = Arrangement.spacedBy(12.dp)) { tiles.chunked(2).forEach { r -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { r.forEach { it(Modifier.weight(1f)) } } } }
            else Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) { tiles.forEach { it(Modifier.weight(1f)) } }
        }
        if (!d.b("available")) {
            block("na") { SectionCard { Empty(Icons.Rounded.SwapHoriz, "Список соединений недоступен", d.s("reason")) { if (d.s("reason") == "Ядро остановлено") CoreButton() } } }
            return@PageList
        }
        block("filters") {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(8.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(q, { q = it }, Modifier.widthIn(min = 240.dp, max = 360.dp), singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) }, placeholder = { Text("Сайт, адрес или правило") })
                Box(Modifier.widthIn(min = 220.dp, max = 300.dp)) { Select("Устройство", listOf("" to "Все устройства · ${sources.size}") + sources.map { it to "${srcName(it)} — $it" }, device, { device = it }) }
                Connected(listOf("all" to "Все", "proxy" to "Прокси", "direct" to "Напрямую"), kind, { kind = it })
            }
        }
        if (list.isEmpty()) block("empty") { SectionCard { Empty(Icons.Rounded.SwapHoriz, if (all.isNotEmpty()) "Ничего не найдено" else "Соединений нет", if (all.isNotEmpty()) "Измените фильтр." else "Устройства сейчас не передают данные через NetPilot.") } }
        val shown = list.take(150)
        shown.forEachIndexed { i, c ->
            item(key = "c-" + c.s("id")) {
                Box(Modifier.widthIn(max = 1128.dp).fillMaxWidth().animateItem()) {
                    SegItem(
                        i, shown.size,
                        headline = { Text(c.s("host").ifEmpty { c.s("destination") }.ifEmpty { "—" }, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        supporting = {
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(listOf(c.s("deviceName").ifEmpty { c.s("sourceIp") }, c.s("network").uppercase() + " · " + duration(c.l("start")), if (c.s("rule").isNotEmpty()) "правило: " + c.s("rule") else "нет правила").joinToString(" · "), maxLines = 2)
                                when {
                                    isDirect(c) -> Tag("Напрямую", Tone.Ok, Icons.Rounded.Home)
                                    isBlock(c) -> Tag("Заблокировано", Tone.Err, Icons.Rounded.Block)
                                    else -> Tag(c.s("outbound").ifEmpty { "прокси" }, Tone.Primary, Icons.Rounded.VpnLock)
                                }
                            }
                        },
                        trailing = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                if (!compact) Column(horizontalAlignment = Alignment.End) {
                                    Text("↓ " + bytes(c.l("down")), fontFamily = LocalMono.current, style = MaterialTheme.typography.bodySmall)
                                    Text("↑ " + bytes(c.l("up")), fontFamily = LocalMono.current, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                                val host = c.s("host").replace(Regex(":\\d+$"), "")
                                if (host.isNotEmpty()) {
                                    if (isDirect(c)) IconButton(onClick = { app.launch { quickRule(app, host, "proxy") } }) { Icon(Icons.Rounded.VpnLock, "Всегда через прокси: $host", tint = MaterialTheme.colorScheme.primary) }
                                    else IconButton(onClick = { app.launch { quickRule(app, host, "direct") } }) { Icon(Icons.Rounded.Home, "Всегда напрямую: $host") }
                                }
                                IconButton(onClick = { app.launch { Api.delete("/connections/" + encodeURIComponent(c.s("id"))); reload() } }) { Icon(Icons.Rounded.Close, "Закрыть соединение") }
                            }
                        },
                    )
                }
            }
        }
        if (list.size > 150) block("more") { Text("Показаны 150 самых крупных из ${list.size}", style = MaterialTheme.typography.bodySmall) }
        val outs = d.a("outbounds")
        if (outs.isNotEmpty()) {
            block("outs-title") { SectionTitle("Трафик по серверам") }
            block("outs") {
                SegGroup {
                    outs.forEachIndexed { i, o ->
                        SegItem(i, outs.size, headline = { Text(o.name) }, supporting = { Text("за сеанс " + bytes(o.l("down"))) }, trailing = { Text("↓ ${mbps(o.d("downRate"))} МБ/с", fontFamily = LocalMono.current) })
                    }
                }
            }
        }
    }
    if (confirmAll) ConfirmDialog("Закрыть все соединения? Приложения переподключатся сами.", confirm = "Закрыть все", onDismiss = { confirmAll = false }) { Api.delete("/connections"); reload() }
}
