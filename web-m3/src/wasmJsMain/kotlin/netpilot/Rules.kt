package netpilot

// Правила: «Куда пойдёт сайт», основной трафик, список правил с перетаскиванием,
// редактор «Кто → Что → Куда», готовые сценарии.

import androidx.compose.animation.*
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import kotlinx.serialization.json.JsonArray

val QUICK_GEOSITE = listOf("category-ru" to "Сайты РФ", "category-ads-all" to "Реклама", "google" to "Google", "category-games" to "Игры", "apple" to "Apple", "microsoft" to "Microsoft")
val QUICK_GEOIP = listOf("ru" to "Россия", "by" to "Беларусь", "kz" to "Казахстан", "us" to "США", "de" to "Германия", "nl" to "Нидерланды")
val PROTOCOLS = listOf("bittorrent" to "Торренты (BitTorrent)")

private val WHAT_LISTS = listOf("apps", "lists", "domains", "ips", "geosite", "geoip", "ports", "protocols")
private val WHO_LISTS = listOf("deviceIds", "tags", "cidrs")

fun emptyRule(): J = jobj(
    "name" to "", "enabled" to true,
    "who" to jobj("deviceIds" to emptyList<String>(), "tags" to emptyList<String>(), "cidrs" to emptyList<String>()),
    "what" to jobj("apps" to emptyList<String>(), "lists" to emptyList<String>(), "domains" to emptyList<String>(), "ips" to emptyList<String>(),
        "geosite" to emptyList<String>(), "geoip" to emptyList<String>(), "ports" to emptyList<String>(), "network" to "", "protocols" to emptyList<String>()),
    "where" to jobj("action" to "proxy", "target" to ""),
)

/** Правило со всеми полями (сервер мог не прислать пустые списки). */
fun normRule(r: J): J {
    var who = r.o("who") ?: jobj()
    WHO_LISTS.forEach { if (who[it] !is JsonArray) who = who.with(it to emptyList<String>()) }
    var what = r.o("what") ?: jobj()
    WHAT_LISTS.forEach { if (what[it] !is JsonArray) what = what.with(it to emptyList<String>()) }
    if (!what.has("network")) what = what.with("network" to "")
    val where = (r.o("where") ?: jobj("action" to "proxy")).let { if (!it.has("target")) it.with("target" to "") else it }
    return r.with("who" to who, "what" to what, "where" to where, "enabled" to (if (r.has("enabled")) r.b("enabled") else true))
}

data class Template(val title: String, val desc: String, val rules: List<J>, val first: Boolean = false, val defaultAction: String? = null)

private fun tr(name: String, what: J, action: String) = normRule(jobj("name" to name, "what" to what, "where" to jobj("action" to action)))

val TEMPLATES = listOf(
    Template("Торренты — напрямую, мимо прокси", "qBittorrent, Transmission и другие клиенты на любых портах идут напрямую и не нагружают серверы.",
        listOf(tr("Торренты напрямую", jobj("protocols" to listOf("bittorrent")), "direct")), first = true),
    Template("Заблокированное в России — через прокси (Re:filter)", "Сайты и адреса из реестра блокировок идут через прокси, остальное — напрямую.",
        listOf(tr("Заблокированное в РФ", jobj("lists" to listOf("refilter-domains", "refilter-ips")), "proxy")), defaultAction = "direct"),
    Template("Звонки Telegram, Discord и FaceTime — через прокси", "Мессенджеры и их голосовые и видеозвонки. В Telegram отключите «Peer-to-peer» в звонках.",
        listOf(tr("Звонки мессенджеров", jobj("apps" to listOf("telegram", "discord", "facetime")), "proxy"), tr("FaceTime — голос и видео", jobj("ips" to listOf("17.0.0.0/8"), "network" to "udp"), "proxy"))),
    Template("Всё через прокси, кроме российского", "Банки, Госуслуги и маркетплейсы — напрямую, остальное — через прокси.",
        listOf(tr("Российские ресурсы напрямую", jobj("geosite" to listOf("category-ru"), "geoip" to listOf("ru")), "direct")), defaultAction = "proxy"),
    Template("Через прокси только выбранные сервисы", "YouTube, Instagram, Discord, X, ChatGPT, Claude, Gemini, LinkedIn, Twitch.",
        listOf(tr("Зарубежные сервисы", jobj("apps" to listOf("youtube", "instagram", "discord", "x", "chatgpt", "claude", "gemini", "linkedin", "twitch")), "proxy")), defaultAction = "direct"),
    Template("Блокировка рекламы", "Рекламные и трекинговые домены блокируются для всей сети.",
        listOf(tr("Реклама и трекеры", jobj("geosite" to listOf("category-ads-all")), "block"))),
)

