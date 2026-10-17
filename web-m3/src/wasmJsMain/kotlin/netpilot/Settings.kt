package netpilot

// Настройки: обновление, оформление, режим работы, проверка серверов, ядро,
// DNS, списки, резервная копия, безопасность, диагностика.

import androidx.compose.animation.*
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private class SetData(val settings: J, val cores: List<J>, val update: J?, val lists: List<J>, val net: J?, val tp: J?)

private val CORE_ABOUT = mapOf(
    "sing-box" to "Встроено в NetPilot: все протоколы, списки, распознавание торрентов",
    "xray" to "Отдельная программа: умеет XHTTP; нет Hysteria2 и TUIC, нет списка соединений",
    "mihomo" to "Отдельная программа (Clash.Meta)",
    "hiddify" to "Отдельная программа: sing-box с дополнениями Hiddify, работает так же, как встроенный",
)

@Composable
fun SettingsPage() {
    val app = LocalApp.current
    val data by rememberLoad {
        app.refreshStatus()
        val s = Api.obj("/settings").let { it.with("design" to it.s("design").ifEmpty { "classic" }, "designScheme" to it.s("designScheme").ifEmpty { "indigo" }, "designMode" to it.s("designMode").ifEmpty { "system" }) }
        SetData(s, Api.listOrEmpty("/cores"), Api.objOrNull("/update"), Api.listOrEmpty("/lists"), Api.objOrNull("/network"), Api.objOrNull("/transparent"))
    }
    val d = data ?: run { Loading(); return }
    var s by remember(d) { mutableStateOf(d.settings) }
    var saved by remember(d) { mutableStateOf(d.settings.toString()) }
    var u by remember(d) { mutableStateOf(d.update ?: app.update ?: jobj()) }
    var autoCheck by remember(d) { mutableStateOf(u.b("autoCheck")) }
    DisposableEffect(d) {
        app.form = FormGuard(isDirty = { s.toString() != saved }) {
            Api.put("/settings", s)
            saved = s.toString()
            app.savedDesign = Design.of(s)
            "Настройки сохранены"
        }
        onDispose { app.form = null }
    }
    // Оформление — сразу для предпросмотра; сохраняется кнопкой.
    LaunchedEffect(s.s("design"), s.s("designScheme"), s.s("designMode")) {
        val nd = Design.of(s)
        app.previewDesign = if (nd == app.savedDesign) null else nd
    }
    fun set(vararg p: Pair<String, Any?>) { s = s.with(*p) }
    val running = app.status?.b("running") == true
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    val sections = listOf("upd" to "Обновление", "look" to "Оформление", "mode" to "Режим работы", "ping" to "Проверка серверов", "core" to "Ядро", "dns" to "DNS", "lists" to "Списки блокировок", "backup" to "Резервная копия", "sec" to "Безопасность", "diag" to "Диагностика")
    val compact = LocalCompact.current

    PageList(listState) {
        block("head") { PageHeader("Настройки", "Изменения сохраняются кнопкой внизу — она появляется, когда что-то изменено") }
        block("toc") {
            androidx.compose.foundation.lazy.LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                sections.forEachIndexed { i, (_, title) ->
                    item { AssistChip(onClick = { scope.launch { listState.animateScrollToItem(i + 2) } }, label = { Text(title) }, leadingIcon = if (i == 0 && u.b("available")) ({ Badge() }) else null) }
                }
            }
        }
        // ----- обновление -----
        block("upd") {
            val latest = u.o("latest")
            SectionCard(color = if (u.b("available")) MaterialTheme.colorScheme.tertiaryContainer else MaterialTheme.colorScheme.surfaceContainerLowest) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    ShapeIcon(Icons.Rounded.Upgrade, MaterialShapes.SoftBurst, MaterialShapes.Cookie12Sided, active = u.b("available"), size = 56.dp,
                        container = if (u.b("available")) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.secondaryContainer,
                        content = if (u.b("available")) MaterialTheme.colorScheme.onTertiary else MaterialTheme.colorScheme.onSecondaryContainer)
                    Column(Modifier.weight(1f)) {
                        Text(if (u.b("available")) "${u.s("current")} → ${latest?.s("version")}" else u.s("current").ifEmpty { "—" }, style = MaterialTheme.typography.headlineSmallEmphasized)
                        Text(listOf(u.s("platform"), if (u.l("checkedAt") > 0) "проверено " + ago(u.l("checkedAt")) else "ещё не проверялось", "сам ничего не ставит").filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall)
                    }
                }
                if (u.s("error").isNotEmpty()) Notice(u.s("error"), Tone.Err, Icons.Rounded.Error)
                if (u["signed"] != null && !u.b("signed")) Notice("Сборка без проверки подписи: обновление сверяется только с контрольной суммой из того же релиза. Ставьте его, только если доверяете источнику.", Tone.Warn, Icons.Rounded.Warning)
                if (u.b("available") && latest?.s("notes").orEmpty().isNotEmpty()) Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLowest.copy(alpha = 0.6f)) {
                    Text(latest!!.s("notes"), Modifier.padding(14.dp), style = MaterialTheme.typography.bodySmall)
                }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    BusyButton("Проверить", Icons.Rounded.Refresh, style = "outlined") {
                        try {
                            u = Api.post("/update/check") as J
                            app.toast(if (u.b("available")) "Доступна версия ${u.o("latest")?.s("version")}" else "Установлена последняя версия", Kind.Ok, "Проверка обновлений")
                        } catch (e: ApiError) {
                            app.toast(e.message ?: "", Kind.Err, "Проверка не удалась")
                            u = Api.objOrNull("/update") ?: u
                        }
                        app.update = u
                    }
                    if (u.b("available")) Button(onClick = { runInstall(app, latest?.s("version").orEmpty()) }, enabled = !u.b("installing"), shapes = ButtonDefaults.shapes(),
                        colors = ButtonDefaults.buttonColors(MaterialTheme.colorScheme.tertiary, MaterialTheme.colorScheme.onTertiary)) {
                        Icon(Icons.Rounded.Download, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text("Обновить до ${latest?.s("version")}")
                    }
                }
                SegGroup {
                    val n = if (u.s("previous").isNotEmpty()) 3 else 2
                    SwitchRow(0, n, "Проверять раз в 6 часов", "Только сообщает о новой версии", Icons.Rounded.Schedule, autoCheck) { v ->
                        autoCheck = v
                        if (v == u.b("autoCheck")) app.pending.remove("update-auto") else app.stage("update-auto") { Api.put("/update", jobj("repo" to u.s("repo"), "autoCheck" to v)) }
                    }
                    var editSource by remember { mutableStateOf(false) }
                    SettingRow(1, n, "Источник обновлений", "github.com/${u.s("repo")}" + (if (u.b("hasToken")) " · с токеном" else ""), Icons.Rounded.Link) {
                        OutlinedButton(onClick = { editSource = true }, shapes = ButtonDefaults.shapes()) { Text("Изменить") }
                    }
                    if (editSource) UpdateSourceDialog(u, autoCheck, onDismiss = { editSource = false }) { nu -> u = nu; app.update = nu }
                    if (u.s("previous").isNotEmpty()) {
                        var confirm by remember { mutableStateOf(false) }
                        SettingRow(2, n, "Предыдущая версия ${u.s("previous")}", "Сохранена для отката", Icons.Rounded.History) { OutlinedButton(onClick = { confirm = true }, shapes = ButtonDefaults.shapes()) { Text("Вернуть") } }
                        if (confirm) ConfirmDialog("Вернуть версию ${u.s("previous")}? Программа перезапустится.", confirm = "Вернуть", onDismiss = { confirm = false }) {
                            Api.post("/update/rollback"); waitRestart(app, null)
                        }
                    }
                }
            }
        }
        // ----- оформление -----
        block("look") {
            SectionCard(title = "Оформление") {
                val design = s.s("design")
                val cards: List<@Composable (Modifier) -> Unit> = listOf(
                    { m -> ChoiceCard(design == "m3", "Material 3 Expressive", "Настоящие компоненты Google: пружинная анимация, превращение форм, крупные заголовки, тёмная тема", Icons.Rounded.AutoAwesome, m) { set("design" to "m3") } },
                    { m -> ChoiceCard(design == "classic", "Классическое", "Тёмное меню, светлые карточки, индиго-акцент. Легче для слабых устройств", Icons.Rounded.ViewQuilt, m) { set("design" to "classic") } },
                )
                if (compact) cards.forEach { it(Modifier) } else Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { cards.forEach { it(Modifier.weight(1f)) } }
                AnimatedContent(design == "m3", transitionSpec = { (fadeIn() + expandVertically()) togetherWith (fadeOut() + shrinkVertically()) }) { m3 ->
                    if (m3) Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text("Цвет", style = MaterialTheme.typography.labelLarge)
                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            SCHEMES.forEach { (v, label) ->
                                val on = s.s("designScheme") == v
                                val seed = SCHEME_SEED[v] ?: Color.Gray
                                Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                    Surface(onClick = { set("designScheme" to v) }, shape = CircleShape, color = Color.Transparent) {
                                        ShapeIcon(if (on) Icons.Rounded.Check else null, MaterialShapes.Cookie9Sided, MaterialShapes.Cookie12Sided, active = on, size = 56.dp, container = seed, content = Color.White)
                                    }
                                    Text(label, style = MaterialTheme.typography.labelMedium)
                                }
                            }
                        }
                        Text("Тема", style = MaterialTheme.typography.labelLarge)
                        Connected(listOf("system" to "Как в системе", "light" to "Светлая", "dark" to "Тёмная"), s.s("designMode"), { set("designMode" to it) }, fill = true)
                    } else Notice("Классическое оформление откроется после сохранения. Вернуться можно в его настройках: «Оформление → Material 3 Expressive».", Tone.Secondary, Icons.Rounded.Info)
                }
                Text("Оформление общее для всех устройств, с которых открывают панель. «Как в системе» — светлая или тёмная тема по настройке устройства.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        // ----- режим работы -----
        block("mode") {
            SectionCard(title = "Режим работы") {
                val cards: List<@Composable (Modifier) -> Unit> = listOf(
                    { m -> ChoiceCard(s.s("mode") == "transparent", "Прозрачный прокси", "Устройства ничего не настраивают: достаточно указать NetPilot шлюзом (Linux)", Icons.Rounded.Lan, m) { set("mode" to "transparent") } },
                    { m -> ChoiceCard(s.s("mode") != "transparent", "Прокси-сервер", "Адрес прокси вписывается на каждом устройстве вручную", Icons.Rounded.SettingsEthernet, m) { set("mode" to "proxy") } },
                )
                if (compact) cards.forEach { it(Modifier) } else Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { cards.forEach { it(Modifier.weight(1f)) } }
                if (s.s("mode") == "transparent") {
                    Select("Кого перехватывать", listOf("all" to "Все устройства сети", "selected" to "Только с маршрутом «через прокси»"), s.s("transparentScope").ifEmpty { "all" }, { set("transparentScope" to it) })
                    SegGroup { SwitchRow(0, 1, "Перехват DNS", "Провайдер не видит, какие сайты открываются", Icons.Rounded.VisibilityOff, s.b("hijackDns")) { set("hijackDns" to it) } }
                    Text("IPv6", style = MaterialTheme.typography.labelLarge)
                    Connected(listOf("block" to "Блокировать", "intercept" to "Перехватывать", "off" to "Не трогать"), s.s("ipv6").ifEmpty { "block" }, { set("ipv6" to it) }, fill = true)
                    Text("Блокировка — устройства сразу переходят на IPv4 и не идут мимо прокси", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Field("Порт перехвата (TPROXY)", s.i("tproxyPort").toString(), { set("tproxyPort" to (it.toIntOrNull() ?: 0)) }, number = true, hint = "Внутренний порт для перехваченного трафика")
                    val tp = d.tp
                    if (tp != null) Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("Состояние перехвата:", style = MaterialTheme.typography.bodyMedium)
                        if (tp.b("active")) Tag("включён", Tone.Ok) else if (tp.b("supported")) Tag("включится при запуске шлюза") else Tag("недоступен на этой системе", Tone.Err)
                    }
                    (listOfNotNull(tp?.s("error")?.ifEmpty { null }) + tp?.sl("problems").orEmpty()).forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = LocalExtra.current.warn) }
                } else {
                    Select("Доступ к прокси", listOf("127.0.0.1" to "Только этот компьютер", "0.0.0.0" to "Вся локальная сеть") + (if (s.s("listenAddr") !in setOf("127.0.0.1", "0.0.0.0", "")) listOf(s.s("listenAddr") to s.s("listenAddr")) else emptyList()), s.s("listenAddr").ifEmpty { "127.0.0.1" }, { set("listenAddr" to it) }, hint = "Для других устройств выберите «Вся локальная сеть»")
                    Field("Порт", s.i("mixedPort").toString(), { set("mixedPort" to (it.toIntOrNull() ?: 0)) }, number = true, hint = "HTTP и SOCKS5 на одном порту")
                    val os = app.status?.s("os")
                    if (os == "windows" || os == "darwin") SegGroup { SwitchRow(0, 1, "Системный прокси", "Направить через NetPilot программы этого компьютера", Icons.Rounded.Computer, s.b("systemProxy")) { set("systemProxy" to it) } }
                }
            }
        }
        // ----- проверка серверов -----
        block("ping") {
            SectionCard(title = "Проверка серверов") {
                Text("Как измерять задержку кнопкой «Проверить». Автоматические группы всегда проверяют настоящим запросом через сервер — только так видно, что сервер пропускает трафик.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                PING_METHODS.chunked(if (compact) 1 else 2).forEach { row ->
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        row.forEach { (v, title, sub) -> ChoiceCard(s.s("pingMethod").ifEmpty { "auto" } == v, title, sub, null, Modifier.weight(1f)) { set("pingMethod" to v) } }
                    }
                }
                Field("Адрес для проверки через сервер", s.s("testUrl"), { set("testUrl" to it) }, mono = true, hint = "Отвечает быстро и без содержимого")
            }
        }
        // ----- ядро -----
        block("core") {
            SectionCard(title = "Ядро") {
                SegGroup {
                    d.cores.forEachIndexed { i, c -> CoreRow(i, d.cores.size, c, running && app.status?.s("core") == c.name) }
                }
                Select("Какое ядро использовать", listOf("auto" to "Выбирать автоматически") + d.cores.map { it.name to it.name + if (it.b("available")) "" else " (не установлено)" }, s.s("core").ifEmpty { "auto" }, { set("core" to it) }, hint = "Автоматически — то, что поддерживает все серверы и расходует меньше ресурсов")
                SegGroup { SwitchRow(0, 1, "Запускать вместе с программой", "Для шлюза обязательно", Icons.Rounded.PowerSettingsNew, s.b("autoStart")) { set("autoStart" to it) } }
            }
        }
        // ----- DNS -----
        block("dns") {
            SectionCard(title = "DNS") {
                Field("Для трафика через прокси", s.s("dnsRemote"), { set("dnsRemote" to it) }, mono = true, hint = "Запросы идут через сервер. https://, tls://, quic:// или IP")
                Field("Для прямого трафика", s.s("dnsDirect"), { set("dnsDirect" to it) }, mono = true, hint = "И для ответов устройствам. local — системный")
                SegGroup { SwitchRow(0, 1, "Отвечать устройствам через прокси", "Выключено (рекомендуется): DNS не зависит от серверов. Включено: если сервер упадёт, DNS пропадёт у всей сети", Icons.Rounded.Dns, s.b("dnsViaProxy")) { set("dnsViaProxy" to it) } }
            }
        }
        // ----- списки -----
        block("lists") {
            SectionCard(title = "Списки блокировок", action = { BusyButton("Обновить", Icons.Rounded.Refresh, style = "tonal") { try { Api.post("/lists/update"); app.toast("Списки обновлены", Kind.Ok) } catch (e: ApiError) { app.toast(e.message ?: "", Kind.Err) }; app.refreshPage() } }) {
                SegGroup {
                    d.lists.forEachIndexed { i, l ->
                        SegItem(i, d.lists.size, headline = { Text(l.name) }, supporting = { Text(l.s("description")) },
                            trailing = { if (l.b("ready")) Tag((if (l.i("entries") > 0) formatThousands(l.i("entries")) + " · " else "") + ago(l.l("updated")), Tone.Ok) else Tag(if (l.s("error").isNotEmpty()) "ошибка" else "не скачан", Tone.Warn) })
                    }
                }
            }
        }
        // ----- резервная копия -----
        block("backup") {
            SectionCard(title = "Резервная копия") {
                Text("Устройства, серверы, группы, правила, подписки и настройки. Без пароля администратора. В файле есть ключи серверов — храните его надёжно.", style = MaterialTheme.typography.bodyMedium)
                var restore by remember { mutableStateOf<Pair<String, String>?>(null) }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilledTonalButton(onClick = { openUrl("/api/backup") }, shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.Download, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Скачать") }
                    OutlinedButton(onClick = { app.launch { restore = pickTextFile(".json,application/json") } }, shapes = ButtonDefaults.shapes()) { Icon(Icons.Rounded.Upload, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Восстановить") }
                }
                restore?.let { (name, text) ->
                    ConfirmDialog("Заменить текущие настройки копией «$name»?", confirm = "Восстановить", onDismiss = { restore = null }) {
                        Api.call("POST", "/restore", rawBody = text)
                        app.toast("Настройки восстановлены из копии", Kind.Ok)
                        app.applier.schedule()
                        app.form = null
                        app.refreshActive()
                        app.refreshPage()
                    }
                }
            }
        }
        // ----- безопасность -----
        block("sec") {
            SectionCard(title = "Безопасность") {
                var cur by remember { mutableStateOf("") }
                var next by remember { mutableStateOf("") }
                val fields: List<@Composable (Modifier) -> Unit> = listOf(
                    { m -> Field("Текущий пароль", cur, { cur = it }, m, password = true) },
                    { m -> Field("Новый пароль", next, { next = it }, m, password = true, hint = "от 8 символов") },
                )
                if (compact) fields.forEach { it(Modifier) } else Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { fields.forEach { it(Modifier.weight(1f)) } }
                BusyButton("Сменить пароль", Icons.Rounded.Password, style = "tonal", enabled = cur.isNotEmpty() && next.isNotEmpty()) {
                    Api.post("/auth/password", jobj("current" to cur, "next" to next))
                    app.toast("Пароль изменён, остальные сеансы завершены", Kind.Ok)
                    cur = ""; next = ""
                }
            }
        }
        // ----- диагностика -----
        block("diag") {
            SectionCard(title = "Диагностика") {
                var out by remember { mutableStateOf<String?>(null) }
                Select("Уровень журнала", listOf("error", "warn", "info", "debug").map { it to it }, s.s("logLevel").ifEmpty { "info" }, { set("logLevel" to it) })
                d.net?.let { n ->
                    SegGroup {
                        SegItem(0, 2, headline = { Text("Платформа и DHCP") }, supporting = { Text(listOf(n.s("environment"), n.s("dhcp"), if (n.b("dhcpManaged")) "закреплённые IP записываются в DHCP сами" else "закрепление IP — только при установке на OpenWrt или в режиме роутера").filter { it.isNotEmpty() }.joinToString(" · ")) })
                        SegItem(1, 2, headline = { Text(n.sl("networks").joinToString(", ").ifEmpty { "—" }, fontFamily = LocalMono.current) }, supporting = { Text("Сканируемые сети") })
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    BusyButton("Журнал ядра", style = "tonal") { out = Api.obj("/core/log").s("log").ifEmpty { "Журнал пуст" } }
                    BusyButton("Итоговая конфигурация", style = "tonal") {
                        out = try {
                            val r = Api.obj("/core/preview")
                            "// ядро: ${r.s("core")} (${r.s("reason")})\n" + JsonPretty.encodeToString(kotlinx.serialization.json.JsonElement.serializer(), r["config"] ?: kotlinx.serialization.json.JsonNull)
                        } catch (e: ApiError) { "Ошибка: " + e.message }
                    }
                }
                AnimatedVisibility(out != null, enter = expandVertically(MaterialTheme.motionScheme.defaultSpatialSpec()) + fadeIn(), exit = shrinkVertically() + fadeOut()) {
                    Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLow) {
                        androidx.compose.foundation.text.selection.SelectionContainer {
                            Text(out ?: "", Modifier.padding(16.dp).heightIn(max = 420.dp).verticalScrollable(), fontFamily = LocalMono.current, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun CoreRow(i: Int, n: Int, c: J, running: Boolean) {
    val app = LocalApp.current
    var confirm by remember { mutableStateOf(false) }
    val outdated = c.b("available") && c.s("latest").isNotEmpty() && c.s("version") != "?" && newer(c.s("latest"), c.s("version"))
    val issues = c.a("issues").size
    SegItem(
        i, n,
        headline = {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(c.name + if (c.b("available") && c.s("version").isNotEmpty()) " " + c.s("version") else "")
                if (running) Tag("работает", Tone.Ok)
                when {
                    !c.b("available") -> Tag("не установлено")
                    c.s("latest").isEmpty() -> {}
                    outdated -> Tag("есть ${c.s("latest")}", Tone.Warn)
                    else -> Tag("последняя версия", Tone.Ok)
                }
            }
        },
        supporting = {
            Text(listOfNotNull(CORE_ABOUT[c.name] ?: if (c.b("installable")) "ставится отдельно" else "встроено",
                if (issues > 0) "не поддерживает серверов: $issues" else "поддерживает все ваши серверы",
                if (!c.b("installable") && outdated) "новая версия придёт с обновлением NetPilot" else null).joinToString(" · "))
        },
        leading = { ShapeIcon(Icons.Rounded.Memory, MaterialShapes.Cookie9Sided, active = running, container = if (running) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.secondaryContainer, content = if (running) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSecondaryContainer) },
        trailing = if (!c.b("installable")) null else ({
            Row(verticalAlignment = Alignment.CenterVertically) {
                BusyButton(if (!c.b("available")) "Установить" + (c.s("latest").takeIf { it.isNotEmpty() }?.let { " $it" } ?: "") else if (outdated) "Обновить до ${c.s("latest")}" else "Переустановить",
                    if (c.b("available")) Icons.Rounded.Refresh else Icons.Rounded.Download, style = if (!c.b("available") || outdated) "filled" else "outlined", enabled = !running) {
                    val r = Api.post("/cores/${c.name}/install") as J
                    app.toast("${c.name} ${r.s("version")} установлено", Kind.Ok)
                    app.refreshPage()
                }
                if (c.b("available")) IconButton(onClick = { confirm = true }, enabled = !running) { Icon(Icons.Rounded.Delete, "Удалить ${c.name}") }
            }
        }),
    )
    if (confirm) ConfirmDialog("Удалить ядро ${c.name}?", onDismiss = { confirm = false }) { Api.delete("/cores/" + c.name); app.refreshPage() }
}

// ---------- обновление программы ----------

/**
 * Смена репозитория или токена. Источник решает, какая программа будет
 * установлена, поэтому сервер требует текущий пароль при его смене.
 */
@Composable
private fun UpdateSourceDialog(u: J, autoCheck: Boolean, onDismiss: () -> Unit, onSaved: (J) -> Unit) {
    val app = LocalApp.current
    fun norm(v: String) = v.trim().removePrefix("https://").removePrefix("github.com/").trim('/')
    var repo by remember { mutableStateOf(u.s("repo")) }
    var token by remember { mutableStateOf("") }
    var dropToken by remember { mutableStateOf(false) }
    var password by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    val changed = norm(repo) != u.s("repo") || token.isNotBlank() || dropToken
    AppDialog(onDismiss, "Источник обновлений", width = 520.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        BusyButton("Сохранить", enabled = !changed || password.isNotEmpty()) {
            if (!changed) { d.close {}; return@BusyButton }
            val body = buildList<Pair<String, Any?>> {
                add("repo" to norm(repo)); add("autoCheck" to autoCheck); add("password" to password)
                if (dropToken) add("token" to "") else if (token.isNotBlank()) add("token" to token.trim())
            }
            try {
                val nu = Api.put("/update", jobj(*body.toTypedArray())) as J
                onSaved(nu)
                app.toast("Источник обновлений изменён", Kind.Ok)
                d.close {}
            } catch (e: ApiError) {
                error = e.message ?: ""
            }
        }
    }) {
        Field("Репозиторий GitHub", repo, { repo = it }, placeholder = "владелец/имя", hint = "Откуда берутся новые версии", mono = true)
        Field("Токен GitHub", if (dropToken) "" else token, { token = it }, password = true,
            placeholder = if (u.b("hasToken")) "сохранён" else "не задан",
            hint = if (u.b("hasToken")) "Пусто — оставить сохранённый" else "Нужен только для закрытого репозитория")
        if (u.b("hasToken")) Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(dropToken, { dropToken = it; if (it) token = "" })
            Text("Удалить сохранённый токен", style = MaterialTheme.typography.bodyMedium)
        }
        if (error.isNotEmpty()) Notice(error, Tone.Err, Icons.Rounded.Error)
        AnimatedVisibility(changed) {
            Field("Текущий пароль", password, { password = it; error = "" }, password = true, hint = "Подтверждает смену источника обновлений")
        }
    }
}

val UPDATE_STAGES = listOf("prepare" to "Подготовка", "download" to "Скачивание с GitHub", "verify" to "Проверка контрольной суммы", "probe" to "Пробный запуск новой версии", "install" to "Установка", "restart" to "Перезапуск — сеть пропадёт на 5–10 секунд")

fun runInstall(app: AppState, target: String) {
    app.updating = UpdateProgress(target)
    var finished = false
    app.scope.launch {
        while (!finished) {
            try {
                val u = Api.obj("/update")
                u.o("progress")?.let { p -> if (!finished) app.updating = app.updating?.copy(stage = p.s("stage"), done = p.l("done"), total = p.l("total")) }
            } catch (_: Throwable) {}
            delay(400)
        }
    }
    app.scope.launch {
        try {
            Api.post("/update/install")
            finished = true
            waitRestart(app, target)
        } catch (e: ApiError) {
            finished = true
            app.updating = null
            app.toast(e.message ?: "", Kind.Err, "Обновление не установлено")
        }
    }
}

fun waitRestart(app: AppState, target: String?) {
    app.updating = (app.updating ?: UpdateProgress(target)).copy(stage = "restart", restarting = true)
    val from = app.status?.s("version")
    val started = nowMs()
    app.scope.launch {
        var wentDown = false
        delay(1500)
        while (true) {
            try {
                val (code, text) = http("GET", "/api/status", null)
                if (code in 200..299) {
                    val st = JsonFmt.parseToJsonElement(text) as J
                    if (st.s("version") != from || (wentDown && nowMs() - started > 4000)) {
                        sessionSet("np-restarted", st.s("version"))
                        reloadPage()
                        return@launch
                    }
                } else wentDown = true
            } catch (_: Throwable) { wentDown = true }
            if (nowMs() - started > 120000) {
                app.updating = app.updating?.copy(note = "Программа долго не отвечает. Проверьте службу на устройстве или обновите страницу позже.")
                return@launch
            }
            delay(1500)
        }
    }
}

@Composable
fun UpdateOverlay(u: UpdateProgress) {
    val idx = UPDATE_STAGES.indexOfFirst { it.first == u.stage }.coerceAtLeast(0)
    val pct = if (u.stage == "download" && u.total > 0) (u.done.toFloat() / u.total).coerceIn(0f, 1f) else null
    val overall = pct?.let { 0.05f + it * 0.8f } ?: mapOf("prepare" to 0.03f, "verify" to 0.87f, "probe" to 0.91f, "install" to 0.95f, "restart" to 1f)[u.stage] ?: 0f
    val p by androidx.compose.animation.core.animateFloatAsState(overall, MaterialTheme.motionScheme.slowSpatialSpec())
    androidx.compose.ui.window.Dialog(onDismissRequest = {}, properties = androidx.compose.ui.window.DialogProperties(dismissOnBackPress = false, dismissOnClickOutside = false, usePlatformDefaultWidth = false)) {
        Surface(shape = MaterialTheme.shapes.extraLarge, color = MaterialTheme.colorScheme.surfaceContainerHigh, modifier = Modifier.padding(24.dp).widthIn(max = 520.dp).fillMaxWidth()) {
            Column(Modifier.padding(28.dp), verticalArrangement = Arrangement.spacedBy(18.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                ContainedLoadingIndicator(Modifier.size(88.dp))
                Text(if (u.target != null) "Обновление до ${u.target}" else "Перезапуск", style = MaterialTheme.typography.headlineMediumEmphasized)
                Text("Не закрывайте страницу — после перезапуска она обновится сама", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row {
                        Text(if (u.stage == "download" && u.total > 0) "Скачивание: ${bytes(u.done)} из ${bytes(u.total)}" else UPDATE_STAGES[idx].second, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
                        Text("${(p * 100).toInt()}%", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                    }
                    if (u.restarting) LinearWavyProgressIndicator(Modifier.fillMaxWidth()) else LinearWavyProgressIndicator(progress = { p }, modifier = Modifier.fillMaxWidth())
                }
                SegGroup {
                    UPDATE_STAGES.forEachIndexed { i, (_, label) ->
                        SegItem(i, UPDATE_STAGES.size, selected = i == idx, headline = { Text(label, style = MaterialTheme.typography.bodyMedium) },
                            leading = {
                                when {
                                    i < idx -> Icon(Icons.Rounded.CheckCircle, null, tint = LocalExtra.current.ok)
                                    i == idx -> LoadingIndicator(Modifier.size(24.dp))
                                    else -> Icon(Icons.Rounded.RadioButtonUnchecked, null, tint = MaterialTheme.colorScheme.outline)
                                }
                            })
                    }
                }
                Notice(u.note ?: "Текущая версия сохранится. Если новая не запустится, NetPilot вернёт её сам.", if (u.note != null) Tone.Warn else Tone.Secondary, Icons.Rounded.Shield)
            }
        }
    }
}
