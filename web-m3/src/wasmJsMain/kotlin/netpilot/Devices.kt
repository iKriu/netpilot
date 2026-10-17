package netpilot

// Устройства: кто ходит через NetPilot, маршрут каждого, карточка устройства.

import androidx.compose.animation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.rememberLazyListState
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

/** Поля, которые считает сервер и которые не хранятся. */
private val VIEW_FIELDS = setOf("online", "lastSeen", "seenIp", "randomMac", "client", "configured", "up", "down", "upRate", "downRate", "conns")
fun stripView(d: J): J = J(d.filterKeys { it !in VIEW_FIELDS })

private class DevData(val net: J, val nodes: List<J>, val groups: List<J>, val stats: J?)

@Composable
fun DevicesPage() {
    val app = LocalApp.current
    val data by rememberLoad { DevData(Api.obj("/network"), Api.listOrEmpty("/nodes"), Api.listOrEmpty("/groups"), Api.objOrNull("/stats")) }
    var net by remember(data) { mutableStateOf(data?.net) }
    val d = data ?: run { Loading(); return }
    // Несохранённые правки маршрутов (id → поля), переживают фоновое обновление.
    val patches = remember(data) { mutableStateMapOf<String, J>() }
    var filter by remember { mutableStateOf(storageGet("np-dev-filter") ?: "clients") }
    var q by remember { mutableStateOf("") }
    val selected = remember { mutableStateListOf<String>() }
    var open by remember { mutableStateOf<J?>(null) }
    var confirmCleanup by remember { mutableStateOf(false) }
    var bulkRoute by remember { mutableStateOf("proxy") }

    LaunchedEffect(data) {
        while (true) {
            delay(15000)
            if (open == null && patches.isEmpty()) Api.objOrNull("/network")?.let { net = it }
        }
    }
    val devs = (net ?: d.net).a("devices").map { dv -> patches[dv.id]?.let { p -> dv.with(*p.entries.map { it.key to it.value }.toTypedArray()) } ?: dv }
    val first = devs.minOfOrNull { if (it.l("firstSeen") > 0) it.l("firstSeen") else Long.MAX_VALUE } ?: 0
    val isNew = { dv: J -> dv.l("firstSeen") > 0 && dv.l("firstSeen") - first > 3600 && nowSec() - dv.l("firstSeen") < 86400 }
    val filters = mapOf<String, (J) -> Boolean>("clients" to { isClient(it) }, "all" to { true }, "online" to { it.b("online") }, "own" to { it.s("route").isNotEmpty() }, "new" to isNew)
    val qq = q.lowercase()
    val list = devs.filter(filters[filter] ?: { isClient(it) })
        .filter { dv -> qq.isEmpty() || listOf("name", "ip", "mac", "hostname", "vendor").any { dv.s(it).lowercase().contains(qq) } }
        .sortedWith(compareByDescending<J> { it.b("online") }.thenByDescending { it.d("downRate") + it.d("upRate") }.thenBy { ipKey(it.s("ip")) })
    fun setFilter(f: String) { filter = f; storageSet("np-dev-filter", f) }
    fun stageDevice(dv: J, vararg patch: Pair<String, Any?>) {
        val merged = (patches[dv.id] ?: jobj()).with(*patch)
        patches[dv.id] = merged
        val full = stripView(dv.with(*patch))
        app.stage("dev:" + dv.id) { Api.put("/devices/" + dv.id, full) }
    }
    val compact = LocalCompact.current

    Box(Modifier.fillMaxSize()) {
        PageList(rememberLazyListState()) {
            block("head") {
                PageHeader("Устройства", if (filter == "all") "Вся локальная сеть · " + plural(devs.size, "устройство", "устройства", "устройств") else "Те, кто ходит через NetPilot или настроен вручную · во всей сети — " + plural(devs.size, "устройство", "устройства", "устройств")) {
                    if (!compact) TextButton(onClick = { confirmCleanup = true }) { Icon(Icons.Rounded.CleaningServices, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Убрать забытые") }
                    BusyButton("Найти в сети", Icons.Rounded.Radar, style = "tonal") {
                        val r = Api.post("/network/scan") as J
                        app.toast("Найдено устройств: ${r.i("found")}" + (if (r.i("added") > 0) ", новых: ${r.i("added")}" else "") + (if (r.i("removed") > 0) ", удалено забытых: ${r.i("removed")}" else ""), Kind.Ok)
                        net = Api.obj("/network")
                    }
                }
            }
            (net ?: d.net).sl("warnings").forEachIndexed { i, w -> block("w$i") { Notice(w) } }
            block("filters") {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                    Connected(listOf("clients" to "NetPilot", "all" to "Вся сеть"), if (filter in setOf("clients", "all")) filter else "", { setFilter(it) },
                        counts = mapOf("clients" to devs.count(filters["clients"]!!), "all" to devs.size))
                    listOf("online" to "В сети", "own" to "Свой маршрут", "new" to "Новые").forEach { (f, l) ->
                        Chip("$l · ${devs.count(filters[f]!!)}", filter == f) { setFilter(if (it) f else "clients") }
                    }
                    OutlinedTextField(q, { q = it }, Modifier.widthIn(min = 240.dp, max = 320.dp), singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) }, placeholder = { Text("Имя, IP или MAC") })
                }
            }
            if (devs.isEmpty()) block("empty") { SectionCard { Empty(Icons.Rounded.Devices, "Устройства не найдены", "Нажмите «Найти в сети» или добавьте устройство вручную.") } }
            else if (list.isEmpty() && filter == "clients" && qq.isEmpty()) block("empty") {
                SectionCard { Empty(Icons.Rounded.Devices, "Пока никто не пользуется NetPilot", "Здесь появятся устройства, которые пускают трафик через NetPilot. Остальные — в «Вся сеть».") { FilledTonalButton(onClick = { setFilter("all") }) { Text("Показать всю сеть") } } }
            } else if (list.isEmpty()) block("empty") { SectionCard { Empty(Icons.Rounded.SearchOff, "Ничего не найдено", "Измените фильтр или строку поиска.") } }
            list.forEachIndexed { i, dv ->
                item(key = "d-" + dv.id) {
                    Box(Modifier.widthIn(max = 1128.dp).fillMaxWidth().animateItem()) {
                        val (ic, shape) = deviceIcon(dv.s("type"))
                        val sel = dv.id in selected
                        var hover by remember { mutableStateOf(false) }
                        SegItem(
                            i, list.size,
                            modifier = Modifier.onHover { hover = it },
                            selected = sel,
                            headline = {
                                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    Text(dv.name, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    if (isNew(dv)) Tag("новое", Tone.Tertiary)
                                }
                            },
                            supporting = {
                                Text(listOfNotNull(
                                    dv.s("ip").ifEmpty { null },
                                    if (dv.s("vendor").isNotEmpty() && !dv.name.startsWith(dv.s("vendor"))) dv.s("vendor") else typeName(dv.s("type")),
                                    if (dv.b("pinned")) "IP закреплён" else if (dv.b("randomMac")) "случайный MAC" else null,
                                    if (!dv.b("online")) (if (dv.l("lastSeen") > 0) "не в сети, " + ago(dv.l("lastSeen")) else "не в сети") else null,
                                    if (dv.d("downRate") > 1024) "↓ ${mbps(dv.d("downRate"))} МБ/с" else null,
                                ).joinToString(" · "), maxLines = 2)
                            },
                            leading = {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    if (!compact) Checkbox(sel, { c -> if (c) selected.add(dv.id) else selected.remove(dv.id) })
                                    ShapeIcon(ic, shape, active = hover || sel, online = dv.b("online"), container = if (dv.b("online")) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.secondaryContainer, content = if (dv.b("online")) MaterialTheme.colorScheme.onPrimaryContainer else MaterialTheme.colorScheme.onSecondaryContainer)
                                }
                            },
                            trailing = { RouteMenu(dv, d.nodes, d.groups) { route -> stageDevice(dv, "route" to route, "routeTarget" to (if (route == "proxy") dv.s("routeTarget") else "")) } },
                            onClick = { open = dv },
                        )
                    }
                }
            }
        }
        // Выбор нескольких устройств — плавающая панель действий.
        Box(Modifier.align(Alignment.BottomCenter).padding(bottom = 96.dp)) {
            AnimatedVisibility(selected.isNotEmpty() && !app.dirty, enter = slideInVertically(MaterialTheme.motionScheme.defaultSpatialSpec()) { it * 2 } + fadeIn(), exit = slideOutVertically { it * 2 } + fadeOut()) {
                FloatingBar(vibrant = false) {
                    Text("Выбрано: ${selected.size}", Modifier.padding(horizontal = 12.dp), style = MaterialTheme.typography.titleSmall)
                    Box(Modifier.width(200.dp)) { Select("Маршрут", ROUTES, bulkRoute, { bulkRoute = it }) }
                    TextButton(onClick = {
                        selected.toList().forEach { id -> devs.firstOrNull { it.id == id }?.let { stageDevice(it, "route" to bulkRoute, "routeTarget" to "") } }
                        selected.clear()
                    }) { Text("Назначить") }
                    IconButton(onClick = {
                        selected.toList().forEach { id -> devs.firstOrNull { it.id == id }?.let { if (it.s("ip").isNotEmpty() && it.s("mac").isNotEmpty()) stageDevice(it, "pinned" to true) } }
                        selected.clear()
                    }) { Icon(Icons.Rounded.PushPin, "Закрепить IP") }
                    IconButton(onClick = { selected.clear() }) { Icon(Icons.Rounded.Close, "Снять выделение") }
                }
            }
        }
        ExtendedFloatingActionButton(
            onClick = { open = jobj("name" to "", "type" to "other", "ip" to "", "mac" to "", "tags" to emptyList<String>(), "pinned" to false, "route" to "", "routeTarget" to "") },
            icon = { Icon(Icons.Rounded.Add, null) }, text = { Text("Устройство") },
            modifier = Modifier.align(Alignment.BottomEnd).padding(end = 24.dp, bottom = 24.dp),
        )
    }
    open?.let { dv -> DeviceDialog(dv, d, onSaved = { patches.remove(dv.id); app.pending.remove("dev:" + dv.id) }) { open = null } }
    if (confirmCleanup) ConfirmDialog("Удалить устройства, которых сейчас нет в сети, которые не пользовались NetPilot 30 дней и ничем не настроены? Переименованные, закреплённые и устройства из правил останутся.", onDismiss = { confirmCleanup = false }) {
        val r = Api.post("/devices/cleanup") as J
        app.toast(if (r.i("removed") > 0) "Удалено устройств: ${r.i("removed")}" else "Удалять нечего", Kind.Ok)
        net = Api.obj("/network")
    }
}