// ---------- торренты одной кнопкой ----------

fun isTorrentRule(r: J) = "bittorrent" in (r.o("what")?.sl("protocols") ?: emptyList()) && r.o("where")?.s("action") == "direct"
fun torrentsOn(rules: List<J>) = rules.any { it.b("enabled") && isTorrentRule(it) }

fun stageTorrents(app: AppState, on: Boolean) {
    app.stage("torrents") {
        val all = Api.list("/rules")
        val ex = all.firstOrNull { isTorrentRule(it) }
        if (ex != null) {
            if (ex.b("enabled") != on) Api.put("/rules/" + ex.id, ex.with("enabled" to on))
            return@stage
        }
        if (!on) return@stage
        val r = Api.post("/rules", TEMPLATES[0].rules[0]) as J
        Api.put("/rules/order", jobj("ids" to listOf(r.id) + all.map { it.id })) // первым — раньше списков
    }
}

/** Правило для сайта, первым в списке. */
suspend fun quickRule(app: AppState, host: String, action: String) {
    val all = Api.list("/rules")
    val r = Api.post("/rules", normRule(jobj("name" to "$host — " + (if (action == "direct") "напрямую" else "через прокси"), "what" to jobj("domains" to listOf(host)), "where" to jobj("action" to action)))) as J
    Api.put("/rules/order", jobj("ids" to listOf(r.id) + all.map { it.id }))
    app.toast("Правило для $host создано и стоит первым", Kind.Ok)
}

// ---------- куда пойдёт сайт ----------

@Composable
fun ExplainView(ex: J) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), itemVerticalAlignment = Alignment.CenterVertically) {
            Text(ex.s("host"), style = MaterialTheme.typography.titleMedium)
            Text("пойдёт", color = MaterialTheme.colorScheme.onSurfaceVariant)
            when (ex.s("action")) {
                "proxy" -> Tag("через прокси · " + ex.s("target"), Tone.Primary, Icons.Rounded.VpnLock)
                "block" -> Tag("блокировка", Tone.Err, Icons.Rounded.Block)
                else -> Tag("напрямую", Tone.Ok, Icons.Rounded.Home)
            }
        }
        Text("Почему: " + ex.s("reason") + (ex.sl("ips").take(2).takeIf { it.isNotEmpty() }?.let { " · адрес " + it.joinToString(", ") } ?: ""), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        val notes = ex.sl("notes")
        if (notes.isNotEmpty()) Text("Учтите: " + notes.joinToString("; "), style = MaterialTheme.typography.bodySmall, color = LocalExtra.current.warn)
    }
}

@Composable
fun ExplainDialog(host: String) {
    val app = LocalApp.current
    var q by remember { mutableStateOf(host) }
    var ex by remember { mutableStateOf<J?>(null) }
    var busy by remember { mutableStateOf(false) }
    val check: () -> Unit = {
        if (q.isNotBlank()) { busy = true; app.launch { try { ex = Api.obj("/route/explain?host=" + encodeURIComponent(q.trim())) } finally { busy = false } } }
    }
    LaunchedEffect(Unit) { if (host.isNotBlank()) check() }
    AppDialog(onDismiss = { app.explainHost = null }, title = "Куда пойдёт сайт", width = 620.dp) { d ->
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Field("Адрес сайта", q, { q = it }, Modifier.weight(1f), placeholder = "parsec.app")
            Button(onClick = check, enabled = !busy, shapes = ButtonDefaults.shapes()) { Text("Проверить") }
        }
        AnimatedContent(ex, transitionSpec = { fadeIn() + expandVertically() togetherWith fadeOut() }) { e ->
            if (e != null) SectionCard(color = MaterialTheme.colorScheme.surfaceContainerLow) {
                ExplainView(e)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (e.s("action") != "proxy") BusyButton("Всегда через прокси") { quickRule(app, e.s("host"), "proxy"); app.refreshPage(); ex = Api.obj("/route/explain?host=" + encodeURIComponent(e.s("host"))) }
                    if (e.s("action") != "direct") BusyButton("Всегда напрямую", style = "outlined") { quickRule(app, e.s("host"), "direct"); app.refreshPage(); ex = Api.obj("/route/explain?host=" + encodeURIComponent(e.s("host"))) }
                }
            }
        }
    }
}

