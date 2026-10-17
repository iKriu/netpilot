package netpilot

// Серверы: группы, подписки, список с проверкой по одному, выбор активного сервера.

import androidx.compose.animation.*
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

val INTERVALS = listOf(0 to "Вручную", 1 to "Каждый час", 3 to "Каждые 3 часа", 6 to "Каждые 6 часов", 12 to "Каждые 12 часов", 24 to "Раз в сутки", 72 to "Раз в 3 дня", 168 to "Раз в неделю")
val PING_METHODS = listOf(
    Triple("auto", "Автоматически", "Через сервер, если шлюз запущен; иначе TCP"),
    Triple("http", "HTTP через сервер", "Настоящая задержка: запрос идёт через сам сервер. Нужен запущенный шлюз"),
    Triple("tcp", "TCP", "Время подключения к порту сервера. Быстро, но не проверяет, что сервер пропускает трафик; не для Hysteria2 и TUIC"),
    Triple("icmp", "ICMP (ping)", "Обычный ping адреса сервера. Подходит для любых протоколов; некоторые серверы на ping не отвечают"),
)
fun pingMethodName(v: String) = PING_METHODS.firstOrNull { it.first == v.ifEmpty { "auto" } }?.second ?: "Автоматически"

private class ServersData(val nodes: List<J>, val subs: List<J>, val groups: List<J>, val settings: J, val auto: J?)

/** Проверка серверов по одному: общее состояние, чтобы переживало перерисовку. */
class TestRun(val total: Int) {
    var done by mutableIntStateOf(0)
    var current by mutableStateOf("")
    var stop by mutableStateOf(false)
}