/** Маршрут устройства — меню прямо в строке. */
@Composable
private fun RouteMenu(dv: J, nodes: List<J>, groups: List<J>, onChange: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val route = dv.s("route")
    val tone = when (route) { "proxy" -> Tone.Primary; "direct" -> Tone.Ok; "block" -> Tone.Err; else -> Tone.Neutral }
    val (bg, fg) = toneColors(tone)
    Box {
        Surface(onClick = { open = true }, shape = MaterialTheme.shapes.medium, color = bg, contentColor = fg) {
            Row(Modifier.padding(start = 12.dp, end = 6.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                Column {
                    Text(routeName(route), style = MaterialTheme.typography.labelLarge)
                    if (route == "proxy") Text("через " + (if (dv.s("routeTarget").isNotEmpty()) targetName(dv.s("routeTarget"), nodes, groups) else "активный сервер"), style = MaterialTheme.typography.labelSmall, maxLines = 1)
                }
                Icon(Icons.Rounded.ArrowDropDown, null)
            }
        }
        DropdownMenu(open, { open = false }, shape = MaterialTheme.shapes.large) {
            ROUTES.forEach { (v, l) ->
                DropdownMenuItem(text = { Text(l) }, onClick = { open = false; onChange(v) }, leadingIcon = if (v == route) ({ Icon(Icons.Rounded.Check, null) }) else null)
            }
        }
    }
}

@Composable
private fun DeviceDialog(orig: J, d: DevData, onSaved: () -> Unit, onDismiss: () -> Unit) {
    val app = LocalApp.current
    val isNew = orig.id.isEmpty()
    var dv by remember { mutableStateOf(stripView(orig)) }
    var tags by remember { mutableStateOf(orig.sl("tags").joinToString(", ")) }
    var confirmDelete by remember { mutableStateOf(false) }
    val today = if (!isNew) d.stats?.o("today")?.o("devices")?.get(orig.s("ip")) as? kotlinx.serialization.json.JsonArray else null
    val (ic, shape) = deviceIcon(dv.s("type"))
    AppDialog(onDismiss, null, width = 620.dp, actions = { dlg ->
        if (!isNew) TextButton(onClick = { confirmDelete = true }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Удалить") }
        Spacer(Modifier.weight(1f))
        TextButton(onClick = { dlg.close {} }) { Text("Отмена") }
        BusyButton("Сохранить") {
            if (dv.name.isBlank()) throw ApiError("Укажите название")
            if (dv.b("pinned") && (dv.s("ip").isEmpty() || dv.s("mac").isEmpty())) throw ApiError("Чтобы закрепить IP, нужны IP и MAC-адрес")
            var body = dv.with("tags" to lines(tags))
            if (body.s("route") != "proxy") body = body.with("routeTarget" to "")
            if (isNew) Api.post("/devices", JsonObjectWithout(body, "id")) else { Api.put("/devices/" + orig.id, body); onSaved() }
            app.toast("Устройство сохранено", Kind.Ok)
            app.refreshCounts()
            dlg.close { app.refreshPage() }
        }
    }) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            ShapeIcon(ic, shape, MaterialShapes.Cookie12Sided, active = true, size = 64.dp, online = orig.b("online"))
            Column(Modifier.weight(1f)) {
                Text(if (isNew) "Новое устройство" else dv.name.ifEmpty { "Устройство" }, style = MaterialTheme.typography.headlineSmallEmphasized)
                if (!isNew) Text(listOfNotNull(typeName(dv.s("type")), dv.s("vendor").ifEmpty { null }, if (orig.b("online")) "в сети" else if (orig.l("lastSeen") > 0) "не в сети, " + ago(orig.l("lastSeen")) else "не в сети").joinToString(" · "), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (!isNew) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            StatTile("Сейчас", if (orig.d("downRate") > 1024) mbps(orig.d("downRate")) + " МБ/с" else "—", Modifier.weight(1f))
            StatTile("Сегодня", today?.let { bytes((it.getOrNull(0)?.toString()?.toLongOrNull() ?: 0) + (it.getOrNull(1)?.toString()?.toLongOrNull() ?: 0)) } ?: "—", Modifier.weight(1f))
            StatTile("Соединений", orig.i("conns").toString(), Modifier.weight(1f))
        }
        Text("Маршрут всего трафика", style = MaterialTheme.typography.labelLarge)
        Connected(listOf("" to "Правила", "proxy" to "Прокси", "direct" to "Напрямую", "block" to "Без сети"), dv.s("route"), { dv = dv.with("route" to it) }, fill = true)
        Text("«По правилам» — решают правила. Свой маршрут важнее правил.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        AnimatedVisibility(dv.s("route") == "proxy", enter = expandVertically() + fadeIn(), exit = shrinkVertically() + fadeOut()) {
            Select("Через какой сервер", targetOptions(d.nodes, d.groups), dv.s("routeTarget"), { dv = dv.with("routeTarget" to it) })
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Field("Название", dv.name, { dv = dv.with("name" to it) }, Modifier.weight(1f), placeholder = "Ноутбук бухгалтерии")
            Select("Тип", DEVICE_TYPES, dv.s("type").ifEmpty { "other" }, { dv = dv.with("type" to it) }, Modifier.weight(1f))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Field("IP-адрес", dv.s("ip"), { dv = dv.with("ip" to it.trim()) }, Modifier.weight(1f), mono = true, placeholder = "192.168.0.20")
            Field("MAC-адрес", dv.s("mac"), { dv = dv.with("mac" to it.trim()) }, Modifier.weight(1f), mono = true, placeholder = "aa:bb:cc:dd:ee:ff")
        }
        SegGroup {
            SwitchRow(0, 1, "Закрепить IP-адрес", if (dv.s("ip").isNotEmpty()) "Роутер всегда будет выдавать ${dv.s("ip")}" else "Нужны IP и MAC-адрес", Icons.Rounded.PushPin, dv.b("pinned")) { dv = dv.with("pinned" to it) }
        }
        Field("Группы устройства", tags, { tags = it }, placeholder = "Семья, Гости", hint = "Через запятую. Группой удобно назначать правило сразу нескольким устройствам.")
        if (!isNew) SegGroup {
            val rows = listOfNotNull(
                orig.s("hostname").takeIf { it.isNotEmpty() }?.let { "Имя в сети" to it },
                "MAC" to (orig.s("mac").ifEmpty { "—" } + if (orig.b("randomMac")) " · случайный" else ""),
                if (orig.l("firstSeen") > 0) "Впервые в сети" to dateTime(orig.l("firstSeen")) else null,
            )
            rows.forEachIndexed { i, (k, v) -> SegItem(i, rows.size, headline = { Text(v) }, supporting = { Text(k) }) }
        }
    }
    if (confirmDelete) ConfirmDialog("Удалить устройство «${dv.name}»? При следующем сканировании оно появится снова как новое.", onDismiss = { confirmDelete = false }) {
        Api.delete("/devices/" + orig.id)
        app.refreshCounts()
        onDismiss()
        app.refreshPage()
    }
}