fun encodeURIComponent(s: String): String = encodeUri(s)
private fun encodeUri(s: String): String = js("encodeURIComponent(s)")

// ---------- страница ----------

private class RulesData(val rules: List<J>, val nodes: List<J>, val groups: List<J>, val devices: List<J>, val settings: J, val lists: List<J>, val profiles: List<J>)

@Composable
fun RulesPage() {
    val app = LocalApp.current
    val data by rememberLoad {
        RulesData(Api.list("/rules").map { normRule(it) }, Api.listOrEmpty("/nodes"), Api.listOrEmpty("/groups"), Api.objOrNull("/network")?.a("devices").orEmpty(),
            Api.obj("/settings"), Api.listOrEmpty("/lists"), Api.listOrEmpty("/profiles"))
    }
    val d = data ?: run { Loading(); return }
    var editing by remember { mutableStateOf<J?>(null) }
    var isNew by remember { mutableStateOf(false) }
    var templates by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<J?>(null) }
    val listState = rememberLazyListState()
    val fabExpanded by remember { derivedStateOf { listState.firstVisibleItemIndex == 0 } }

    // Порядок и включение — черновик до «Сохранить».
    val order = remember(d) { mutableStateListOf<J>().apply { addAll(d.rules) } }
    var defaultAction by remember(d) { mutableStateOf(d.settings.s("defaultAction").ifEmpty { "proxy" }) }
    var bypassLan by remember(d) { mutableStateOf(d.settings.b("bypassLan")) }
    var torrents by remember(d) { mutableStateOf(torrentsOn(d.rules)) }
    fun stageSettings() = app.stage("settings") { val cur = Api.obj("/settings"); Api.put("/settings", cur.with("defaultAction" to defaultAction, "bypassLan" to bypassLan)) }

    Box(Modifier.fillMaxSize()) {
        PageList(listState) {
            block("head") {
                PageHeader("Правила", "Проверяются сверху вниз, срабатывает первое подходящее. Порядок меняется перетаскиванием.") {
                    FilledTonalButton(onClick = { templates = true }, shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.AutoAwesome, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text("Сценарии") }
                }
            }
            block("explain") { ExplainBox() }
            block("main-title") { SectionTitle("Основной трафик") }
            block("main") {
                val cards: List<@Composable (Modifier) -> Unit> = listOf(
                    { m -> SectionCard(m.fillMaxHeight(), padding = PaddingValues(20.dp)) {
                        Text("Без подходящего правила", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Connected(listOf("proxy" to "Через прокси", "direct" to "Напрямую"), defaultAction, { defaultAction = it; stageSettings() }, fill = true)
                    } },
                    { m -> Surface(onClick = { app.pickerOpen = true }, modifier = m.fillMaxHeight(), shape = MaterialTheme.shapes.extraLarge, color = MaterialTheme.colorScheme.surfaceContainerLowest) {
                        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text("«Через прокси» — это", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(activeName(d.settings, d.nodes, d.groups), style = MaterialTheme.typography.titleLargeEmphasized, modifier = Modifier.weight(1f))
                                Icon(Icons.Rounded.ChevronRight, null, tint = MaterialTheme.colorScheme.primary)
                            }
                            Text("Активный сервер. В правиле можно выбрать другой.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    } },
                    { m -> SectionCard(m.fillMaxHeight(), padding = PaddingValues(20.dp)) {
                        Text("Всегда", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Row(verticalAlignment = Alignment.CenterVertically) { Text("Локальная сеть напрямую", Modifier.weight(1f)); NpSwitch(bypassLan, { bypassLan = it; stageSettings() }) }
                        Row(verticalAlignment = Alignment.CenterVertically) { Text("Торренты мимо прокси", Modifier.weight(1f)); NpSwitch(torrents, { torrents = it; stageTorrents(app, it) }) }
                    } },
                )
                if (LocalCompact.current) Column(verticalArrangement = Arrangement.spacedBy(12.dp)) { cards.forEach { it(Modifier) } }
                else Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(16.dp)) { cards.forEach { it(Modifier.weight(1f)) } }
            }
            val devRoutes = d.devices.count { it.s("route").isNotEmpty() }
            if (devRoutes > 0) block("devnote") {
                Notice("Устройств с собственным маршрутом: $devRoutes. Для них этот маршрут важнее правил.", Tone.Secondary, Icons.Rounded.Info) {
                    TextButton(onClick = { app.go(Page.Devices) }) { Text("Устройства") }
                }
            }
            block("list-title") { SectionTitle("Правила · ${order.size}") }
            if (order.isEmpty()) block("empty") {
                SectionCard { Empty(Icons.Rounded.AltRoute, "Правил пока нет", "Начните с готового сценария или создайте правило вручную.") { FilledTonalButton(onClick = { templates = true }) { Text("Сценарии") } } }
            }
            orderedRules(order, d, onEdit = { editing = it; isNew = false }, onCopy = { editing = it.with("name" to it.name + " (копия)").let { c -> JsonObjectWithout(c, "id") }; isNew = true }, onDelete = { deleting = it })
        }
        ExtendedFloatingActionButton(
            onClick = { editing = emptyRule(); isNew = true },
            expanded = fabExpanded,
            icon = { Icon(Icons.Rounded.Add, null) },
            text = { Text("Новое правило") },
            modifier = Modifier.align(Alignment.BottomEnd).padding(end = 24.dp, bottom = if (LocalCompact.current) 16.dp else 24.dp),
        )
    }
    editing?.let { r -> RuleEditor(r, isNew, d, onDismiss = { editing = null }) }
    if (templates) TemplatesDialog { templates = false }
    deleting?.let { r ->
        ConfirmDialog("Удалить правило «${r.name}»?", onDismiss = { deleting = null }) { Api.delete("/rules/" + r.id); app.refreshPage() }
    }
}

fun JsonObjectWithout(o: J, key: String): J = J(o.filterKeys { it != key })

/** Список правил: перетаскивание за ручку меняет порядок (черновик). */
private fun androidx.compose.foundation.lazy.LazyListScope.orderedRules(order: MutableList<J>, d: RulesData, onEdit: (J) -> Unit, onCopy: (J) -> Unit, onDelete: (J) -> Unit) {
    order.forEachIndexed { index, r ->
        item(key = "rule-" + r.id) {
            val app = LocalApp.current
            var dragY by remember { mutableFloatStateOf(0f) }
            var h by remember { mutableIntStateOf(1) }
            var dragging by remember { mutableStateOf(false) }
            var enabled by remember(r) { mutableStateOf(r.b("enabled")) }
            val idx = order.indexOfFirst { it.id == r.id }
            Box(
                Modifier.widthIn(max = 1128.dp).fillMaxWidth()
                    .then(if (dragging) Modifier else Modifier.animateItem())
                    .zIndex(if (dragging) 1f else 0f)
                    .graphicsLayer { translationY = if (dragging) dragY else 0f; scaleX = if (dragging) 1.02f else 1f; scaleY = if (dragging) 1.02f else 1f; shadowElevation = if (dragging) 12f else 0f }
                    .onSizeChanged { h = it.height },
            ) {
                SegItem(
                    idx, order.size,
                    modifier = Modifier.alpha(if (enabled) 1f else 0.6f),
                    headline = { Text(r.name.ifEmpty { "Правило" }, fontWeight = FontWeight.Medium) },
                    supporting = { RuleFlow(r, d) },
                    leading = {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Icon(Icons.Rounded.DragIndicator, "Перетащить", tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.pointerInput(r.id) {
                                detectDragGestures(
                                    onDragStart = { dragging = true; dragY = 0f },
                                    onDragEnd = {
                                        dragging = false; dragY = 0f
                                        val ids = order.map { it.id }
                                        if (ids != d.rules.map { it.id }) app.stage("rules-order") { Api.put("/rules/order", jobj("ids" to ids)) } else app.pending.remove("rules-order")
                                    },
                                    onDragCancel = { dragging = false; dragY = 0f },
                                ) { change, amount ->
                                    change.consume()
                                    dragY += amount.y
                                    val cur = order.indexOfFirst { it.id == r.id }
                                    if (dragY > h * 0.6f && cur < order.lastIndex) { order.add(cur + 1, order.removeAt(cur)); dragY -= h }
                                    else if (dragY < -h * 0.6f && cur > 0) { order.add(cur - 1, order.removeAt(cur)); dragY += h }
                                }
                            })
                            ShapeIcon(null, MaterialShapes.Cookie4Sided, text = (idx + 1).toString().padStart(2, '0'), size = 36.dp,
                                container = if (enabled) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surfaceContainerHighest,
                                content = MaterialTheme.colorScheme.onSecondaryContainer, active = dragging)
                        }
                    },
                    trailing = {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            NpSwitch(enabled, { v ->
                                enabled = v
                                val i = order.indexOfFirst { it.id == r.id }
                                if (i >= 0) order[i] = order[i].with("enabled" to v)
                                if (v == r.b("enabled")) app.pending.remove("rule:" + r.id) else app.stage("rule:" + r.id) { Api.put("/rules/" + r.id, r.with("enabled" to v)) }
                            })
                            IconButton(onClick = { onEdit(r) }) { Icon(Icons.Rounded.Edit, "Изменить") }
                            RuleMenu(onCopy = { onCopy(r) }, onDelete = { onDelete(r) })
                        }
                    },
                    onClick = { onEdit(r) },
                )
            }
        }
    }
}

@Composable
private fun RuleMenu(onCopy: () -> Unit, onDelete: () -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { open = true }) { Icon(Icons.Rounded.MoreVert, "Ещё") }
        DropdownMenu(open, onDismissRequest = { open = false }, shape = MaterialTheme.shapes.large) {
            DropdownMenuItem(text = { Text("Копировать") }, leadingIcon = { Icon(Icons.Rounded.ContentCopy, null) }, onClick = { open = false; onCopy() })
            DropdownMenuItem(text = { Text("Удалить", color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Rounded.Delete, null, tint = MaterialTheme.colorScheme.error) }, onClick = { open = false; onDelete() })
        }
    }
}

