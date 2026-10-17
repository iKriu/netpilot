package netpilot

// Обзор: активный сервер, трафик, показатели, активные устройства, шлюз, обновление.

import androidx.compose.animation.*
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.Canvas
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

/** Загрузка данных страницы; повторяется после сохранения (app.reload). */
@Composable
fun <T> rememberLoad(vararg keys: Any?, load: suspend () -> T): MutableState<T?> {
    val app = LocalApp.current
    val st = remember { mutableStateOf<T?>(null) }
    LaunchedEffect(app.reload, *keys) {
        try { st.value = load() } catch (e: ApiError) { if (e.status != 401) app.toast(e.message ?: "Ошибка", Kind.Err) }
    }
    return st
}

private class OverviewData(
    val settings: J, val nodes: List<J>, val groups: List<J>, val rules: List<J>, val net: J?,
    val conns: J?, val stats: J?, val auto: J?,
)

@Composable
fun OverviewPage() {
    val app = LocalApp.current
    val data by rememberLoad {
        app.refreshStatus()
        OverviewData(
            Api.obj("/settings"), Api.listOrEmpty("/nodes"), Api.listOrEmpty("/groups"), Api.listOrEmpty("/rules"),
            Api.objOrNull("/network"), Api.objOrNull("/connections"), Api.objOrNull("/stats"), Api.objOrNull("/auto"),
        )
    }
    var conns by remember { mutableStateOf<J?>(null) }
    var auto by remember { mutableStateOf<J?>(null) }
    val history = remember { mutableStateListOf<Double>() }
    LaunchedEffect(data) {
        val d = data ?: return@LaunchedEffect
        conns = d.conns; auto = d.auto
        while (true) {
            conns?.let { if (it.has("totals")) { history.add(it.d("downRate")); while (history.size > 60) history.removeAt(0) } }
            delay(3000)
            Api.objOrNull("/connections")?.let { conns = it }
            Api.objOrNull("/auto")?.let { auto = it }
        }
    }
    val d = data
    if (d == null) { Loading(); return }
    val compact = LocalCompact.current
    val s = app.status ?: jobj()
    val devs = d.net?.a("devices").orEmpty()
    val clients = devs.filter { isClient(it) }

    PageList {
        block("head") {
            val ip = s.sl("lanIps").firstOrNull()
            PageHeader(greeting(), listOfNotNull(ip?.let { "Шлюз $it" }, "через NetPilot — " + plural(clients.size, "устройство", "устройства", "устройств"), if (s.b("running")) "ядро " + s.s("core") else null).joinToString(" · "))
        }
        val alerts = buildList {
            if (s.s("error").isNotEmpty()) add(s.s("error") to Tone.Err)
            s.a("issues").forEach { add(it.s("message") to Tone.Warn) }
            if (d.nodes.isEmpty()) add("Серверы не добавлены: весь трафик идёт напрямую." to Tone.Secondary)
            d.net?.sl("warnings")?.forEach { add(it to Tone.Warn) }
        }
        alerts.forEachIndexed { i, (t, tone) -> block("alert$i") { Notice(t, tone, if (tone == Tone.Err) Icons.Rounded.Error else Icons.Rounded.Info) } }

        block("hero") {
            if (compact) Column(verticalArrangement = Arrangement.spacedBy(16.dp)) { Hero(d, auto); Traffic(conns, d.stats, history) }
            else Row(horizontalArrangement = Arrangement.spacedBy(16.dp), modifier = Modifier.height(IntrinsicSize.Min)) {
                Box(Modifier.weight(2f)) { Hero(d, auto) }
                Box(Modifier.weight(1f).fillMaxHeight()) { Traffic(conns, d.stats, history) }
            }
        }
        block("kpi") { Kpis(d, conns, auto, devs, clients) }
        block("bottom") {
            if (compact) Column(verticalArrangement = Arrangement.spacedBy(16.dp)) { ActiveDevices(clients); SideCards() }
            else Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                Box(Modifier.weight(2f)) { ActiveDevices(clients) }
                Box(Modifier.weight(1f)) { SideCards() }
            }
        }
    }
}

