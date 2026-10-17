package netpilot

// Оболочка: вход, навигация (широкая боковая панель / нижняя панель),
// верхняя панель, переходы между разделами, сохранение, уведомления.

import androidx.compose.animation.*
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Logout
import androidx.compose.material.icons.outlined.*
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

@Composable
fun App() {
    val scope = rememberCoroutineScope()
    val app = remember { AppState(scope) }
    LaunchedEffect(Unit) {
        Api.onAuthLost = { app.auth = Auth.Login }
        Api.onMutation = { app.applier.schedule() }
        onHashChange { app.onHash() }
        onCtrlK { if (app.auth == Auth.In) app.paletteOpen = true }
        guardUnload { app.dirty }
        SpringScroll.install(scope)
        try {
            val st = Api.obj("/auth/state")
            app.auth = when {
                st.b("setup") -> Auth.Setup
                !st.b("authenticated") -> Auth.Login
                else -> Auth.In
            }
        } catch (e: Throwable) {
            println("NetPilot: проверка входа не удалась: $e")
            app.auth = Auth.Login
        }
    }
    LaunchedEffect(app.auth) {
        if (app.auth == Auth.In) startApp(app)
    }
    CompositionLocalProvider(LocalApp provides app) {
        NetPilotTheme(app.design) {
            Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surfaceContainer) {
                val slowFx = MaterialTheme.motionScheme.slowEffectsSpec<Float>()
                val fastFx = MaterialTheme.motionScheme.fastEffectsSpec<Float>()
                AnimatedContent(app.auth, transitionSpec = { fadeIn(slowFx) togetherWith fadeOut(fastFx) }) { a ->
                    when (a) {
                        Auth.Checking -> Splash()
                        Auth.Login, Auth.Setup -> AuthScreen(a == Auth.Setup)
                        Auth.In -> Shell()
                    }
                }
            }
        }
    }
}

private var started = false