@Composable
fun ServersPage() {
    val app = LocalApp.current
    val data by rememberLoad {
        val auto = Api.objOrNull("/auto")
        auto?.o("latency")?.forEach { (id, l) -> if (id !in app.latency) (l as? J)?.let { app.latency[id] = Latency.of(it) } }
        ServersData(Api.list("/nodes"), Api.listOrEmpty("/subscriptions"), Api.listOrEmpty("/groups"), Api.obj("/settings"), auto)
    }
    val d = data ?: run { Loading(); return }
    var filter by remember { mutableStateOf("all") }
    var q by remember { mutableStateOf("") }
    var shown by remember { mutableIntStateOf(100) }
    var run by remember { mutableStateOf<TestRun?>(null) }
    var dialog by remember { mutableStateOf<String?>(null) }
    var editGroup by remember { mutableStateOf<J?>(null) }
    var pickGroup by remember { mutableStateOf<J?>(null) }
    var editSub by remember { mutableStateOf<J?>(null) }
    var editNode by remember { mutableStateOf<J?>(null) }
    var confirm by remember { mutableStateOf<Pair<String, suspend () -> Unit>?>(null) }
    var fabOpen by remember { mutableStateOf(false) }
    val autoOf = { id: String -> d.auto?.a("groups")?.firstOrNull { it.s("groupId") == id } }
    val nodeName = { id: String -> d.nodes.firstOrNull { it.id == id }?.name ?: "" }
    val selected = d.auto?.a("groups")?.map { it.s("selected") }?.filter { it.isNotEmpty() }?.toSet().orEmpty()
    val okCount = d.nodes.count { (app.latency[it.id]?.ms ?: 0) > 0 }
    val visible = d.nodes.filter { n ->
        val qq = q.lowercase()
        (qq.isEmpty() || n.name.lowercase().contains(qq) || n.s("server").contains(qq) || n.s("protocol").contains(qq)) &&
            when (filter) { "ok" -> (app.latency[n.id]?.ms ?: 0) > 0; "nolte" -> !Regex("lte", RegexOption.IGNORE_CASE).containsMatchIn(n.name); else -> true }
    }

    suspend fun testOne(id: String): Boolean {
        app.latency[id] = Latency.Wait
        return try {
            val res = Api.call("POST", "/nodes/test", toJson(jobj("ids" to listOf(id)))) as kotlinx.serialization.json.JsonArray
            (res.firstOrNull() as? J)?.let { app.latency[id] = Latency.of(it) }
            true
        } catch (e: ApiError) {
            app.latency.remove(id); app.toast(e.message ?: "", Kind.Err); false
        }
    }
    fun startTest() {
        val ids = visible.map { it.id }
        val r = TestRun(ids.size)
        run = r
        app.launch {
            for (id in ids) {
                if (r.stop) break
                r.current = nodeName(id)
                if (!testOne(id)) break
                r.done++
            }
            val ok = ids.count { (app.latency[it]?.ms ?: 0) > 0 }
            app.toast("Проверено: ${r.done} из ${r.total}, отвечают: $ok", Kind.Ok, if (r.stop) "Проверка остановлена" else "Проверка завершена")
            run = null
        }
    }
    suspend fun makeActive(id: String) {
        val cur = Api.obj("/settings")
        Api.put("/settings", cur.with("defaultTarget" to id))
        app.toast("Активный сервер изменён", Kind.Ok)
        app.refreshActive()
        app.refreshPage()
    }
    val listState = rememberLazyListState()
    val compact = LocalCompact.current

    val header: @Composable () -> Unit = {
        PageHeader("Серверы", "${plural(d.nodes.size, "сервер", "сервера", "серверов")} · ${plural(d.subs.size, "подписка", "подписки", "подписок")} · ${plural(d.groups.size, "группа", "группы", "групп")} · проверка: ${pingMethodName(d.settings.s("pingMethod"))}") {
            val r = run
            if (r == null) FilledTonalButton(onClick = { startTest() }, enabled = d.nodes.isNotEmpty(), shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.NetworkCheck, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text("Проверить все") }
            else OutlinedButton(onClick = { r.stop = true }, shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.Stop, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text("Остановить") }
        }
    }
    val progress: @Composable () -> Unit = {
        AnimatedVisibility(run != null, enter = expandVertically(MaterialTheme.motionScheme.defaultSpatialSpec()) + fadeIn(), exit = shrinkVertically() + fadeOut()) {
            val r = run ?: return@AnimatedVisibility
            val p by animateFloatAsState(if (r.total == 0) 0f else r.done.toFloat() / r.total, MaterialTheme.motionScheme.defaultSpatialSpec())
            SectionCard(color = MaterialTheme.colorScheme.secondaryContainer) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    LoadingIndicator(Modifier.size(40.dp))
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("Проверка по одному: ${r.done} из ${r.total}", style = MaterialTheme.typography.titleMedium)
                        LinearWavyProgressIndicator(progress = { p }, modifier = Modifier.fillMaxWidth())
                        Text("отвечают $okCount" + if (r.current.isNotEmpty()) " · сейчас ${r.current}" else "", style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
    val newGroup = { jobj("name" to "", "type" to "auto", "nodeIds" to emptyList<String>(), "filter" to "", "exclude" to "") }
    val groupsCol: @Composable ColumnScope.() -> Unit = {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Группы", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = { editGroup = newGroup() }) { Icon(Icons.Rounded.Add, null, Modifier.size(18.dp)); Text(" Новая") }
        }
        d.groups.forEach { g -> GroupCard(g, d, autoOf(g.id), nodeName, onEdit = { editGroup = g }, onPick = { pickGroup = g }, onActivate = { app.launch { makeActive(g.id) } }) }
        if (d.nodes.size > 1) {
            val b = autoOf("")
            val active = d.settings.s("defaultTarget").isEmpty()
            SectionCard(color = if (active) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surfaceContainerLowest) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Авто — все серверы", style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                    if (active) Tag("активный", Tone.Inverse) else FilledTonalButton(onClick = { app.launch { makeActive("") } }, shapes = ButtonDefaults.shapes()) { Text("Сделать активной") }
                }
                val sel = b?.s("selected").orEmpty()
                Text(plural(d.nodes.size, "сервер", "сервера", "серверов") + ", включая Москву и LTE" + (if (active && sel.isNotEmpty()) " · сейчас " + nodeName(sel) else ""), style = MaterialTheme.typography.bodySmall)
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 8.dp)) {
            Text("Подписки", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = { editSub = jobj("name" to "", "url" to "", "interval" to 24) }) { Icon(Icons.Rounded.Add, null, Modifier.size(18.dp)); Text(" Новая") }
        }
        if (d.subs.isEmpty()) SectionCard { Text("Подписка — адрес от провайдера, по которому список серверов обновляется сам.", style = MaterialTheme.typography.bodyMedium) }
        d.subs.forEach { s ->
            SubCard(s, d.nodes.count { it.s("subscriptionId") == s.id }, onEdit = { editSub = s }, onDelete = {
                confirm = "Удалить подписку «${s.name}» вместе с её серверами?" to { Api.post("/subscriptions/${s.id}/purge"); app.refreshCounts(); app.refreshPage() }
            })
        }
    }
    val listHead: @Composable () -> Unit = { ServerListHead(d, filter, { filter = it; shown = 100 }, q, { q = it; shown = 100 }, okCount) }
    val vs = visible.take(shown)
    val listItems: androidx.compose.foundation.lazy.LazyListScope.() -> Unit = {
        vs.forEachIndexed { i, n ->
            item(key = "n-" + n.id) {
                Box(Modifier.widthIn(max = 1128.dp).fillMaxWidth().animateItem()) {
                    SegItem(
                        i, vs.size,
                        selected = n.id in selected,
                        headline = { Text(n.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        supporting = {
                            val proto = listOfNotNull(n.s("protocol"), n.s("security").takeIf { it.isNotEmpty() && it != "none" } ?: n.s("transport").takeIf { it.isNotEmpty() && it != "tcp" }).filter { it.isNotEmpty() }.joinToString(" · ")
                            val where = if (n.id in selected) "работает в группе" else d.subs.firstOrNull { it.id == n.s("subscriptionId") }?.name
                            Text(listOfNotNull(proto, where).joinToString(" · "), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        },
                        trailing = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Box(Modifier.widthIn(min = 100.dp)) { LatencyView(app.latency[n.id]) }
                                IconButton(onClick = { app.launch { testOne(n.id) } }, enabled = run == null) { Icon(Icons.Rounded.NetworkCheck, "Проверить") }
                                if (!compact) IconButton(onClick = { confirm = "Удалить сервер «${n.name}»?" to { Api.delete("/nodes/" + n.id); app.refreshCounts(); app.refreshPage() } }) { Icon(Icons.Rounded.Delete, "Удалить") }
                            }
                        },
                        onClick = { editNode = n },
                    )
                }
            }
        }
        if (visible.size > shown) item("more") { TextButton(onClick = { shown += 300 }, modifier = Modifier.fillMaxWidth()) { Text("Показать ещё ${minOf(300, visible.size - shown)} из ${visible.size - shown}") } }
        if (visible.isEmpty()) item("empty") {
            Empty(Icons.Rounded.Dns, if (d.nodes.isEmpty()) "Серверов нет" else "Ничего не найдено", if (d.nodes.isEmpty()) "Импортируйте ссылки или добавьте подписку провайдера." else "Измените фильтр или поиск.")
        }
    }

    Box(Modifier.fillMaxSize()) {
        if (compact) {
            PageList(listState) {
                block("head") { header() }
                block("progress") { progress() }
                block("groups") { Column(verticalArrangement = Arrangement.spacedBy(12.dp), content = groupsCol) }
                block("listhead") { listHead() }
                listItems()
            }
        } else {
            Box(Modifier.fillMaxSize().padding(start = 8.dp, end = 24.dp), contentAlignment = Alignment.TopCenter) {
                Column(Modifier.widthIn(max = 1128.dp).fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                    header()
                    progress()
                    Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        Column(Modifier.width(360.dp).fillMaxHeight().verticalScrollable().padding(bottom = 140.dp), verticalArrangement = Arrangement.spacedBy(12.dp), content = groupsCol)
                        LazyColumn(Modifier.weight(1f).fillMaxHeight().springScroll(listState), state = listState, verticalArrangement = Arrangement.spacedBy(ListItemDefaults.SegmentedGap), contentPadding = PaddingValues(bottom = 140.dp)) {
                            item("listhead") { Box(Modifier.padding(bottom = 12.dp)) { listHead() } }
                            listItems()
                        }
                    }
                }
            }
        }
        // Меню кнопки «Добавить» (FAB menu M3E).
        FloatingActionButtonMenu(
            expanded = fabOpen,
            button = {
                ToggleFloatingActionButton(checked = fabOpen, onCheckedChange = { fabOpen = it }, modifier = Modifier.semantics { contentDescription = "Добавить" }) {
                    val icon = if (checkedProgress > 0.5f) Icons.Rounded.Close else Icons.Rounded.Add
                    Icon(icon, null, with(ToggleFloatingActionButtonDefaults) { Modifier.animateIcon({ checkedProgress }) })
                }
            },
            modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = if (compact) 4.dp else 12.dp),
        ) {
            FloatingActionButtonMenuItem(onClick = { fabOpen = false; dialog = "import" }, icon = { Icon(Icons.Rounded.Link, null) }, text = { Text("Ссылки на серверы") })
            FloatingActionButtonMenuItem(onClick = { fabOpen = false; editSub = jobj("name" to "", "url" to "", "interval" to 24) }, icon = { Icon(Icons.Rounded.RssFeed, null) }, text = { Text("Подписка") })
            FloatingActionButtonMenuItem(onClick = { fabOpen = false; editGroup = jobj("name" to "", "type" to "auto", "nodeIds" to emptyList<String>(), "filter" to "", "exclude" to "") }, icon = { Icon(Icons.Rounded.Workspaces, null) }, text = { Text("Группа") })
        }
    }

    if (dialog == "import") ImportDialog { dialog = null }
    editSub?.let { s -> SubDialog(s) { editSub = null } }
    editGroup?.let { g -> GroupEditor(g, d.nodes, d.settings, onDelete = { confirm = it }) { editGroup = null } }
    pickGroup?.let { g -> PickInGroup(g, d.nodes, autoOf(g.id)) { pickGroup = null } }
    editNode?.let { n -> NodeDialog(n) { editNode = null } }
    confirm?.let { (text, act) -> ConfirmDialog(text, onDismiss = { confirm = null }) { act() } }
}