@Composable
private fun RuleFlow(r: J, d: RulesData) {
    val who = r.o("who") ?: jobj()
    val w = r.o("what") ?: jobj()
    val where = r.o("where") ?: jobj()
    fun label(list: List<Pair<String, String>>, id: String) = list.firstOrNull { it.first == id }?.second ?: id
    FlowRow(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp), itemVerticalAlignment = Alignment.CenterVertically) {
        val whoTags = who.sl("deviceIds").map { id -> d.devices.firstOrNull { it.id == id }?.name ?: "удалено" } + who.sl("tags").map { "группа $it" } + who.sl("cidrs")
        if (whoTags.isEmpty()) Tag("все устройства") else whoTags.forEach { Tag(it) }
        Icon(Icons.Rounded.ArrowForward, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.outline)
        val what = buildList {
            w.sl("apps").forEach { id -> add(d.profiles.firstOrNull { it.id == id }?.name ?: id) }
            w.sl("lists").forEach { id -> add(d.lists.firstOrNull { it.id == id }?.name?.replace(" — заблокированные ", " — ") ?: id) }
            w.sl("protocols").forEach { add(label(PROTOCOLS, it).substringBefore(" (")) }
            w.sl("geosite").forEach { add(label(QUICK_GEOSITE, it)) }
            w.sl("geoip").forEach { add("страна " + label(QUICK_GEOIP, it)) }
            w.sl("domains").let { if (it.isNotEmpty()) add(it[0] + if (it.size > 1) " +${it.size - 1}" else "") }
            w.sl("ips").let { if (it.isNotEmpty()) add(if (it.size == 1) it[0] else "${it.size} подсетей") }
            w.sl("ports").let { if (it.isNotEmpty()) add("порт " + it.joinToString(", ")) }
            if (w.s("network").isNotEmpty()) add(w.s("network").uppercase())
        }
        if (what.isEmpty()) Tag("весь трафик") else what.forEach { Tag(it) }
        Icon(Icons.Rounded.ArrowForward, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.outline)
        when (where.s("action")) {
            "block" -> Tag("блокировать", Tone.Err)
            "direct" -> Tag("напрямую", Tone.Ok)
            else -> Tag("через прокси · " + (if (where.s("target").isNotEmpty()) targetName(where.s("target"), d.nodes, d.groups) else "активный"), Tone.Primary)
        }
    }
}

