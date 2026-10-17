package netpilot

// Общие элементы интерфейса на компонентах Material 3 Expressive.

import androidx.compose.animation.*
import androidx.compose.animation.core.*
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.hoverable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsHoveredAsState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.*
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.graphics.shapes.Morph
import androidx.graphics.shapes.RoundedPolygon
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** Узкий экран (телефон): нижняя навигация, листы снизу. */
val LocalCompact = compositionLocalOf { false }

// ---------- формы M3 Expressive и их превращение ----------

/** Форма из пары многоугольников M3 с долей превращения 0..1. */
class MorphShape(private val morph: Morph, private val progress: Float, private val rotation: Float = 0f) : Shape {
    override fun createOutline(size: Size, layoutDirection: LayoutDirection, density: Density): Outline {
        val path = morph.toPath(progress)
        val m = Matrix()
        m.translate(size.width / 2, size.height / 2)
        if (rotation != 0f) m.rotateZ(rotation)
        m.scale(size.width, size.height)
        m.translate(-0.5f, -0.5f)
        path.transform(m)
        return Outline.Generic(path)
    }
}

/** Значок в фигуре M3: при active фигура пружинисто превращается в другую. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun ShapeIcon(
    icon: ImageVector?,
    from: RoundedPolygon,
    to: RoundedPolygon = MaterialShapes.Circle,
    active: Boolean = false,
    size: Dp = 40.dp,
    container: Color = MaterialTheme.colorScheme.primaryContainer,
    content: Color = MaterialTheme.colorScheme.onPrimaryContainer,
    online: Boolean = false,
    text: String? = null,
) {
    val progress by animateFloatAsState(if (active) 1f else 0f, MaterialTheme.motionScheme.defaultSpatialSpec())
    val morph = remember(from, to) { Morph(from, to) }
    Box(Modifier.size(size), contentAlignment = Alignment.Center) {
        Box(Modifier.matchParentSize().background(container, MorphShape(morph, progress, rotation = progress * 90f)))
        if (icon != null) Icon(icon, null, tint = content, modifier = Modifier.size(size * 0.5f))
        if (text != null) Text(text, color = content, style = MaterialTheme.typography.labelLarge)
        if (online) Box(
            Modifier.align(Alignment.BottomEnd).size(12.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surface).padding(2.dp)
                .clip(CircleShape).background(LocalExtra.current.ok),
        )
    }
}

/** Фигура, которая медленно вращается и «дышит» — для крупных акцентов. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun LivingShape(a: RoundedPolygon, b: RoundedPolygon, color: Color, modifier: Modifier = Modifier, periodMs: Int = 6000) {
    val t = rememberInfiniteTransition()
    val p by t.animateFloat(0f, 1f, infiniteRepeatable(tween(periodMs, easing = FastOutSlowInEasing), RepeatMode.Reverse))
    val r by t.animateFloat(0f, 360f, infiniteRepeatable(tween(periodMs * 4, easing = LinearEasing)))
    val morph = remember(a, b) { Morph(a, b) }
    Box(modifier.background(color, MorphShape(morph, p, r)))
}

// ---------- значки устройств ----------

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
fun deviceIcon(type: String): Pair<ImageVector, RoundedPolygon> = when (type) {
    "phone" -> Icons.Rounded.PhoneIphone to MaterialShapes.Cookie9Sided
    "tablet" -> Icons.Rounded.TabletAndroid to MaterialShapes.Cookie6Sided
    "laptop" -> Icons.Rounded.Laptop to MaterialShapes.Cookie4Sided
    "computer" -> Icons.Rounded.DesktopWindows to MaterialShapes.Arch
    "tv" -> Icons.Rounded.Tv to MaterialShapes.Clover4Leaf
    "console" -> Icons.Rounded.SportsEsports to MaterialShapes.SoftBurst
    "printer" -> Icons.Rounded.Print to MaterialShapes.Pill
    "iot" -> Icons.Rounded.Sensors to MaterialShapes.Sunny
    "network" -> Icons.Rounded.Router to MaterialShapes.Gem
    else -> Icons.Rounded.Devices to MaterialShapes.Cookie7Sided
}

// ---------- метки и состояния ----------

enum class Tone { Neutral, Primary, Secondary, Tertiary, Ok, Warn, Err, Inverse }

@Composable
fun toneColors(t: Tone): Pair<Color, Color> {
    val cs = MaterialTheme.colorScheme
    val x = LocalExtra.current
    return when (t) {
        Tone.Neutral -> cs.surfaceContainerHighest to cs.onSurfaceVariant
        Tone.Primary -> cs.primaryContainer to cs.onPrimaryContainer
        Tone.Secondary -> cs.secondaryContainer to cs.onSecondaryContainer
        Tone.Tertiary -> cs.tertiaryContainer to cs.onTertiaryContainer
        Tone.Ok -> x.okContainer to x.onOkContainer
        Tone.Warn -> x.warnContainer to x.onWarnContainer
        Tone.Err -> cs.errorContainer to cs.onErrorContainer
        Tone.Inverse -> cs.primary to cs.onPrimary
    }
}

/** Небольшая метка состояния. */
@Composable
fun Tag(text: String, tone: Tone = Tone.Neutral, icon: ImageVector? = null, modifier: Modifier = Modifier) {
    val (bg, fg) = toneColors(tone)
    Surface(color = bg, contentColor = fg, shape = RoundedCornerShape(8.dp), modifier = modifier) {
        Row(Modifier.padding(horizontal = 8.dp, vertical = 3.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            if (icon != null) Icon(icon, null, Modifier.size(14.dp))
            Text(text, style = MaterialTheme.typography.labelMedium, maxLines = 1)
        }
    }
}

@Composable
fun Dot(color: Color, size: Dp = 8.dp) = Box(Modifier.size(size).clip(CircleShape).background(color))

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun LatencyView(l: Latency?) {
    val x = LocalExtra.current
    when {
        l == null -> Text("—", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        l.waiting -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            LoadingIndicator(Modifier.size(24.dp))
            Text("проверка", color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.labelLarge)
        }
        l.ms <= 0 -> Tag("не отвечает", Tone.Err)
        else -> {
            val col = when { l.ms < 200 -> x.ok; l.ms < 500 -> x.warn; else -> MaterialTheme.colorScheme.error }
            val bars = when { l.ms < 120 -> Icons.Rounded.SignalCellularAlt; l.ms < 300 -> Icons.Rounded.SignalCellularAlt2Bar; else -> Icons.Rounded.SignalCellularAlt1Bar }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Icon(bars, null, tint = col, modifier = Modifier.size(18.dp))
                Text("${l.ms} мс", color = col, style = MaterialTheme.typography.labelLarge, fontFamily = LocalMono.current)
                val how = when (l.method) { "tcp" -> "TCP"; "icmp" -> "ICMP"; else -> null }
                if (how != null) Text(how, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

// ---------- группа кнопок с соединением ----------

/** Выбор одного из вариантов — connected button group M3E. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun <T> Connected(
    options: List<Pair<T, String>>,
    selected: T,
    onSelect: (T) -> Unit,
    modifier: Modifier = Modifier,
    fill: Boolean = false,
    counts: Map<T, Int> = emptyMap(),
) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(ButtonGroupDefaults.ConnectedSpaceBetween)) {
        options.forEachIndexed { i, (v, label) ->
            val on = v == selected
            ToggleButton(
                checked = on,
                onCheckedChange = { if (!on) onSelect(v) },
                modifier = if (fill) Modifier.weight(1f) else Modifier,
                shapes = when {
                    options.size == 1 -> ToggleButtonDefaults.shapes()
                    i == 0 -> ButtonGroupDefaults.connectedLeadingButtonShapes()
                    i == options.lastIndex -> ButtonGroupDefaults.connectedTrailingButtonShapes()
                    else -> ButtonGroupDefaults.connectedMiddleButtonShapes()
                },
            ) {
                AnimatedVisibility(on, enter = expandHorizontally(MaterialTheme.motionScheme.fastSpatialSpec()) + fadeIn(), exit = shrinkHorizontally(MaterialTheme.motionScheme.fastSpatialSpec()) + fadeOut()) {
                    Icon(Icons.Rounded.Check, null, Modifier.padding(end = 6.dp).size(18.dp))
                }
                Text(label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                counts[v]?.let { Text("  $it", style = MaterialTheme.typography.labelMedium, modifier = Modifier.alpha(0.75f)) }
            }
        }
    }
}

fun Modifier.alpha(a: Float) = this.graphicsLayer { alpha = a }

// ---------- карточки и заголовки ----------

@Composable
fun SectionCard(
    modifier: Modifier = Modifier,
    title: String? = null,
    action: (@Composable () -> Unit)? = null,
    color: Color = MaterialTheme.colorScheme.surfaceContainerLowest,
    padding: PaddingValues = PaddingValues(20.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    Surface(modifier.fillMaxWidth(), shape = MaterialTheme.shapes.extraLarge, color = color) {
        Column(Modifier.padding(padding), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (title != null || action != null) Row(verticalAlignment = Alignment.CenterVertically) {
                if (title != null) Text(title, style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
                action?.invoke()
            }
            content()
        }
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun PageHeader(title: String, sub: String? = null, actions: @Composable RowScope.() -> Unit = {}) {
    val compact = LocalCompact.current
    Row(Modifier.fillMaxWidth().padding(top = if (compact) 0.dp else 8.dp, bottom = 4.dp), verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Column(Modifier.weight(1f)) {
            if (!compact) Text(title, style = MaterialTheme.typography.displaySmallEmphasized)
            if (sub != null) Text(sub, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        actions()
    }
}

@Composable
fun SectionTitle(text: String, modifier: Modifier = Modifier) =
    Text(text, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.primary, modifier = modifier.padding(start = 4.dp, top = 8.dp, bottom = 2.dp))

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun Empty(icon: ImageVector, title: String, text: String = "", action: (@Composable () -> Unit)? = null) {
    Column(Modifier.fillMaxWidth().padding(vertical = 40.dp, horizontal = 16.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        ShapeIcon(icon, MaterialShapes.Cookie9Sided, size = 64.dp, container = MaterialTheme.colorScheme.secondaryContainer, content = MaterialTheme.colorScheme.onSecondaryContainer)
        Text(title, style = MaterialTheme.typography.titleMedium)
        if (text.isNotEmpty()) Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        action?.invoke()
    }
}

@Composable
fun Notice(text: String, tone: Tone = Tone.Warn, icon: ImageVector = Icons.Rounded.Info, action: (@Composable () -> Unit)? = null) {
    val (bg, fg) = toneColors(tone)
    Surface(color = bg, contentColor = fg, shape = MaterialTheme.shapes.large, modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(icon, null)
            Text(text, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
            action?.invoke()
        }
    }
}

/** Центрированная загрузка страницы — индикатор, меняющий форму. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun Loading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().padding(vertical = 96.dp), contentAlignment = Alignment.Center) { ContainedLoadingIndicator(Modifier.size(72.dp)) }
}

// ---------- списки из сегментов ----------

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun SegItem(
    index: Int,
    count: Int,
    headline: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    supporting: (@Composable () -> Unit)? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
    selected: Boolean = false,
    onClick: (() -> Unit)? = null,
) {
    val shapes = ListItemDefaults.segmentedShapes(index, count)
    if (onClick != null) {
        SegmentedListItem(
            selected = selected, onClick = onClick, shapes = shapes, modifier = modifier,
            leadingContent = leading, trailingContent = trailing, supportingContent = supporting, content = headline,
        )
    } else {
        SegmentedListItem(
            onClick = {}, enabled = true, shapes = shapes, modifier = modifier,
            leadingContent = leading, trailingContent = trailing, supportingContent = supporting, content = headline,
        )
    }
}

@Composable
fun SegGroup(content: @Composable ColumnScope.() -> Unit) =
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(ListItemDefaults.SegmentedGap), content = content)

// ---------- поля ----------

@Composable
fun Field(
    label: String,
    value: String,
    onChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    hint: String? = null,
    error: String? = null,
    placeholder: String? = null,
    mono: Boolean = false,
    lines: Int = 1,
    password: Boolean = false,
    number: Boolean = false,
    leading: ImageVector? = null,
    trailing: (@Composable () -> Unit)? = null,
) {
    OutlinedTextField(
        value = value, onValueChange = onChange, modifier = modifier.fillMaxWidth(),
        label = { Text(label) },
        placeholder = placeholder?.let { { Text(it) } },
        supportingText = (error?.takeIf { it.isNotEmpty() } ?: hint)?.let { { Text(it) } },
        isError = !error.isNullOrEmpty(),
        singleLine = lines == 1, minLines = lines,
        textStyle = if (mono) MaterialTheme.typography.bodyLarge.copy(fontFamily = LocalMono.current) else MaterialTheme.typography.bodyLarge,
        visualTransformation = if (password) androidx.compose.ui.text.input.PasswordVisualTransformation() else VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = when { password -> KeyboardType.Password; number -> KeyboardType.Number; else -> KeyboardType.Text }),
        leadingIcon = leading?.let { { Icon(it, null) } },
        trailingIcon = trailing,
    )
}

/** Выпадающий список (exposed dropdown menu). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> Select(label: String, options: List<Pair<T, String>>, value: T, onChange: (T) -> Unit, modifier: Modifier = Modifier, hint: String? = null) {
    var open by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(expanded = open, onExpandedChange = { open = it }, modifier = modifier) {
        OutlinedTextField(
            value = options.firstOrNull { it.first == value }?.second ?: value.toString(),
            onValueChange = {}, readOnly = true, singleLine = true,
            label = { Text(label) },
            supportingText = hint?.let { { Text(it) } },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(open) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable),
        )
        ExposedDropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { (v, l) ->
                DropdownMenuItem(
                    text = { Text(l) }, onClick = { onChange(v); open = false },
                    leadingIcon = if (v == value) ({ Icon(Icons.Rounded.Check, null) }) else null,
                    contentPadding = ExposedDropdownMenuDefaults.ItemContentPadding,
                )
            }
        }
    }
}

@Composable
fun NpSwitch(checked: Boolean, onChange: (Boolean) -> Unit, enabled: Boolean = true) {
    Switch(
        checked = checked, onCheckedChange = onChange, enabled = enabled,
        thumbContent = if (checked) ({ Icon(Icons.Rounded.Check, null, Modifier.size(SwitchDefaults.IconSize)) }) else null,
    )
}

/** Строка настройки: заголовок, пояснение и элемент справа. */
@Composable
fun SettingRow(index: Int, count: Int, title: String, sub: String? = null, icon: ImageVector? = null, onClick: (() -> Unit)? = null, trailing: (@Composable () -> Unit)? = null) {
    SegItem(
        index, count,
        headline = { Text(title) },
        supporting = sub?.let { { Text(it) } },
        leading = icon?.let { { Icon(it, null) } },
        trailing = trailing,
        onClick = onClick,
    )
}

@Composable
fun SwitchRow(index: Int, count: Int, title: String, sub: String? = null, icon: ImageVector? = null, checked: Boolean, onChange: (Boolean) -> Unit) =
    SettingRow(index, count, title, sub, icon, onClick = { onChange(!checked) }) { NpSwitch(checked, onChange) }

// ---------- окна: диалог на ПК, лист снизу на телефоне ----------

class DialogScope(val close: (after: () -> Unit) -> Unit)

/**
 * Окно. На ПК — диалог M3 (скругление 28) с пружинным появлением и уходом,
 * на телефоне — лист снизу. Действия получают close { … } — сначала анимация.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun AppDialog(
    onDismiss: () -> Unit,
    title: String?,
    width: Dp = 560.dp,
    icon: ImageVector? = null,
    actions: @Composable RowScope.(DialogScope) -> Unit = {},
    content: @Composable ColumnScope.(DialogScope) -> Unit,
) {
    val compact = LocalCompact.current
    val scope = rememberCoroutineScope()
    if (compact) {
        val sheet = rememberModalBottomSheetState(skipPartiallyExpanded = true)
        val ds = remember { DialogScope { after -> scope.launch { sheet.hide(); onDismiss(); after() } } }
        ModalBottomSheet(onDismissRequest = onDismiss, sheetState = sheet, containerColor = MaterialTheme.colorScheme.surfaceContainerLow) {
            Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                if (title != null) Text(title, style = MaterialTheme.typography.headlineSmall)
                Column(Modifier.weight(1f, fill = false).verticalScrollable(), verticalArrangement = Arrangement.spacedBy(16.dp)) { content(ds) }
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) { actions(ds) }
            }
        }
        return
    }
    val visible = remember { MutableTransitionState(false).apply { targetState = true } }
    // Закрытие: сначала анимация ухода, затем окно убирается.
    val ds = remember {
        DialogScope { after ->
            if (visible.targetState) {
                visible.targetState = false
                scope.launch { delay(260); onDismiss(); after() }
            }
        }
    }
    Dialog(onDismissRequest = { ds.close {} }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        AnimatedVisibility(
            visibleState = visible,
            enter = fadeIn(MaterialTheme.motionScheme.defaultEffectsSpec()) + scaleIn(MaterialTheme.motionScheme.defaultSpatialSpec(), initialScale = 0.85f),
            exit = fadeOut(MaterialTheme.motionScheme.fastEffectsSpec()) + scaleOut(MaterialTheme.motionScheme.fastSpatialSpec(), targetScale = 0.9f),
        ) {
            Surface(
                shape = MaterialTheme.shapes.extraLarge, color = MaterialTheme.colorScheme.surfaceContainerHigh, tonalElevation = 6.dp,
                modifier = Modifier.padding(24.dp).widthIn(max = width).fillMaxWidth(),
            ) {
                Column(Modifier.padding(24.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                    if (icon != null) Icon(icon, null, Modifier.align(Alignment.CenterHorizontally), tint = MaterialTheme.colorScheme.secondary)
                    if (title != null) Text(title, style = MaterialTheme.typography.headlineSmall, modifier = if (icon != null) Modifier.align(Alignment.CenterHorizontally) else Modifier)
                    Column(Modifier.weight(1f, fill = false).verticalScrollable(), verticalArrangement = Arrangement.spacedBy(16.dp)) { content(ds) }
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) { actions(ds) }
                }
            }
        }
    }
}


/** Подтверждение опасного действия. */
@Composable
fun ConfirmDialog(text: String, confirm: String = "Удалить", danger: Boolean = true, onDismiss: () -> Unit, onConfirm: suspend () -> Unit) {
    val app = LocalApp.current
    var busy by remember { mutableStateOf(false) }
    AppDialog(onDismiss, "Подтверждение", width = 440.dp, actions = { d ->
        TextButton(onClick = { d.close {} }) { Text("Отмена") }
        Button(
            onClick = { busy = true; app.launch { try { onConfirm(); d.close {} } finally { busy = false } } },
            enabled = !busy,
            colors = if (danger) ButtonDefaults.buttonColors(MaterialTheme.colorScheme.error, MaterialTheme.colorScheme.onError) else ButtonDefaults.buttonColors(),
        ) { Text(confirm) }
    }) { Text(text, style = MaterialTheme.typography.bodyLarge) }
}

/** Кнопка с действием и состоянием «выполняется». */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun BusyButton(
    text: String,
    icon: ImageVector? = null,
    style: String = "filled",
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
    action: suspend () -> Unit,
) {
    val app = LocalApp.current
    var busy by remember { mutableStateOf(false) }
    val onClick: () -> Unit = { busy = true; app.launch { try { action() } finally { busy = false } } }
    val inner: @Composable RowScope.() -> Unit = {
        AnimatedContent(busy, transitionSpec = { fadeIn() togetherWith fadeOut() }) { b ->
            if (b) LoadingIndicator(Modifier.size(20.dp), color = LocalContentColor.current)
            else if (icon != null) Icon(icon, null, Modifier.size(ButtonDefaults.IconSize))
        }
        if (icon != null || busy) Spacer(Modifier.width(ButtonDefaults.IconSpacing))
        Text(text)
    }
    val shapes = ButtonDefaults.shapes()
    when (style) {
        "tonal" -> FilledTonalButton(onClick = onClick, modifier = modifier, enabled = enabled && !busy, shapes = shapes, content = inner)
        "outlined" -> OutlinedButton(onClick = onClick, modifier = modifier, enabled = enabled && !busy, shapes = shapes, content = inner)
        "text" -> TextButton(onClick = onClick, modifier = modifier, enabled = enabled && !busy, shapes = shapes, content = inner)
        "error" -> Button(onClick = onClick, modifier = modifier, enabled = enabled && !busy, shapes = shapes, colors = ButtonDefaults.buttonColors(MaterialTheme.colorScheme.error, MaterialTheme.colorScheme.onError), content = inner)
        else -> Button(onClick = onClick, modifier = modifier, enabled = enabled && !busy, shapes = shapes, content = inner)
    }
}

/** Плитка с числом (обзор, соединения). */
@Composable
fun StatTile(label: String, value: String, modifier: Modifier = Modifier, small: String? = null, sub: String? = null, tone: Tone? = null, icon: ImageVector? = null, shape: RoundedPolygon? = null, onClick: (() -> Unit)? = null) {
    val (bg, fg) = if (tone != null) toneColors(tone) else MaterialTheme.colorScheme.surfaceContainerLowest to MaterialTheme.colorScheme.onSurface
    var hover by remember { mutableStateOf(false) }
    val corner by animateDpAsState(if (hover) 32.dp else 24.dp, MaterialTheme.motionScheme.defaultSpatialSpec())
    Surface(
        modifier = modifier.then(if (onClick != null) Modifier.onHover { hover = it }.clickable(onClick = onClick) else Modifier),
        shape = RoundedCornerShape(corner), color = bg, contentColor = fg,
    ) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (icon != null && shape != null) ShapeIcon(icon, shape, active = hover, container = MaterialTheme.colorScheme.secondaryContainer, content = MaterialTheme.colorScheme.onSecondaryContainer)
            Text(label, style = MaterialTheme.typography.labelLarge, color = if (tone != null) fg else MaterialTheme.colorScheme.onSurfaceVariant)
            Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(value, style = MaterialTheme.typography.headlineMediumEmphasized)
                if (small != null) Text(small, style = MaterialTheme.typography.bodyMedium, color = if (tone != null) fg else MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(bottom = 4.dp))
            }
            if (sub != null) Text(sub, style = MaterialTheme.typography.bodySmall, color = if (tone != null) fg else MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** Наведение мыши (для смены формы при наведении). */
@Composable
fun Modifier.onHover(onHover: (Boolean) -> Unit): Modifier {
    val src = remember { MutableInteractionSource() }
    val h by src.collectIsHoveredAsState()
    LaunchedEffect(h) { onHover(h) }
    return this.hoverable(src)
}

/** Пауза для анимаций последовательного появления. */
suspend fun settle(ms: Long = 60) = delay(ms)

/**
 * Плавающая панель инструментов M3 Expressive (floating toolbar):
 * «пилюля» высотой 64 dp, vibrant — на primaryContainer, standard — на surfaceContainer.
 */
@Composable
fun FloatingBar(vibrant: Boolean = true, modifier: Modifier = Modifier, content: @Composable RowScope.() -> Unit) {
    val cs = MaterialTheme.colorScheme
    Surface(
        modifier = modifier.height(64.dp),
        shape = CircleShape,
        color = if (vibrant) cs.primaryContainer else cs.surfaceContainer,
        contentColor = if (vibrant) cs.onPrimaryContainer else cs.onSurface,
        shadowElevation = 6.dp,
    ) {
        Row(Modifier.fillMaxHeight().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp), content = content)
    }
}