@Composable
private fun Hero(d: OverviewData, auto: J?) {
    val app = LocalApp.current
    val cs = MaterialTheme.colorScheme
    val id = d.settings.s("defaultTarget")
    val ag = auto?.a("groups")?.firstOrNull { it.s("groupId") == id && (id.isNotEmpty() || it.name == "Авто") }
    var defaultAction by remember(d) { mutableStateOf(d.settings.s("defaultAction").ifEmpty { "proxy" }) }
    var torrents by remember(d) { mutableStateOf(torrentsOn(d.rules)) }
    val compact = LocalCompact.current
    Surface(shape = RoundedCornerShape(if (compact) 28.dp else 32.dp), color = cs.primaryContainer, contentColor = cs.onPrimaryContainer, modifier = Modifier.fillMaxWidth()) {
        Box {
            LivingShape(MaterialShapes.Cookie12Sided, MaterialShapes.Sunny, cs.primary.copy(alpha = 0.12f), Modifier.align(Alignment.TopEnd).offset(60.dp, (-60).dp).size(260.dp))
            Column(Modifier.padding(if (compact) 20.dp else 28.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Активный сервер", style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                    if (!compact) Tag("для всего «через прокси»", Tone.Inverse, Icons.Rounded.AltRoute)
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(if (compact) 14.dp else 20.dp)) {
                    Box(Modifier.size(if (compact) 60.dp else 88.dp), contentAlignment = Alignment.Center) {
                        LivingShape(MaterialShapes.Cookie12Sided, MaterialShapes.Cookie9Sided, cs.primary, Modifier.matchParentSize(), periodMs = 3000)
                        Icon(Icons.Rounded.Public, null, tint = cs.onPrimary, modifier = Modifier.size(if (compact) 28.dp else 40.dp))
                    }
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(activeName(d.settings, d.nodes, d.groups), style = if (compact) MaterialTheme.typography.headlineSmallEmphasized else MaterialTheme.typography.displaySmallEmphasized, maxLines = 2)
                        val now = when {
                            ag == null -> "Отдельный сервер"
                            ag.has("scanning") -> ag.il("scanning").let { "Подбираю сервер: ${it.getOrElse(0) { 0 }} из ${it.getOrElse(1) { 0 }}…" }
                            ag.s("selected").isNotEmpty() -> "Сейчас работает " + (d.nodes.firstOrNull { it.id == ag.s("selected") }?.name ?: "—") + (if (ag.i("ms") > 0) " · ${ag.i("ms")} мс" else if (ag.i("ms") < 0) " · не отвечает" else "")
                            app.status?.b("running") == true -> "Сервер подбирается…"
                            else -> "Сервер выберется при запуске шлюза"
                        }
                        AnimatedContent(now, transitionSpec = { fadeIn() togetherWith fadeOut() }) { Text(it, style = MaterialTheme.typography.bodyLarge) }
                        if (ag != null) Text(listOfNotNull(if (ag.l("checkedAt") > 0) "проверен " + agoShort(ag.l("checkedAt")) else null, plural(ag.i("members"), "сервер", "сервера", "серверов") + " в группе").joinToString(" · "), style = MaterialTheme.typography.bodySmall)
                    }
                }
                Button(
                    onClick = { app.pickerOpen = true }, enabled = d.nodes.isNotEmpty(),
                    shapes = ButtonDefaults.shapesFor(ButtonDefaults.MediumContainerHeight),
                    contentPadding = ButtonDefaults.contentPaddingFor(ButtonDefaults.MediumContainerHeight),
                    modifier = Modifier.heightIn(min = ButtonDefaults.MediumContainerHeight),
                ) {
                    Icon(Icons.Rounded.SwapVert, null, Modifier.size(ButtonDefaults.iconSizeFor(ButtonDefaults.MediumContainerHeight)))
                    Spacer(Modifier.width(ButtonDefaults.iconSpacingFor(ButtonDefaults.MediumContainerHeight)))
                    Text("Сменить сервер", style = ButtonDefaults.textStyleFor(ButtonDefaults.MediumContainerHeight))
                }
                HorizontalDivider(color = cs.onPrimaryContainer.copy(alpha = 0.16f))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                    Text("Трафик без правила", style = MaterialTheme.typography.titleSmall)
                    Connected(listOf("proxy" to "Через прокси", "direct" to "Напрямую"), defaultAction, {
                        defaultAction = it
                        app.stage("settings") { val cur = Api.obj("/settings"); Api.put("/settings", cur.with("defaultAction" to it)) }
                    })
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        NpSwitch(torrents, { torrents = it; stageTorrents(app, it) })
                        Text("Торренты мимо прокси", style = MaterialTheme.typography.titleSmall)
                    }
                }
            }
        }
    }
}