private suspend fun startApp(app: AppState) {
    app.refreshActive()
    if (started) { app.refreshPage(); return }
    started = true
    setTitle(app.page.title + " — NetPilot")
    sessionGet("np-restarted")?.let { sessionSet("np-restarted", null); app.toast("Работает версия $it", Kind.Ok, "Перезапуск завершён") }
    app.refreshStatus()
    app.loadUpdate()
    app.update?.let { u ->
        if (u.b("available")) {
            val v = u.o("latest")?.s("version").orEmpty()
            if (storageGet("np-update-seen") != v) {
                app.toast("Доступна версия $v. Установить можно в настройках.", Kind.Info, "Обновление NetPilot")
                storageSet("np-update-seen", v)
            }
        }
    }
    app.refreshCounts()
    app.startPolling()
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun Splash() = Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { ContainedLoadingIndicator(Modifier.size(80.dp)) }

// ---------- разделы ----------

class NavItem(val page: Page?, val title: String, val icon: ImageVector, val iconOn: ImageVector)

val NAV = listOf(
    NavItem(Page.Overview, "Обзор", Icons.Outlined.SpaceDashboard, Icons.Rounded.SpaceDashboard),
    NavItem(Page.Servers, "Серверы", Icons.Outlined.Dns, Icons.Rounded.Dns),
    NavItem(Page.Rules, "Правила", Icons.Outlined.AltRoute, Icons.Rounded.AltRoute),
    NavItem(Page.Devices, "Устройства", Icons.Outlined.Devices, Icons.Rounded.Devices),
    NavItem(Page.Connections, "Соединения", Icons.Outlined.SwapHoriz, Icons.Rounded.SwapHoriz),
    NavItem(Page.Internet, "Интернет", Icons.Outlined.Public, Icons.Rounded.Public),
    NavItem(Page.Settings, "Настройки", Icons.Outlined.Settings, Icons.Rounded.Settings),
)
private val TABS = NAV.take(4) + NavItem(null, "Ещё", Icons.Outlined.MoreHoriz, Icons.Rounded.MoreHoriz)

@Composable
fun Shell() {
    val app = LocalApp.current
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val compact = maxWidth < 860.dp
        CompositionLocalProvider(LocalCompact provides compact) {
            if (compact) MobileShell() else DesktopShell()
            SaveToolbar()
            if (app.leaveTo != null) LeaveDialog()
            if (app.paletteOpen) Palette()
            if (app.pickerOpen) ActivePicker()
            if (app.moreOpen) MoreSheet()
            app.explainHost?.let { ExplainDialog(it) }
            app.updating?.let { UpdateOverlay(it) }
        }
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun DesktopShell() {
    val app = LocalApp.current
    val rail = rememberWideNavigationRailState(
        if (storageGet("np-rail") == "collapsed") WideNavigationRailValue.Collapsed else WideNavigationRailValue.Expanded,
    )
    val scope = rememberCoroutineScope()
    val expanded = rail.targetValue == WideNavigationRailValue.Expanded
    Row(Modifier.fillMaxSize()) {
        WideNavigationRail(
            state = rail,
            colors = WideNavigationRailDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainer),
            header = {
                Column(Modifier.padding(start = 20.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        IconButton(onClick = {
                            scope.launch { rail.toggle(); storageSet("np-rail", if (rail.targetValue == WideNavigationRailValue.Expanded) "expanded" else "collapsed") }
                        }) {
                            Icon(if (expanded) Icons.Rounded.MenuOpen else Icons.Rounded.Menu, if (expanded) "Свернуть меню" else "Развернуть меню")
                        }
                        AnimatedVisibility(expanded, enter = fadeIn() + expandHorizontally(), exit = fadeOut() + shrinkHorizontally()) { Brand() }
                    }
                }
            },
        ) {
            NAV.forEach { item ->
                val sel = app.page == item.page
                WideNavigationRailItem(
                    railExpanded = expanded,
                    selected = sel,
                    onClick = { item.page?.let { app.go(it) } },
                    icon = { NavIcon(item, sel) },
                    label = { Text(item.title) },
                )
            }
            Spacer(Modifier.height(16.dp))
            WideNavigationRailItem(
                railExpanded = expanded, selected = false, onClick = { app.launch { logout(app) } },
                icon = { Icon(Icons.AutoMirrored.Rounded.Logout, null) }, label = { Text("Выйти") },
            )
        }
        Scaffold(
            containerColor = MaterialTheme.colorScheme.surfaceContainer,
            topBar = { DesktopTopBar() },
            snackbarHost = { AppSnackbars(app) },
        ) { pad ->
            Box(Modifier.padding(pad).fillMaxSize()) { PageHost() }
        }
    }
}

@Composable
private fun NavIcon(item: NavItem, sel: Boolean) {
    val app = LocalApp.current
    val n = when (item.page) {
        Page.Settings -> if (app.update?.b("available") == true) "1" else null
        else -> null
    }
    BadgedBox(badge = { if (n != null) Badge { Text(n) } }) { Icon(if (sel) item.iconOn else item.icon, null) }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun Brand() {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        ShapeIcon(Icons.Rounded.Hub, MaterialShapes.Cookie9Sided, size = 36.dp, container = MaterialTheme.colorScheme.primary, content = MaterialTheme.colorScheme.onPrimary)
        Text("NetPilot", style = MaterialTheme.typography.titleLargeEmphasized)
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun DesktopTopBar() {
    val app = LocalApp.current
    Row(
        Modifier.fillMaxWidth().padding(start = 8.dp, end = 24.dp, top = 16.dp, bottom = 12.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Surface(
            onClick = { app.paletteOpen = true }, shape = CircleShape, color = MaterialTheme.colorScheme.surfaceContainerHigh,
            modifier = Modifier.widthIn(max = 520.dp).weight(1f, fill = false).fillMaxWidth().height(56.dp),
        ) {
            Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                Icon(Icons.Rounded.Search, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("Устройство, сервер, правило или сайт", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Surface(shape = RoundedCornerShape(8.dp), color = MaterialTheme.colorScheme.surfaceContainerHighest) {
                    Text("Ctrl K", Modifier.padding(horizontal = 8.dp, vertical = 2.dp), style = MaterialTheme.typography.labelMedium)
                }
            }
        }
        Spacer(Modifier.weight(1f))
        GatewayChip(long = true)
        ActiveSplitButton()
    }
}

@Composable
fun GatewayChip(long: Boolean) {
    val app = LocalApp.current
    val s = app.status
    val x = LocalExtra.current
    val (tone, text) = when {
        s == null -> Tone.Neutral to "…"
        s.b("running") -> Tone.Ok to (if (long) "Шлюз работает · " + uptime(s.l("startedAt")) else "Работает")
        s.s("error").isNotEmpty() -> Tone.Err to "Ошибка ядра"
        else -> Tone.Neutral to (if (long) "Шлюз остановлен" else "Остановлен")
    }
    val (bg, fg) = toneColors(tone)
    Surface(color = bg, contentColor = fg, shape = RoundedCornerShape(12.dp), modifier = Modifier.height(40.dp)) {
        Row(Modifier.padding(horizontal = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Dot(if (s?.b("running") == true) x.ok else if (s?.s("error").orEmpty().isNotEmpty()) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.outline)
            Text(text, style = MaterialTheme.typography.labelLarge, maxLines = 1)
        }
    }
}

/** Разделённая кнопка активного сервера (M3E split button). */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun ActiveSplitButton() {
    val app = LocalApp.current
    val rot by animateFloatAsState(if (app.pickerOpen) 180f else 0f, MaterialTheme.motionScheme.defaultSpatialSpec())
    SplitButtonLayout(
        leadingButton = {
            SplitButtonDefaults.TonalLeadingButton(
                onClick = { app.pickerOpen = true },
                shapes = SplitButtonDefaults.leadingButtonShapesFor(SplitButtonDefaults.MediumContainerHeight),
                modifier = Modifier.heightIn(min = SplitButtonDefaults.MediumContainerHeight),
            ) {
                ShapeIcon(Icons.Rounded.Bolt, MaterialShapes.Cookie9Sided, active = app.pickerOpen, size = 32.dp, container = MaterialTheme.colorScheme.primary, content = MaterialTheme.colorScheme.onPrimary)
                Spacer(Modifier.width(10.dp))
                Column {
                    Text("Активный сервер", style = MaterialTheme.typography.labelSmall)
                    Text(app.activeName, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 200.dp))
                }
            }
        },
        trailingButton = {
            SplitButtonDefaults.TonalTrailingButton(
                checked = app.pickerOpen, onCheckedChange = { app.pickerOpen = it },
                shapes = SplitButtonDefaults.trailingButtonShapesFor(SplitButtonDefaults.MediumContainerHeight),
                modifier = Modifier.heightIn(min = SplitButtonDefaults.MediumContainerHeight),
            ) { Icon(Icons.Rounded.KeyboardArrowDown, "Выбрать активный сервер", Modifier.rotate(rot)) }
        },
    )
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class, ExperimentalMaterial3Api::class)
@Composable
private fun MobileShell() {
    val app = LocalApp.current
    val scroll = TopAppBarDefaults.exitUntilCollapsedScrollBehavior()
    Scaffold(
        modifier = Modifier.nestedScroll(scroll.nestedScrollConnection),
        containerColor = MaterialTheme.colorScheme.surfaceContainer,
        topBar = {
            LargeFlexibleTopAppBar(
                title = { Text(if (app.page == Page.Overview) greeting() else app.page.title) },
                subtitle = if (app.page == Page.Overview) ({ Text("NetPilot") }) else null,
                navigationIcon = { Box(Modifier.padding(start = 12.dp)) { ShapeIcon(Icons.Rounded.Hub, MaterialShapes.Cookie9Sided, size = 32.dp, container = MaterialTheme.colorScheme.primary, content = MaterialTheme.colorScheme.onPrimary) } },
                actions = {
                    IconButton(onClick = { app.paletteOpen = true }) { Icon(Icons.Rounded.Search, "Поиск") }
                    GatewayChip(long = false)
                    Spacer(Modifier.width(8.dp))
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surfaceContainer, scrolledContainerColor = MaterialTheme.colorScheme.surfaceContainer),
                scrollBehavior = scroll,
            )
        },
        bottomBar = {
            ShortNavigationBar {
                TABS.forEach { item ->
                    val more = item.page == null
                    val sel = if (more) app.page in listOf(Page.Connections, Page.Internet, Page.Settings) else app.page == item.page
                    ShortNavigationBarItem(
                        selected = sel,
                        onClick = { if (more) app.moreOpen = true else item.page?.let { app.go(it) } },
                        icon = {
                            BadgedBox(badge = { if (more && app.update?.b("available") == true) Badge() }) { Icon(if (sel) item.iconOn else item.icon, null) }
                        },
                        label = { Text(item.title) },
                    )
                }
            }
        },
        snackbarHost = { AppSnackbars(app) },
    ) { pad ->
        Box(Modifier.padding(pad).fillMaxSize()) { PageHost() }
    }
}

@Composable
private fun AppSnackbars(app: AppState) {
    SnackbarHost(app.snackbar) { data ->
        val t = data.visuals as? Toast
        val icon = when (t?.kind) { Kind.Ok -> Icons.Rounded.CheckCircle; Kind.Err -> Icons.Rounded.Error; Kind.Warn -> Icons.Rounded.Warning; else -> Icons.Rounded.Info }
        Snackbar(
            modifier = Modifier.padding(12.dp).widthIn(max = 480.dp),
            dismissAction = { IconButton(onClick = { data.dismiss() }) { Icon(Icons.Rounded.Close, "Закрыть") } },
            containerColor = if (t?.kind == Kind.Err) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.inverseSurface,
            contentColor = if (t?.kind == Kind.Err) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.inverseOnSurface,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Icon(icon, null, tint = if (t?.kind == Kind.Err) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.inversePrimary)
                Column {
                    if (t?.title != null) Text(t.title, style = MaterialTheme.typography.titleSmall)
                    Text(data.visuals.message)
                }
            }
        }
    }
}

// ---------- страницы и переходы ----------

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun PageHost() {
    val app = LocalApp.current
    val fx = MaterialTheme.motionScheme.defaultEffectsSpec<Float>()
    val fastFx = MaterialTheme.motionScheme.fastEffectsSpec<Float>()
    val spatial = MaterialTheme.motionScheme.defaultSpatialSpec<androidx.compose.ui.unit.IntOffset>()
    AnimatedContent(
        targetState = app.page,
        transitionSpec = {
            (fadeIn(fx) + slideInVertically(spatial) { it / 16 }).togetherWith(fadeOut(fastFx))
        },
        modifier = Modifier.fillMaxSize(),
    ) { p ->
        when (p) {
            Page.Overview -> OverviewPage()
            Page.Servers -> ServersPage()
            Page.Rules -> RulesPage()
            Page.Devices -> DevicesPage()
            Page.Connections -> ConnectionsPage()
            Page.Internet -> InternetPage()
            Page.Settings -> SettingsPage()
        }
    }
}

/** Прокручиваемая страница: содержимое по центру, не шире 1128 dp. */
@Composable
fun PageList(state: LazyListState = rememberLazyListState(), content: LazyListScope.() -> Unit) {
    val compact = LocalCompact.current
    LazyColumn(
        state = state,
        modifier = Modifier.fillMaxSize().springScroll(state),
        contentPadding = PaddingValues(start = if (compact) 16.dp else 8.dp, end = if (compact) 16.dp else 24.dp, top = 4.dp, bottom = 140.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        content = content,
    )
}

/** Элемент страницы шириной не больше колонки содержимого. */
fun LazyListScope.block(key: Any? = null, content: @Composable () -> Unit) =
    item(key) { Box(Modifier.widthIn(max = 1128.dp).fillMaxWidth().animateItem()) { content() } }

// ---------- сохранение ----------

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun SaveToolbar() {
    val app = LocalApp.current
    val compact = LocalCompact.current
    var busy by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxSize().padding(bottom = if (compact) 96.dp else 24.dp), contentAlignment = Alignment.BottomCenter) {
        AnimatedVisibility(
            visible = app.dirty,
            enter = slideInVertically(MaterialTheme.motionScheme.defaultSpatialSpec()) { it * 2 } + fadeIn(MaterialTheme.motionScheme.defaultEffectsSpec()),
            exit = slideOutVertically(MaterialTheme.motionScheme.fastSpatialSpec()) { it * 2 } + fadeOut(MaterialTheme.motionScheme.fastEffectsSpec()),
        ) {
            FloatingBar(vibrant = true, modifier = Modifier.padding(horizontal = 12.dp)) {
                Icon(Icons.Rounded.EditNote, null, Modifier.padding(start = 8.dp))
                Text("Есть несохранённые изменения", Modifier.padding(horizontal = 12.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.titleSmall)
                TextButton(onClick = { app.discardAll() }, enabled = !busy) { Text("Сбросить") }
                Button(
                    onClick = {
                        busy = true
                        app.launch {
                            try {
                                val msg = app.saveAll()
                                app.previewDesign?.let { app.savedDesign = it }
                                app.previewDesign = null
                                app.toast(msg, Kind.Ok)
                                app.refreshActive()
                                app.refreshPage()
                            } finally { busy = false }
                        }
                    },
                    enabled = !busy,
                    shapes = ButtonDefaults.shapes(),
                ) {
                    if (busy) LoadingIndicator(Modifier.size(20.dp), color = LocalContentColor.current) else Icon(Icons.Rounded.Check, null, Modifier.size(18.dp))
                    Spacer(Modifier.width(8.dp))
                    Text("Сохранить")
                }
            }
        }
    }
}

@Composable
private fun LeaveDialog() {
    val app = LocalApp.current
    val proceed = app.leaveTo ?: return
    AppDialog(onDismiss = { app.leaveTo = null }, title = "Несохранённые изменения", width = 480.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Остаться") }
        TextButton(onClick = { d.close { app.pending.clear(); app.form = null; app.previewDesign = null; proceed() } }) { Text("Сбросить") }
        BusyButton("Сохранить") {
            app.toast(app.saveAll(), Kind.Ok)
            app.previewDesign?.let { app.savedDesign = it }
            app.refreshActive()
            d.close { proceed() }
        }
    }) {
        Text("На этой странице есть изменения, которые ещё не сохранены. Сохранить их перед уходом?", style = MaterialTheme.typography.bodyLarge)
    }
}

suspend fun logout(app: AppState) {
    if (app.dirty) { app.leaveTo = { app.launch { logout(app) } }; return }
    try { Api.post("/auth/logout") } catch (_: Throwable) {}
    setHash("")
    reloadPage()
}

// ---------- «Ещё» на телефоне ----------

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun MoreSheet() {
    val app = LocalApp.current
    val s = app.status
    AppDialog(onDismiss = { app.moreOpen = false }, title = "Ещё") { d ->
        val u = app.update
        if (u?.b("available") == true) {
            Surface(onClick = { d.close { app.go(Page.Settings) } }, shape = MaterialTheme.shapes.extraLarge, color = MaterialTheme.colorScheme.tertiaryContainer) {
                Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    ShapeIcon(Icons.Rounded.Upgrade, MaterialShapes.SoftBurst, container = MaterialTheme.colorScheme.tertiary, content = MaterialTheme.colorScheme.onTertiary)
                    Column(Modifier.weight(1f)) {
                        Text("Доступна версия " + u.o("latest")?.s("version"), style = MaterialTheme.typography.titleMedium)
                        Text("Сейчас " + u.s("current"), style = MaterialTheme.typography.bodySmall)
                    }
                    Icon(Icons.Rounded.ChevronRight, null)
                }
            }
        }
        SegGroup {
            listOf(Triple(Page.Connections, Icons.Rounded.SwapHoriz, "что открывают устройства"), Triple(Page.Internet, Icons.Rounded.Public, "как NetPilot стоит в сети"), Triple(Page.Settings, Icons.Rounded.Settings, "оформление, ядро, DNS, обновление"))
                .forEachIndexed { i, (p, ic, sub) ->
                    SegItem(i, 3, headline = { Text(p.title) }, supporting = { Text(sub) }, leading = { Icon(ic, null) }, trailing = { Icon(Icons.Rounded.ChevronRight, null) }, onClick = { d.close { app.go(p) } })
                }
        }
        SegGroup {
            SegItem(0, 2, headline = { Text("Шлюз") }, supporting = { Text(if (s?.b("running") == true) s.s("core") + " · работает " + uptime(s.l("startedAt")) else "остановлен") }, leading = { Icon(Icons.Rounded.Memory, null) }, trailing = { CoreButton(small = true) })
            SegItem(0 + 1, 2, headline = { Text("Выйти", color = MaterialTheme.colorScheme.error) }, leading = { Icon(Icons.AutoMirrored.Rounded.Logout, null, tint = MaterialTheme.colorScheme.error) }, onClick = { d.close { app.launch { logout(app) } } })
        }
    }
}

/** Запуск и остановка шлюза. */
@Composable
fun CoreButton(small: Boolean = false) {
    val app = LocalApp.current
    val running = app.status?.b("running") == true
    BusyButton(if (running) "Остановить" else "Запустить", if (running) Icons.Rounded.Stop else Icons.Rounded.PlayArrow, style = if (running) "outlined" else "filled") {
        Api.post(if (running) "/core/stop" else "/core/start")
        app.toast(if (running) "Шлюз остановлен" else "Шлюз запущен", Kind.Ok)
        app.refreshStatus()
        app.refreshPage()
    }
}