@Composable
private fun ExplainBox() {
    val app = LocalApp.current
    var q by remember { mutableStateOf("") }
    var ex by remember { mutableStateOf<J?>(null) }
    var busy by remember { mutableStateOf(false) }
    val check: () -> Unit = {
        if (q.isNotBlank()) { busy = true; app.launch { try { ex = Api.obj("/route/explain?host=" + encodeURIComponent(q.trim())) } finally { busy = false } } }
    }
    SectionCard(padding = PaddingValues(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                q, { q = it }, Modifier.weight(1f), singleLine = true, shape = CircleShape,
                leadingIcon = { Icon(Icons.Rounded.TravelExplore, null) },
                placeholder = { Text("Куда пойдёт сайт? Например, parsec.app") },
                keyboardActions = androidx.compose.foundation.text.KeyboardActions(onDone = { check() }, onGo = { check() }, onSearch = { check() }),
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
            )
            Button(onClick = check, enabled = !busy, shapes = ButtonDefaults.shapes()) { Text("Проверить") }
        }
        AnimatedVisibility(ex != null, enter = expandVertically(MaterialTheme.motionScheme.defaultSpatialSpec()) + fadeIn(), exit = shrinkVertically() + fadeOut()) {
            val e = ex ?: return@AnimatedVisibility
            Column(Modifier.padding(horizontal = 8.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                ExplainView(e)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (e.s("action") != "proxy") BusyButton("Пустить через прокси") { quickRule(app, e.s("host"), "proxy"); app.refreshPage() }
                    if (e.s("action") != "direct") BusyButton("Всегда напрямую", style = "outlined") { quickRule(app, e.s("host"), "direct"); app.refreshPage() }
                }
            }
        }
    }
}