@Composable
private fun ServerListHead(d: ServersData, filter: String, onFilter: (String) -> Unit, q: String, onQ: (String) -> Unit, okCount: Int) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(q, onQ, Modifier.fillMaxWidth(), singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) }, placeholder = { Text("Страна, название, протокол") })
        Connected(listOf("all" to "Все", "ok" to "Отвечают", "nolte" to "Без LTE"), filter, onFilter, counts = mapOf("all" to d.nodes.size, "ok" to okCount))
    }
}

@Composable
private fun GroupCard(g: J, d: ServersData, ag: J?, nodeName: (String) -> String, onEdit: () -> Unit, onPick: () -> Unit, onActivate: () -> Unit) {
    val m = groupMembers(g, d.nodes)
    val isActive = d.settings.s("defaultTarget") == g.id
    val manual = g.s("type") == "select"
    val cs = MaterialTheme.colorScheme
    var hover by remember { mutableStateOf(false) }
    val corner by androidx.compose.animation.core.animateDpAsState(if (isActive || hover) 32.dp else 24.dp, MaterialTheme.motionScheme.defaultSpatialSpec())
    Surface(shape = androidx.compose.foundation.shape.RoundedCornerShape(corner), color = if (isActive) cs.secondaryContainer else cs.surfaceContainerLowest, modifier = Modifier.fillMaxWidth().onHover { hover = it }) {
        Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                ShapeIcon(if (manual) Icons.Rounded.TouchApp else Icons.Rounded.AutoMode, MaterialShapes.Cookie9Sided, MaterialShapes.Cookie12Sided, active = isActive || hover,
                    container = if (isActive) cs.primary else cs.primaryContainer, content = if (isActive) cs.onPrimary else cs.onPrimaryContainer)
                Text(g.name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                if (isActive) Tag("активная", Tone.Inverse)
                IconButton(onClick = onEdit) { Icon(Icons.Rounded.Edit, "Изменить группу") }
            }
            if (g.s("filter").isNotEmpty()) Text(g.s("filter").split('|').joinToString(" · "), style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (m.isNotEmpty()) Tag(plural(m.size, "сервер", "сервера", "серверов")) else Tag("пустая", Tone.Warn)
                if (g.s("exclude").isNotEmpty()) Tag("кроме " + g.s("exclude"), Tone.Err)
                Tag(if (manual) "выбор вручную" else "автовыбор")
            }
            if (m.isNotEmpty()) {
                HorizontalDivider()
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (manual || ag?.b("inUse") == true) {
                        Dot(if ((ag?.i("ms") ?: 0) > 0) LocalExtra.current.ok else if ((ag?.i("ms") ?: 0) < 0) cs.error else cs.outline)
                        val sc = ag?.il("scanning").orEmpty()
                        Text(if (sc.size == 2) "подбор: ${sc[0]} из ${sc[1]}" else "Сейчас " + (ag?.s("selected")?.let(nodeName)?.ifEmpty { null } ?: "выбирается"), Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if ((ag?.i("ms") ?: 0) != 0) LatencyView(Latency(ag!!.i("ms"), "http"))
                    } else Text("не используется — не проверяется", Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = cs.onSurfaceVariant)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = onPick, shapes = ButtonDefaults.shapes()) { Text("Выбрать сервер") }
                    if (!isActive) FilledTonalButton(onClick = onActivate, shapes = ButtonDefaults.shapes()) { Text("Сделать активной") }
                }
            }
        }
    }
}

