package netpilot

// Вход и первая настройка пароля, поиск по панели (Ctrl K).

import androidx.compose.animation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.*
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import kotlinx.coroutines.launch

@Composable
fun AuthScreen(setup: Boolean) {
    val app = LocalApp.current
    var pw by remember { mutableStateOf("") }
    var pw2 by remember { mutableStateOf("") }
    var err by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    val cs = MaterialTheme.colorScheme
    val submit: () -> Unit = {
        if (setup && pw != pw2) err = "Пароли не совпадают"
        else if (!busy) {
            busy = true; err = ""
            app.scope.launch {
                try {
                    Api.post(if (setup) "/auth/setup" else "/auth/login", jobj("password" to pw))
                    app.auth = Auth.In
                } catch (e: ApiError) { err = e.message ?: "Ошибка" } finally { busy = false }
            }
        }
    }
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val compact = maxWidth < 860.dp
        val promo: @Composable (Modifier) -> Unit = { m ->
            Surface(m, shape = RoundedCornerShape(if (compact) 40.dp else 48.dp), color = cs.primaryContainer, contentColor = cs.onPrimaryContainer) {
                Box {
                    LivingShape(MaterialShapes.Cookie12Sided, MaterialShapes.Sunny, cs.primary, Modifier.align(Alignment.TopEnd).offset(if (compact) 70.dp else 110.dp, if (compact) (-50).dp else (-90).dp).size(if (compact) 260.dp else 460.dp), 7000)
                    LivingShape(MaterialShapes.Clover4Leaf, MaterialShapes.Flower, cs.tertiaryContainer, Modifier.align(Alignment.BottomEnd).offset((-180).dp, 60.dp).size(if (compact) 150.dp else 300.dp), 9000)
                    LivingShape(MaterialShapes.SoftBurst, MaterialShapes.Cookie9Sided, cs.secondaryContainer, Modifier.align(Alignment.BottomEnd).offset((-30).dp, (-120).dp).size(if (compact) 80.dp else 170.dp), 5000)
                    Column(Modifier.fillMaxSize().padding(if (compact) 28.dp else 56.dp), verticalArrangement = Arrangement.SpaceBetween) {
                        Brand()
                        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            Text("Вся сеть — по вашим правилам", style = if (compact) MaterialTheme.typography.headlineLargeEmphasized else MaterialTheme.typography.displayLargeEmphasized, modifier = Modifier.widthIn(max = 600.dp))
                            Text("Шлюз, прокси и маршрутизация для дома и офиса", style = MaterialTheme.typography.bodyLarge)
                        }
                        if (!compact) Spacer(Modifier.height(1.dp))
                    }
                }
            }
        }
        val form: @Composable (Modifier) -> Unit = { m ->
            Box(m, contentAlignment = Alignment.Center) {
                Surface(shape = MaterialTheme.shapes.extraLarge, color = cs.surfaceContainerLowest, modifier = Modifier.widthIn(max = 420.dp).fillMaxWidth().padding(16.dp)) {
                    Column(Modifier.padding(28.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                        Text(if (setup) "Задайте пароль" else "Вход", style = MaterialTheme.typography.headlineMediumEmphasized)
                        Text(if (setup) "Первый запуск: придумайте пароль администратора, не короче 8 символов." else "Пароль администратора NetPilot", style = MaterialTheme.typography.bodyMedium, color = cs.onSurfaceVariant)
                        var show by remember { mutableStateOf(false) }
                        OutlinedTextField(
                            pw, { pw = it }, Modifier.fillMaxWidth(), label = { Text("Пароль") }, singleLine = true,
                            visualTransformation = if (show) androidx.compose.ui.text.input.VisualTransformation.None else androidx.compose.ui.text.input.PasswordVisualTransformation(),
                            trailingIcon = { IconButton(onClick = { show = !show }) { Icon(if (show) Icons.Rounded.VisibilityOff else Icons.Rounded.Visibility, "Показать пароль") } },
                            keyboardOptions = KeyboardOptions(imeAction = if (setup) ImeAction.Next else ImeAction.Done),
                            keyboardActions = KeyboardActions(onDone = { submit() }),
                            isError = err.isNotEmpty(),
                        )
                        if (setup) OutlinedTextField(pw2, { pw2 = it }, Modifier.fillMaxWidth(), label = { Text("Ещё раз") }, singleLine = true,
                            visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done), keyboardActions = KeyboardActions(onDone = { submit() }))
                        AnimatedVisibility(err.isNotEmpty()) { Text(err, color = cs.error, style = MaterialTheme.typography.bodySmall) }
                        Button(
                            onClick = submit, enabled = !busy && pw.isNotEmpty(), modifier = Modifier.fillMaxWidth().heightIn(min = ButtonDefaults.MediumContainerHeight),
                            shapes = ButtonDefaults.shapesFor(ButtonDefaults.MediumContainerHeight), contentPadding = ButtonDefaults.contentPaddingFor(ButtonDefaults.MediumContainerHeight),
                        ) {
                            if (busy) LoadingIndicator(Modifier.size(24.dp), color = LocalContentColor.current)
                            else Text(if (setup) "Сохранить и войти" else "Войти", style = ButtonDefaults.textStyleFor(ButtonDefaults.MediumContainerHeight))
                        }
                        if (!setup) Text("Забыли пароль — задайте новый на устройстве командой netpilot -password", style = MaterialTheme.typography.bodySmall, color = cs.onSurfaceVariant)
                    }
                }
            }
        }
        if (compact) Column(Modifier.fillMaxSize().padding(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) { promo(Modifier.fillMaxWidth().height(320.dp)); form(Modifier.fillMaxWidth()) }
        else Row(Modifier.fillMaxSize().padding(16.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) { promo(Modifier.weight(1f).fillMaxHeight()); form(Modifier.width(520.dp).fillMaxHeight()) }
    }
}