// ---------- редактор ----------

@Composable
private fun RuleEditor(orig: J, isNew: Boolean, d: RulesData, onDismiss: () -> Unit) {
    val app = LocalApp.current
    var r by remember { mutableStateOf(normRule(orig)) }
    val who = r.o("who")!!
    val what = r.o("what")!!
    val where = r.o("where")!!
    var whoMode by remember { mutableStateOf(if (who.sl("deviceIds").size + who.sl("tags").size + who.sl("cidrs").size > 0) "pick" else "all") }
    var extra by remember { mutableStateOf(what.sl("geosite").size + what.sl("geoip").size + what.sl("ips").size + what.sl("ports").size > 0 || what.s("network").isNotEmpty()) }
    var domainsText by remember { mutableStateOf(what.sl("domains").joinToString("\n")) }
    var cidrText by remember { mutableStateOf(who.sl("cidrs").joinToString("\n")) }
    var ipsText by remember { mutableStateOf(what.sl("ips").joinToString("\n")) }
    var portsText by remember { mutableStateOf(what.sl("ports").joinToString(", ")) }
    var geositeText by remember { mutableStateOf(what.sl("geosite").joinToString(", ")) }
    var geoipText by remember { mutableStateOf(what.sl("geoip").joinToString(", ")) }
    fun setWhat(k: String, v: Any?) { r = r.withIn("what", k to v) }
    fun setWho(k: String, v: Any?) { r = r.withIn("who", k to v) }
    fun toggle(list: List<String>, v: String, on: Boolean) = if (on) (list + v).distinct() else list - v
    val compact = LocalCompact.current

    AppDialog(onDismiss, if (isNew) "Новое правило" else "Правило", width = 1080.dp, actions = { dlg ->
        TextButton(onClick = { dlg.close {} }) { Text("Отмена") }
        BusyButton(if (isNew) "Создать правило" else "Сохранить") {
            var rule = r.withIn("what", "domains" to lines(domainsText), "ips" to lines(ipsText), "ports" to lines(portsText), "geosite" to lines(geositeText), "geoip" to lines(geoipText))
                .withIn("who", "cidrs" to lines(cidrText))
            if (rule.name.isBlank()) {
                val apps = rule.o("what")!!.sl("apps").mapNotNull { id -> d.profiles.firstOrNull { it.id == id }?.name }
                rule = rule.with("name" to (apps.take(3).joinToString(", ").ifEmpty { rule.o("what")!!.sl("domains").firstOrNull() ?: "Правило" }))
            }
            if (isNew) {
                val created = Api.post("/rules", JsonObjectWithout(rule, "id")) as J
                // Правило с сайтами — первым: иначе его перекроют общие списки.
                if (rule.o("what")!!.sl("domains").isNotEmpty()) Api.put("/rules/order", jobj("ids" to listOf(created.id) + d.rules.map { it.id }))
            } else Api.put("/rules/" + rule.id, rule)
            app.toast(if (isNew) "Правило создано" else "Правило сохранено", Kind.Ok)
            dlg.close { app.refreshPage() }
        }
    }) {
        Field("Название", r.name, { r = r.with("name" to it) }, placeholder = "Например: Parsec через прокси")
        val col1: @Composable ColumnScope.() -> Unit = {
            StepTitle(1, "Кто")
            Text("Для каких устройств действует правило.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Connected(listOf("all" to "Все устройства", "pick" to "Выбрать"), whoMode, { whoMode = it; if (it == "all") { r = r.with("who" to jobj("deviceIds" to emptyList<String>(), "tags" to emptyList<String>(), "cidrs" to emptyList<String>())); cidrText = "" } })
            AnimatedVisibility(whoMode == "pick", enter = expandVertically(MaterialTheme.motionScheme.defaultSpatialSpec()) + fadeIn(), exit = shrinkVertically() + fadeOut()) {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    val devs = d.devices.sortedBy { ipKey(it.s("ip")) }
                    Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLow) {
                        Column(Modifier.heightIn(max = 260.dp).verticalScrollable()) {
                            devs.forEach { dv ->
                                val on = dv.id in r.o("who")!!.sl("deviceIds")
                                ListItem(
                                    headlineContent = { Text(dv.name) }, supportingContent = { Text(dv.s("ip"), fontFamily = LocalMono.current) },
                                    leadingContent = { Checkbox(on, { setWho("deviceIds", toggle(r.o("who")!!.sl("deviceIds"), dv.id, it)) }) },
                                    colors = ListItemDefaults.colors(containerColor = androidx.compose.ui.graphics.Color.Transparent),
                                    modifier = Modifier.clickableNoRipple { setWho("deviceIds", toggle(r.o("who")!!.sl("deviceIds"), dv.id, !on)) },
                                )
                            }
                            if (devs.isEmpty()) Text("Устройств нет", Modifier.padding(16.dp))
                        }
                    }
                    val tags = d.devices.flatMap { it.sl("tags") }.distinct()
                    if (tags.isNotEmpty()) {
                        Text("Группы устройств", style = MaterialTheme.typography.labelLarge)
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { tags.forEach { t -> Chip(t, t in r.o("who")!!.sl("tags")) { setWho("tags", toggle(r.o("who")!!.sl("tags"), t, it)) } } }
                    }
                    Field("Адреса и подсети", cidrText, { cidrText = it }, mono = true, lines = 2, placeholder = "192.168.0.0/24")
                }
            }
        }
        val col2: @Composable ColumnScope.() -> Unit = {
            StepTitle(2, "Что")
            Field("Сайты", domainsText, { domainsText = it }, lines = 3, placeholder = "parsec.app\nfull:api.example.com\nkeyword:tracker", hint = "С поддоменами. full: — точный адрес, keyword: — по слову.")
            Text("Сервисы", style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(0.dp)) {
                d.profiles.forEach { p -> Chip(p.name, p.id in r.o("what")!!.sl("apps")) { setWhat("apps", toggle(r.o("what")!!.sl("apps"), p.id, it)) } }
            }
            Text("Списки и протоколы", style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                d.lists.forEach { l -> Chip(l.name.replace(" — заблокированные ", " — "), l.id in r.o("what")!!.sl("lists")) { setWhat("lists", toggle(r.o("what")!!.sl("lists"), l.id, it)) } }
                PROTOCOLS.forEach { (id, l) -> Chip(l, id in r.o("what")!!.sl("protocols")) { setWhat("protocols", toggle(r.o("what")!!.sl("protocols"), id, it)) } }
            }
            AnimatedContent(extra, transitionSpec = { fadeIn() + expandVertically() togetherWith fadeOut() + shrinkVertically() }) { ex ->
                if (!ex) TextButton(onClick = { extra = true }) { Icon(Icons.Rounded.Add, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Страны, категории, IP и порты") }
                else Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("Категории сайтов", style = MaterialTheme.typography.labelLarge)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { QUICK_GEOSITE.forEach { (v, l) -> Chip(l, v in lines(geositeText)) { geositeText = toggle(lines(geositeText), v, it).joinToString(", ") } } }
                    Field("Категории (geosite)", geositeText, { geositeText = it }, mono = true, placeholder = "youtube, category-ru")
                    Text("Страны по IP", style = MaterialTheme.typography.labelLarge)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { QUICK_GEOIP.forEach { (v, l) -> Chip(l, v in lines(geoipText)) { geoipText = toggle(lines(geoipText), v, it).joinToString(", ") } } }
                    Field("Страны (geoip)", geoipText, { geoipText = it }, mono = true, placeholder = "ru, by")
                    Field("IP-адреса и подсети", ipsText, { ipsText = it }, mono = true, lines = 2, placeholder = "1.2.3.0/24")
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Field("Порты", portsText, { portsText = it }, Modifier.weight(1f), placeholder = "443, 27000-27100")
                        Select("Протокол", listOf("" to "TCP и UDP", "tcp" to "Только TCP", "udp" to "Только UDP"), r.o("what")!!.s("network"), { setWhat("network", it) }, Modifier.weight(1f))
                    }
                }
            }
        }
        val col3: @Composable ColumnScope.() -> Unit = {
            StepTitle(3, "Куда")
            listOf(Triple("proxy", "Через прокси", "Через сервер — по умолчанию активный"), Triple("direct", "Напрямую", "Мимо прокси, через домашний интернет"), Triple("block", "Блокировать", "Устройства не смогут открыть")).forEach { (v, t, sub) ->
                ChoiceCard(where.s("action") == v || (v == "proxy" && where.s("action").isEmpty()), t, sub, when (v) { "proxy" -> Icons.Rounded.VpnLock; "direct" -> Icons.Rounded.Home; else -> Icons.Rounded.Block }) { r = r.withIn("where", "action" to v) }
            }
            AnimatedVisibility(r.o("where")!!.s("action").let { it == "proxy" || it.isEmpty() }, enter = expandVertically() + fadeIn(), exit = shrinkVertically() + fadeOut()) {
                Select("Через какой сервер", targetOptions(d.nodes, d.groups), r.o("where")!!.s("target"), { r = r.withIn("where", "target" to it) }, hint = "Активный сервер — тот, что выбран на «Обзоре». Выберите другой, если этому правилу нужен свой.")
            }
            Text("Правила проверяются сверху вниз, срабатывает первое подходящее. Маршрут устройства важнее правил.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (compact) { Column(verticalArrangement = Arrangement.spacedBy(14.dp)) { col1(); col2(); col3() } }
        else Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(14.dp), content = col1)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(14.dp), content = col2)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(14.dp), content = col3)
        }
    }
}