@Composable
private fun Traffic(c: J?, stats: J?, history: List<Double>) {
    val cs = MaterialTheme.colorScheme
    val today = stats?.o("today")
    SectionCard(Modifier.fillMaxHeight(), title = "Трафик сейчас") {
        if (c == null || !c.has("totals")) {
            Text(if (LocalApp.current.status?.b("running") == true) "Данные появятся через пару секунд" else "Шлюз остановлен", color = cs.onSurfaceVariant)
        } else {
            Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Rounded.Download, null, tint = cs.primary)
                AnimatedContent(mbps(c.d("downRate")), transitionSpec = { (slideInVertically { -it } + fadeIn()) togetherWith (slideOutVertically { it } + fadeOut()) }) {
                    Text(it, style = MaterialTheme.typography.displayMediumEmphasized)
                }
                Text("МБ/с", style = MaterialTheme.typography.titleMedium, color = cs.onSurfaceVariant, modifier = Modifier.padding(bottom = 8.dp))
            }
            Text("отдача ${mbps(c.d("upRate"))} МБ/с", style = MaterialTheme.typography.bodyMedium, color = cs.onSurfaceVariant)
            Sparkline(history, Modifier.fillMaxWidth().height(56.dp))
        }
        if (today != null && today.l("down") > 0) {
            val prox = (today.l("proxyDown").toFloat() / today.l("down")).coerceIn(0f, 1f)
            val p by animateFloatAsState(prox, MaterialTheme.motionScheme.slowSpatialSpec())
            Row(Modifier.fillMaxWidth().height(12.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                if (p > 0.01f) Box(Modifier.weight(p).fillMaxHeight().clip(RoundedCornerShape(6.dp)).background(cs.primary))
                if (p < 0.99f) Box(Modifier.weight(1 - p).fillMaxHeight().clip(RoundedCornerShape(6.dp)).background(cs.secondaryContainer))
            }
            Row {
                Text("${(prox * 100).toInt()}% через прокси", style = MaterialTheme.typography.bodySmall, color = cs.primary, modifier = Modifier.weight(1f))
                Text("${100 - (prox * 100).toInt()}% напрямую", style = MaterialTheme.typography.bodySmall, color = cs.onSurfaceVariant)
            }
        }
        if (today != null) {
            HorizontalDivider()
            Text("За сегодня " + bytes(today.l("down") + today.l("up")) + (if (today.d("peak") > 0) " · пик ${mbps(today.d("peak"))} МБ/с в ${clockTime(today.l("peakAt"))}" else ""), style = MaterialTheme.typography.bodySmall, color = cs.onSurfaceVariant)
        }
    }
}


/** График скорости за последние минуты с плавной сменой значений. */
@Composable
private fun Sparkline(values: List<Double>, modifier: Modifier) {
    val cs = MaterialTheme.colorScheme
    val anim by animateFloatAsState(values.size.toFloat(), MaterialTheme.motionScheme.slowEffectsSpec())
    Canvas(modifier) {
        if (values.size < 2) return@Canvas
        val max = (values.maxOrNull() ?: 1.0).coerceAtLeast(1.0)
        val step = size.width / (values.size - 1).coerceAtLeast(1)
        val pts = values.mapIndexed { i, v -> Offset(i * step, size.height - (v / max * (size.height - 6)).toFloat() - 3) }
        val line = Path().apply {
            moveTo(pts[0].x, pts[0].y)
            for (i in 1 until pts.size) {
                val a = pts[i - 1]; val b = pts[i]
                cubicTo((a.x + b.x) / 2, a.y, (a.x + b.x) / 2, b.y, b.x, b.y)
            }
        }
        val fill = Path().apply { addPath(line); lineTo(pts.last().x, size.height); lineTo(0f, size.height); close() }
        drawPath(fill, Brush.verticalGradient(listOf(cs.primary.copy(alpha = 0.28f), cs.primary.copy(alpha = 0f))))
        drawPath(line, cs.primary, style = Stroke(width = 3.dp.toPx(), cap = StrokeCap.Round))
        if (anim > 0) drawCircle(cs.primary, 4.dp.toPx(), pts.last())
    }
}