// ---------- поиск по панели ----------

private class PaletteItem(val icon: ImageVector, val title: String, val sub: String, val go: () -> Unit)

@Composable
fun Palette() {
    val app = LocalApp.current
    var q by remember { mutableStateOf("") }
    var cur by remember { mutableIntStateOf(0) }
    val data by rememberLoad { listOf(Api.objOrNull("/network")?.a("devices").orEmpty(), Api.listOrEmpty("/nodes"), Api.listOrEmpty("/rules"), Api.listOrEmpty("/groups")) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    val groups = remember(q, data) {
        val out = mutableListOf<Pair<String, List<PaletteItem>>>()
        val d = data
        val qq = q.trim().lowercase()
        fun m(s: String) = s.lowercase().contains(qq)
        if (qq.isNotEmpty() && d != null) {
            val host = qq.removePrefix("https://").removePrefix("http://").substringBefore('/')
            if (Regex("^[a-z0-9.-]+\\.[a-z]{2,}$").matches(host) || Regex("^\\d+\\.\\d+\\.\\d+\\.\\d+$").matches(host))
                out += "Сайт" to listOf(PaletteItem(Icons.Rounded.Public, "Куда пойдёт $host", "проверить по правилам") { app.explainHost = host })
            d[0].filter { m(it.name) || m(it.s("ip")) || m(it.s("mac")) }.take(6).map { PaletteItem(deviceIcon(it.s("type")).first, it.name, it.s("ip")) { app.go(Page.Devices) } }.let { if (it.isNotEmpty()) out += "Устройства" to it }
            (d[3].filter { m(it.name) }.map { PaletteItem(Icons.Rounded.Workspaces, it.name, "группа") { app.go(Page.Servers) } } +
                d[1].filter { m(it.name) || m(it.s("server")) }.map { PaletteItem(Icons.Rounded.Dns, it.name, it.s("protocol")) { app.go(Page.Servers) } }).take(6).let { if (it.isNotEmpty()) out += "Серверы и группы" to it }
            d[2].filter { m(it.name) }.take(6).map { PaletteItem(Icons.Rounded.AltRoute, it.name, if (it.b("enabled")) "включено" else "выключено") { app.go(Page.Rules) } }.let { if (it.isNotEmpty()) out += "Правила" to it }
        }
        Page.entries.filter { qq.isEmpty() || it.title.lowercase().contains(qq) }.map { p -> PaletteItem(NAV.first { it.page == p }.icon, p.title, "") { app.go(p) } }.let { if (it.isNotEmpty()) out += "Разделы" to it }
        out
    }
    val flat = groups.flatMap { it.second }
    AppDialog(onDismiss = { app.paletteOpen = false }, title = null, width = 640.dp) { d ->
        OutlinedTextField(
            q, { q = it; cur = 0 }, Modifier.fillMaxWidth().focusRequester(focus).onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionDown -> { cur = minOf(flat.lastIndex, cur + 1); true }
                    Key.DirectionUp -> { cur = maxOf(0, cur - 1); true }
                    Key.Enter, Key.NumPadEnter -> { flat.getOrNull(cur)?.let { d.close { it.go() } }; true }
                    else -> false
                }
            },
            singleLine = true, shape = CircleShape, leadingIcon = { Icon(Icons.Rounded.Search, null) },
            placeholder = { Text("Устройство, сервер, правило или адрес сайта") },
        )
        if (flat.isEmpty()) Text("Ничего не найдено", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        var i = 0
        groups.forEach { (title, items) ->
            SectionTitle(title)
            SegGroup {
                items.forEachIndexed { j, it ->
                    val idx = i++
                    SegItem(j, items.size, selected = idx == cur, headline = { Text(it.title) }, supporting = if (it.sub.isNotEmpty()) ({ Text(it.sub) }) else null,
                        leading = { Icon(it.icon, null) }, onClick = { d.close { it.go() } })
                }
            }
        }
    }
}