@Composable
private fun SubCard(s: J, count: Int, onEdit: () -> Unit, onDelete: () -> Unit) {
    val app = LocalApp.current
    SectionCard {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            ShapeIcon(Icons.Rounded.RssFeed, MaterialShapes.Sunny, container = LocalExtra.current.okContainer, content = LocalExtra.current.onOkContainer)
            Text(s.name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            if (s.s("lastError").isNotEmpty()) Tag("ошибка", Tone.Err) else Tag("обновлена", Tone.Ok)
        }
        Text("${plural(count, "сервер", "сервера", "серверов")} · ${ago(s.l("updatedAt"))} · " + (INTERVALS.firstOrNull { it.first == s.i("interval") }?.second?.lowercase() ?: "каждые ${s.i("interval")} ч"), style = MaterialTheme.typography.bodySmall)
        if (s.s("lastError").isNotEmpty()) Text(s.s("lastError"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            BusyButton("Обновить", Icons.Rounded.Refresh, style = "tonal") { updateSub(app, s.id); app.refreshCounts(); app.refreshPage() }
            TextButton(onClick = onEdit) { Text("Изменить") }
            TextButton(onClick = onDelete, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Удалить") }
        }
    }
}

suspend fun updateSub(app: AppState, id: String) {
    try {
        val r = Api.post("/subscriptions/$id/update") as J
        app.toast("Загружено серверов: ${r.i("nodes")}", Kind.Ok, "Подписка обновлена")
    } catch (e: ApiError) {
        app.toast(e.message ?: "", Kind.Err, "Подписка не обновилась")
    }
}

// ---------- окна ----------

@Composable
private fun ImportDialog(onDismiss: () -> Unit) {
    val app = LocalApp.current
    var text by remember { mutableStateOf("") }
    AppDialog(onDismiss, "Добавить серверы", width = 640.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        BusyButton("Добавить", enabled = text.isNotBlank()) {
            val r = Api.post("/nodes/import", jobj("text" to text)) as J
            app.toast("Добавлено серверов: ${r.i("added")}", if (r.i("added") > 0) Kind.Ok else Kind.Warn)
            r.sl("errors").take(3).forEach { app.toast(it, Kind.Err) }
            app.refreshCounts()
            d.close { app.refreshPage() }
        }
    }) {
        Field("Ссылки на серверы, по одной в строке", text, { text = it }, mono = true, lines = 8, placeholder = "vless://…\nvmess://…\ntrojan://…\nss://…\nhysteria2://…\ntuic://…",
            hint = "VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC. Можно вставить содержимое подписки в base64.")
    }
}

@Composable
private fun SubDialog(s0: J, onDismiss: () -> Unit) {
    val app = LocalApp.current
    val isNew = s0.id.isEmpty()
    var name by remember { mutableStateOf(s0.name) }
    var url by remember { mutableStateOf(s0.s("url")) }
    var interval by remember { mutableIntStateOf(if (isNew) 24 else s0.i("interval")) }
    var custom by remember { mutableStateOf(INTERVALS.none { it.first == interval }) }
    var customText by remember { mutableStateOf(interval.toString()) }
    AppDialog(onDismiss, if (isNew) "Новая подписка" else "Подписка", width = 600.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        BusyButton(if (isNew) "Добавить" else "Сохранить") {
            if (url.isBlank()) throw ApiError("Укажите адрес подписки")
            val iv = if (custom) customText.toIntOrNull()?.takeIf { it in 1..720 } ?: throw ApiError("Интервал — от 1 до 720 часов") else interval
            val body = s0.with("name" to name.trim().ifEmpty { "Подписка" }, "url" to url.trim(), "interval" to iv)
            if (isNew) {
                val sub = Api.post("/subscriptions", JsonObjectWithout(body, "id")) as J
                updateSub(app, sub.id)
            } else {
                Api.put("/subscriptions/" + s0.id, body)
                if (url.trim() != s0.s("url")) updateSub(app, s0.id) else app.toast("Подписка сохранена", Kind.Ok)
            }
            app.refreshCounts()
            d.close { app.refreshPage() }
        }
    }) {
        Field("Название", name, { name = it }, placeholder = "Основной провайдер")
        Field("Адрес подписки", url, { url = it }, mono = true, placeholder = "https://…")
        Select("Автообновление", INTERVALS.map { it.first to it.second } + (-1 to "Свой интервал…"), if (custom) -1 else interval, { v -> if (v == -1) custom = true else { custom = false; interval = v } },
            hint = "Если список серверов изменился, конфигурация применяется сама; правила и группы сохраняются.")
        AnimatedVisibility(custom) { Field("Каждые, часов", customText, { customText = it.filter { c -> c.isDigit() } }, number = true, hint = "От 1 до 720") }
    }
}