@Composable
private fun Kpis(d: OverviewData, conns: J?, auto: J?, devs: List<J>, clients: List<J>) {
    val app = LocalApp.current
    val compact = LocalCompact.current
    val online = clients.count { it.b("online") }
    val newCount = devs.count { it.l("firstSeen") > 0 && nowSec() - it.l("firstSeen") < 86400 && isClient(it) }
    val id = d.settings.s("defaultTarget")
    val ag = auto?.a("groups")?.firstOrNull { it.s("groupId") == id && (id.isNotEmpty() || it.name == "Авто") }
    val okServers = auto?.o("latency")?.values?.count { (it as? J)?.i("ms") ?: 0 > 0 } ?: 0
    val list = conns?.a("connections").orEmpty()
    val tiles: List<@Composable (Modifier) -> Unit> = listOf(
        { m -> StatTile("Устройства", "$online", m, "в сети из ${clients.size}", if (newCount > 0) "новых за сутки: $newCount" else "в локальной сети: ${devs.count { it.b("online") }}", icon = Icons.Rounded.Devices, shape = MaterialShapes.Cookie4Sided) { app.go(Page.Devices) } },
        { m -> StatTile("Соединения", if (conns?.b("available") == true) "${list.size}" else "—", m, sub = if (conns?.b("available") == true) "через прокси " + list.count { it.s("outbound") != "Напрямую" && it.s("outbound") != "Заблокировано" } else conns?.s("reason").orEmpty(), icon = Icons.Rounded.SwapHoriz, shape = MaterialShapes.Clover4Leaf) { app.go(Page.Connections) } },
        { m -> StatTile("Правила", "${d.rules.count { it.b("enabled") }}", m, "из ${d.rules.size} включены", "проверяются сверху вниз", icon = Icons.Rounded.AltRoute, shape = MaterialShapes.Sunny) { app.go(Page.Rules) } },
        { m -> StatTile("Серверы", "${ag?.i("members") ?: d.nodes.size}", m, if (ag != null) "в активной группе" else "всего", if (okServers > 0) "отвечают: $okServers" else "ещё не проверялись", icon = Icons.Rounded.Dns, shape = MaterialShapes.SoftBurst) { app.go(Page.Servers) } },
    )
    if (compact) Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        tiles.chunked(2).forEach { row -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { row.forEach { it(Modifier.weight(1f)) } } }
    } else Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) { tiles.forEach { it(Modifier.weight(1f)) } }
}

@Composable
private fun ActiveDevices(clients: List<J>) {
    val app = LocalApp.current
    val active = clients.filter { it.b("online") }.sortedWith(compareByDescending<J> { it.d("downRate") + it.d("upRate") }.thenBy { ipKey(it.s("ip")) }).take(6)
    SectionCard(title = "Сейчас активны", action = { TextButton(onClick = { app.go(Page.Devices) }) { Text("Все устройства") } }) {
        if (active.isEmpty()) Empty(Icons.Rounded.Devices, "Сейчас никого нет", if (clients.isNotEmpty()) "Устройства, которые пользуются NetPilot, не в сети." else "Пока ни одно устройство не пускало трафик через NetPilot.")
        else SegGroup {
            active.forEachIndexed { i, dv ->
                val (ic, shape) = deviceIcon(dv.s("type"))
                var hover by remember { mutableStateOf(false) }
                SegItem(
                    i, active.size,
                    modifier = Modifier.onHover { hover = it },
                    headline = { Text(dv.name) },
                    supporting = { Text(dv.s("ip"), fontFamily = LocalMono.current) },
                    leading = { ShapeIcon(ic, shape, active = hover, online = true) },
                    trailing = {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            if (dv.d("downRate") > 1024) Text("↓ ${mbps(dv.d("downRate"))} МБ/с", fontFamily = LocalMono.current, style = MaterialTheme.typography.bodyMedium)
                            RouteTag(dv.s("route"))
                        }
                    },
                    onClick = { app.go(Page.Devices) },
                )
            }
        }
    }
}

@Composable
fun RouteTag(route: String) = when (route) {
    "proxy" -> Tag("Через прокси", Tone.Primary)
    "direct" -> Tag("Напрямую", Tone.Ok)
    "block" -> Tag("Без интернета", Tone.Err)
    else -> Tag("По правилам")
}

@Composable
private fun SideCards() {
    val app = LocalApp.current
    val s = app.status ?: jobj()
    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
        SectionCard(title = "Шлюз", action = { if (s.b("running")) Tag("работает", Tone.Ok) else Tag("остановлен") }) {
            Text(if (s.b("running")) s.s("core") + " " + s.s("coreVersion") else s.s("error").ifEmpty { "Трафик устройств не обрабатывается" }, style = MaterialTheme.typography.bodyMedium)
            CoreButton()
        }
        val u = app.update
        AnimatedVisibility(u?.b("available") == true) {
            SectionCard(color = MaterialTheme.colorScheme.tertiaryContainer) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    ShapeIcon(Icons.Rounded.Upgrade, MaterialShapes.SoftBurst, container = MaterialTheme.colorScheme.tertiary, content = MaterialTheme.colorScheme.onTertiary)
                    Text("Обновление " + u?.o("latest")?.s("version"), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                }
                Text("Доступна новая версия NetPilot. Сама она не ставится.", style = MaterialTheme.typography.bodyMedium)
                Button(onClick = { app.go(Page.Settings) }, colors = ButtonDefaults.buttonColors(MaterialTheme.colorScheme.tertiary, MaterialTheme.colorScheme.onTertiary), shapes = ButtonDefaults.shapes()) { Text("Посмотреть и обновить") }
            }
        }
    }
}