@Composable
fun StepTitle(n: Int, title: String) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        ShapeIcon(null, MaterialShapes.Cookie9Sided, text = "$n", size = 32.dp, container = MaterialTheme.colorScheme.primary, content = MaterialTheme.colorScheme.onPrimary)
        Text(title, style = MaterialTheme.typography.titleLargeEmphasized)
    }
}

/** Фильтр-чип M3: выбранный — с галочкой. */
@Composable
fun Chip(label: String, selected: Boolean, leading: androidx.compose.ui.graphics.vector.ImageVector? = null, onChange: (Boolean) -> Unit) {
    FilterChip(
        selected = selected, onClick = { onChange(!selected) }, label = { Text(label) },
        leadingIcon = if (selected) ({ Icon(Icons.Rounded.Check, null, Modifier.size(FilterChipDefaults.IconSize)) }) else leading?.let { { Icon(it, null, Modifier.size(FilterChipDefaults.IconSize)) } },
    )
}

/** Выбор варианта карточкой: выбранная скругляется сильнее и окрашивается. */
@Composable
fun ChoiceCard(selected: Boolean, title: String, sub: String, icon: androidx.compose.ui.graphics.vector.ImageVector? = null, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val corner by androidx.compose.animation.core.animateDpAsState(if (selected) 28.dp else 12.dp, MaterialTheme.motionScheme.defaultSpatialSpec())
    val bg by androidx.compose.animation.animateColorAsState(if (selected) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surfaceContainerLow, MaterialTheme.motionScheme.defaultEffectsSpec())
    Surface(onClick = onClick, shape = androidx.compose.foundation.shape.RoundedCornerShape(corner), color = bg, modifier = modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
            RadioButton(selected, onClick)
            Column(Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleMedium)
                Text(sub, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (icon != null) Icon(icon, null, tint = if (selected) MaterialTheme.colorScheme.onSecondaryContainer else MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
fun Modifier.clickableNoRipple(onClick: () -> Unit): Modifier = this.clickable(onClick = onClick)

@Composable
private fun TemplatesDialog(onDismiss: () -> Unit) {
    val app = LocalApp.current
    AppDialog(onDismiss, "Готовые сценарии", width = 680.dp) { dlg ->
        SegGroup {
            TEMPLATES.forEachIndexed { i, t ->
                SegItem(
                    i, TEMPLATES.size,
                    headline = { Text(t.title) },
                    supporting = {
                        Column {
                            Text(t.desc)
                            if (t.defaultAction != null) Text("Трафик без правила: " + if (t.defaultAction == "proxy") "через прокси" else "напрямую", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                        }
                    },
                    trailing = {
                        BusyButton("Добавить", style = "tonal") {
                            val added = t.rules.map { Api.post("/rules", it.with("enabled" to true)) as J }
                            if (t.first) {
                                val all = Api.list("/rules")
                                val ids = added.map { it.id }
                                Api.put("/rules/order", jobj("ids" to ids + all.map { it.id }.filter { it !in ids }))
                            }
                            t.defaultAction?.let { a -> val cur = Api.obj("/settings"); Api.put("/settings", cur.with("defaultAction" to a)) }
                            app.toast("Сценарий добавлен", Kind.Ok)
                            dlg.close { app.refreshPage() }
                        }
                    },
                )
            }
        }
    }
}