@Composable
private fun NodeDialog(n: J, onDismiss: () -> Unit) {
    val app = LocalApp.current
    var name by remember { mutableStateOf(n.name) }
    var server by remember { mutableStateOf(n.s("server")) }
    var port by remember { mutableStateOf(n.i("port").toString()) }
    val rest = J(n.filterKeys { it !in setOf("id", "name", "server", "port") })
    var raw by remember { mutableStateOf(JsonPretty.encodeToString(kotlinx.serialization.json.JsonObject.serializer(), rest)) }
    AppDialog(onDismiss, "Сервер", width = 640.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        BusyButton("Сохранить") {
            val extra = runCatching { JsonFmt.parseToJsonElement(raw) as J }.getOrElse { throw ApiError("Ошибка в JSON") }
            Api.put("/nodes/" + n.id, extra.with("name" to name, "server" to server, "port" to (port.toIntOrNull() ?: 0)))
            d.close { app.refreshPage() }
        }
    }) {
        Field("Название", name, { name = it })
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Field("Адрес", server, { server = it }, Modifier.weight(2f), mono = true)
            Field("Порт", port, { port = it.filter { c -> c.isDigit() } }, Modifier.weight(1f), number = true)
        }
        Field("Параметры подключения", raw, { raw = it }, mono = true, lines = 10, hint = "Протокол, ключи, транспорт и TLS в формате JSON")
    }
}

val JsonPretty = kotlinx.serialization.json.Json { prettyPrint = true }

// ---------- группа ----------

@Composable
private fun GroupEditor(g0: J, nodes: List<J>, settings: J, onDelete: (Pair<String, suspend () -> Unit>) -> Unit, onDismiss: () -> Unit) {
    val app = LocalApp.current
    val isNew = g0.id.isEmpty()
    var name by remember { mutableStateOf(g0.name) }
    var type by remember { mutableStateOf(g0.s("type").ifEmpty { "auto" }) }
    var filter by remember { mutableStateOf(g0.s("filter")) }
    var exclude by remember { mutableStateOf(g0.s("exclude")) }
    val picked = remember { mutableStateListOf<String>().apply { addAll(g0.sl("nodeIds").filter { id -> nodes.any { it.id == id } }) } }
    var q by remember { mutableStateOf("") }
    var show by remember { mutableStateOf("all") }
    var shown by remember { mutableIntStateOf(150) }
    val g = jobj("name" to name, "filter" to filter, "exclude" to exclude, "nodeIds" to picked.toList())
    val members = groupMembers(g, nodes)
    val memberIds = members.map { it.id }.toSet()
    val re = runCatching { if (filter.isBlank()) null else Regex(filter, RegexOption.IGNORE_CASE) }.getOrNull()
    val ex = runCatching { if (exclude.isBlank()) null else Regex(exclude, RegexOption.IGNORE_CASE) }.getOrNull()
    val visible = nodes.filter { n ->
        (q.isEmpty() || n.name.lowercase().contains(q.lowercase()) || n.s("server").contains(q) || n.s("protocol").contains(q.lowercase())) &&
            when (show) { "picked" -> n.id in picked; "members" -> n.id in memberIds; else -> true }
    }
    val byFilter = if (re != null) nodes.count { re.containsMatchIn(it.name) && it.id !in picked && !(ex?.containsMatchIn(it.name) ?: false) } else 0
    val excluded = if (ex != null) nodes.count { ex.containsMatchIn(it.name) } else 0
    val compact = LocalCompact.current

    AppDialog(onDismiss, if (isNew) "Новая группа" else "Группа", width = 1000.dp, actions = { d ->
        if (!isNew) TextButton(onClick = {
            onDelete("Удалить группу «${g0.name}»?" + (if (settings.s("defaultTarget") == g0.id) " Это активный сервер — после удаления трафик пойдёт через «Авто — все серверы»." else "") to {
                Api.delete("/groups/" + g0.id)
                if (settings.s("defaultTarget") == g0.id) { val cur = Api.obj("/settings"); Api.put("/settings", cur.with("defaultTarget" to "")) }
                app.refreshActive()
                d.close { app.refreshPage() }
            })
        }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Удалить") }
        Spacer(Modifier.weight(1f))
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        BusyButton(if (isNew) "Создать группу" else "Сохранить") {
            if (name.isBlank()) throw ApiError("Укажите название группы")
            (regexError(filter).ifEmpty { regexError(exclude) }).takeIf { it.isNotEmpty() }?.let { throw ApiError(it) }
            val body = g0.with("name" to name.trim(), "type" to type, "filter" to filter, "exclude" to exclude, "nodeIds" to picked.toList())
            if (isNew) Api.post("/groups", JsonObjectWithout(body, "id")) else Api.put("/groups/" + g0.id, body)
            app.toast(if (isNew) "Группа создана. Чтобы она заработала, сделайте её активным сервером или укажите в правиле." else "Группа сохранена", Kind.Ok)
            d.close { app.refreshPage() }
        }
    }) {
        val top: @Composable (Modifier) -> Unit = { m -> Field("Название", name, { name = it }, m, placeholder = "Европа") }
        val kind: @Composable (Modifier) -> Unit = { m ->
            Column(m, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("Выбор сервера в группе", style = MaterialTheme.typography.labelLarge)
                Connected(listOf("auto" to "Автоматически", "select" to "Вручную"), type, { type = it }, fill = true)
                Text(if (type == "auto") "NetPilot проверяет серверы по одному и держится за рабочий." else "Сервер выбираете вы кнопкой «Выбрать сервер».", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (compact) { top(Modifier); kind(Modifier) } else Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) { top(Modifier.weight(1f)); kind(Modifier.weight(1f)) }

        Text("Серверы группы", style = MaterialTheme.typography.titleMedium)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(8.dp), itemVerticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(q, { q = it; shown = 150 }, Modifier.widthIn(min = 260.dp), singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) }, placeholder = { Text("Найти сервер") })
            Connected(listOf("all" to "Все", "picked" to "Отмеченные", "members" to "В группе"), show, { show = it; shown = 150 }, counts = mapOf("all" to nodes.size, "picked" to picked.size, "members" to members.size))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilledTonalButton(onClick = { visible.forEach { if (it.id !in picked) picked.add(it.id) } }, shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.DoneAll, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Отметить найденные") }
            TextButton(onClick = { val ids = visible.map { it.id }.toSet(); picked.removeAll { it in ids } }) { Text("Снять отметки с найденных") }
        }
        Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLow) {
            Column(Modifier.heightIn(max = 380.dp).verticalScrollable()) {
                visible.take(shown).forEach { n ->
                    val on = n.id in picked
                    val why = when {
                        ex?.containsMatchIn(n.name) == true -> "исключён" to Tone.Err
                        !on && re?.containsMatchIn(n.name) == true -> "по фильтру" to Tone.Primary
                        n.id in memberIds && !on && filter.isBlank() && picked.isEmpty() -> "все серверы" to Tone.Neutral
                        else -> null
                    }
                    ListItem(
                        headlineContent = { Text(n.name, maxLines = 1, overflow = TextOverflow.Ellipsis, color = if (n.id in memberIds) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant) },
                        supportingContent = { Text(n.s("protocol")) },
                        leadingContent = { Checkbox(on, { c -> if (c) picked.add(n.id) else picked.remove(n.id) }) },
                        trailingContent = { Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) { why?.let { Tag(it.first, it.second) }; LatencyView(app.latency[n.id]) } },
                        colors = ListItemDefaults.colors(containerColor = androidx.compose.ui.graphics.Color.Transparent),
                        modifier = Modifier.clickable { if (on) picked.remove(n.id) else picked.add(n.id) },
                    )
                }
                if (visible.size > shown) TextButton(onClick = { shown += 300 }, modifier = Modifier.fillMaxWidth()) { Text("Показать ещё ${minOf(300, visible.size - shown)} из ${visible.size - shown}") }
                if (visible.isEmpty()) Text("Ничего не найдено", Modifier.padding(16.dp))
            }
        }
        val filterBlock: @Composable (Modifier) -> Unit = { m -> Field("И по названию (фильтр)", filter, { filter = it }, m, mono = true, placeholder = "Германия|Нидерланды", error = regexError(filter), hint = "Необязательно. Новые серверы из подписок с таким названием попадут в группу сами.") }
        val excludeBlock: @Composable (Modifier) -> Unit = { m -> Field("Исключить по названию", exclude, { exclude = it }, m, mono = true, placeholder = "LTE", error = regexError(exclude), hint = "Эти серверы не попадут в группу, даже если отмечены или подошли по фильтру.") }
        if (compact) { filterBlock(Modifier); excludeBlock(Modifier) } else Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) { filterBlock(Modifier.weight(1f)); excludeBlock(Modifier.weight(1f)) }
        Surface(shape = MaterialTheme.shapes.large, color = if (members.isEmpty()) LocalExtra.current.warnContainer else MaterialTheme.colorScheme.secondaryContainer) {
            Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                AnimatedContent(members.size, transitionSpec = { (slideInVertically { -it } + fadeIn()) togetherWith (slideOutVertically { it } + fadeOut()) }) { c ->
                    Text(if (c > 0) "В группе " + plural(c, "сервер", "сервера", "серверов") else "Группа пустая", style = MaterialTheme.typography.titleMedium)
                }
                Text(
                    if (picked.isEmpty() && filter.isBlank()) "Ничего не отмечено и фильтр пуст — в группе все серверы" + (if (excluded > 0) ", кроме исключённых ($excluded)" else "")
                    else listOfNotNull(if (picked.isNotEmpty()) "отмечено: ${picked.size}" else null, if (byFilter > 0) "по фильтру: $byFilter" else null, if (excluded > 0) "исключено: $excluded" else null).joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall,
                )
                if (members.isEmpty()) Text("Пустая группа в ядро не попадёт: правила и устройства с ней пойдут через активный сервер.", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
private fun PickInGroup(g: J, nodes: List<J>, ag: J?, onDismiss: () -> Unit) {
    val app = LocalApp.current
    val members = groupMembers(g, nodes)
    var picked by remember { mutableStateOf(ag?.s("selected")?.ifEmpty { null } ?: members.firstOrNull()?.id.orEmpty()) }
    var q by remember { mutableStateOf("") }
    AppDialog(onDismiss, "Сервер в группе «${g.name}»", width = 640.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        BusyButton("Выбрать", enabled = picked.isNotEmpty()) {
            Api.post("/groups/${g.id}/select", jobj("nodeId" to picked))
            app.toast("В группе «${g.name}» выбран " + (nodes.firstOrNull { it.id == picked }?.name ?: ""), Kind.Ok)
            d.close { app.refreshPage() }
        }
    }) {
        Text(if (g.s("type") == "select") "Группа с ручным выбором: работает выбранный здесь сервер." else "Автоматическая группа будет держаться за выбранный сервер, пока он отвечает.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        OutlinedTextField(q, { q = it }, Modifier.fillMaxWidth(), singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) }, placeholder = { Text("Найти сервер") })
        val vs = members.filter { q.isEmpty() || it.name.lowercase().contains(q.lowercase()) }.take(300)
        SegGroup {
            vs.forEachIndexed { i, n ->
                SegItem(i, vs.size, selected = picked == n.id, headline = { Text(n.name) }, leading = { RadioButton(picked == n.id, { picked = n.id }) }, trailing = { LatencyView(app.latency[n.id]) }, onClick = { picked = n.id })
            }
        }
    }
}

// ---------- активный сервер ----------

@Composable
fun ActivePicker() {
    val app = LocalApp.current
    val data by rememberLoad { Triple(Api.obj("/settings"), Api.listOrEmpty("/nodes") to Api.listOrEmpty("/groups"), Api.objOrNull("/auto")) }
    var picked by remember { mutableStateOf<String?>(null) }
    var tab by remember { mutableStateOf("groups") }
    var q by remember { mutableStateOf("") }
    AppDialog(onDismiss = { app.pickerOpen = false }, title = "Активный сервер", width = 640.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        val dd = data
        BusyButton("Сделать активным", enabled = dd != null && picked != null && picked != dd.first.s("defaultTarget")) {
            val cur = Api.obj("/settings")
            Api.put("/settings", cur.with("defaultTarget" to (picked ?: "")))
            app.toast("Активный сервер изменён", Kind.Ok)
            app.refreshActive()
            d.close { app.refreshPage() }
        }
    }) {
        val dd = data ?: run { Loading(); return@AppDialog }
        val (settings, ng, auto) = dd
        val (nodes, groups) = ng
        val cur = picked ?: settings.s("defaultTarget")
        val lat = { id: String -> app.latency[id] ?: auto?.o("latency")?.o(id)?.let { Latency.of(it) } }
        Text("Через него идёт всё, что по правилам «через прокси», если в правиле или у устройства не выбран другой.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        OutlinedTextField(q, { q = it }, Modifier.fillMaxWidth(), singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) }, placeholder = { Text("Страна, название или протокол") })
        Connected(listOf("groups" to "Группы", "fast" to "Быстрые", "all" to "Все серверы"), tab, { tab = it }, fill = true)
        val qq = q.lowercase()
        val fxIn = MaterialTheme.motionScheme.defaultEffectsSpec<Float>()
        val fxOut = MaterialTheme.motionScheme.fastEffectsSpec<Float>()
        AnimatedContent(tab, transitionSpec = { fadeIn(fxIn) togetherWith fadeOut(fxOut) }) { t ->
            Column {
                if (t == "groups") {
                    val rows = buildList {
                        if (nodes.size > 1 && (qq.isEmpty() || "авто".contains(qq))) add(Triple("", "Авто — все серверы", plural(nodes.size, "сервер", "сервера", "серверов")))
                        groups.filter { qq.isEmpty() || it.name.lowercase().contains(qq) }.forEach { g ->
                            val ag = auto?.a("groups")?.firstOrNull { it.s("groupId") == g.id }
                            val curName = ag?.s("selected")?.let { s -> nodes.firstOrNull { it.id == s }?.name }
                            add(Triple(g.id, g.name, listOfNotNull(plural(groupMembers(g, nodes).size, "сервер", "сервера", "серверов"), g.s("exclude").takeIf { it.isNotEmpty() }?.let { "кроме $it" }, curName?.let { "сейчас $it" }).joinToString(" · ")))
                        }
                    }
                    if (rows.isEmpty()) Empty(Icons.Rounded.Workspaces, "Групп нет", "Создайте группу на странице «Серверы» — например, «Европа» с автовыбором.")
                    SegGroup {
                        rows.forEachIndexed { i, (id, title, sub) ->
                            SegItem(i, rows.size, selected = cur == id, headline = { Text(title) }, supporting = { Text(sub) }, leading = { RadioButton(cur == id, { picked = id }) },
                                trailing = { if (settings.s("defaultTarget") == id) Tag("активный", Tone.Inverse) }, onClick = { picked = id })
                        }
                    }
                } else {
                    var ns = nodes.filter { qq.isEmpty() || it.name.lowercase().contains(qq) || it.s("protocol").contains(qq) }
                    if (t == "fast") ns = ns.filter { (lat(it.id)?.ms ?: 0) > 0 }.sortedBy { lat(it.id)!!.ms }
                    val vs = ns.take(60)
                    if (vs.isEmpty()) Empty(Icons.Rounded.Dns, if (t == "fast") "Нет замеров" else "Ничего не найдено", if (t == "fast") "Проверьте серверы на странице «Серверы»." else "")
                    SegGroup {
                        vs.forEachIndexed { i, n ->
                            SegItem(i, vs.size, selected = cur == n.id, headline = { Text(n.name) }, supporting = { Text(n.s("protocol") + (n.s("security").takeIf { it.isNotEmpty() && it != "none" }?.let { " · $it" } ?: "")) },
                                leading = { RadioButton(cur == n.id, { picked = n.id }) }, trailing = { LatencyView(lat(n.id)) }, onClick = { picked = n.id })
                        }
                    }
                    if (ns.size > 60) Text("Показаны 60 из ${ns.size} — уточните поиск", Modifier.padding(8.dp), style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}
