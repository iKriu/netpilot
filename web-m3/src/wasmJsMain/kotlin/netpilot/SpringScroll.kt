package netpilot

// Прокрутка колесом и трекпадом с пружиной и инерцией.
//
// Compose на каждое событие колеса запускает свою короткую анимацию; Safari
// на Mac шлёт события трекпада неравномерно и уже с собственной инерцией —
// анимации накладываются, и прокрутка дёргается. Поэтому колесо перехватываем
// раньше Compose и двигаем содержимое сами, ровно один раз за кадр:
//   • трекпад — дельты копятся и сглаженно применяются по кадрам;
//   • колесо мыши — пружина к цели с небольшой инерцией (чуть «доезжает»);
//   • у края — упругое оттягивание с сопротивлением и пружинный возврат.

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.spring
import androidx.compose.foundation.gestures.ScrollableState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.math.abs
import kotlin.math.exp
import kotlin.math.sign

class SpringScroller(val state: ScrollableState, val order: Long) {
    var rect: Rect = Rect.Zero
    val over = Animatable(0f)       // упругое смещение у края, px
    var remaining = 0f              // сколько ещё проехать, px
    var velocity = 0f               // px/с
    var trackpad = false
    var lastInput = 0.0
    var release: Job? = null

    fun canScroll(dy: Float) = if (dy > 0) state.canScrollForward else state.canScrollBackward
}

object SpringScroll {
    private val scrollers = mutableListOf<SpringScroller>()
    private var counter = 0L
    private var installed = false
    private lateinit var scope: CoroutineScope

    // Пружина для колеса мыши: чуть недодемпфирована — содержимое мягко «доезжает».
    private const val STIFFNESS = 170f
    private const val DAMPING = 0.92f
    private const val MAX_OVER = 140f // предел оттягивания, dp

    fun register(state: ScrollableState): SpringScroller = SpringScroller(state, counter++).also { scrollers.add(it) }
    fun unregister(s: SpringScroller) { scrollers.remove(s) }

    /** Подключается один раз: перехват колеса и покадровый цикл. */
    fun install(scope: CoroutineScope) {
        if (installed) return
        installed = true
        this.scope = scope
        onWheel { x, y, dx, dy, trackpad ->
            if (abs(dx) > abs(dy) || dy == 0.0) return@onWheel false
            val dpr = devicePixelRatio().toFloat()
            val px = x.toFloat() * dpr
            val py = y.toFloat() * dpr
            // Последний зарегистрированный (окно поверх страницы, вложенный список)
            // под указателем, который может прокрутиться; иначе — последний под указателем.
            val under = scrollers.filter { it.rect.contains(androidx.compose.ui.geometry.Offset(px, py)) }.sortedByDescending { it.order }
            if (under.isEmpty()) return@onWheel false
            val delta = dy.toFloat() * dpr
            val s = under.firstOrNull { it.canScroll(delta) || it.over.value != 0f } ?: under.first()
            feed(s, delta, trackpad)
            true
        }
        scope.launch {
            var last = 0L
            while (true) {
                withFrameNanos { now ->
                    val dt = if (last == 0L) 1f / 60 else ((now - last) / 1e9f).coerceIn(0.001f, 0.05f)
                    last = now
                    for (s in scrollers.toList()) step(s, dt)
                }
            }
        }
    }

    private fun feed(s: SpringScroller, delta: Float, trackpad: Boolean) {
        s.trackpad = trackpad
        s.lastInput = nowMs()
        s.release?.cancel()
        // Вернулись от края: сначала убираем оттягивание.
        if (s.over.value != 0f && sign(delta) == sign(s.over.value)) {
            val back = minOf(abs(delta), abs(s.over.value)) * sign(s.over.value)
            scope.launch { s.over.snapTo(s.over.value - back) }
            s.remaining += delta - back
        } else {
            s.remaining += delta
        }
        // Отпустили у края — пружинный возврат.
        s.release = scope.launch {
            delay(140)
            if (s.over.value != 0f) {
                s.remaining = 0f; s.velocity = 0f
                s.over.animateTo(0f, spring(dampingRatio = 0.62f, stiffness = 260f))
            }
        }
    }

    private fun step(s: SpringScroller, dt: Float) {
        if (abs(s.remaining) < 0.3f && abs(s.velocity) < 5f) {
            s.remaining = 0f; s.velocity = 0f
            return
        }
        val move: Float
        if (s.trackpad) {
            // У трекпада своя инерция: просто сглаживаем по кадрам (≈ 45 мс).
            move = s.remaining * (1f - exp(-dt / 0.045f))
            s.velocity = move / dt
        } else {
            // Пружина к цели: ускорение к оставшемуся пути с затуханием.
            val k = STIFFNESS
            val c = 2f * DAMPING * kotlin.math.sqrt(k)
            s.velocity += (k * s.remaining - c * s.velocity) * dt
            move = s.velocity * dt
        }
        s.remaining -= move
        val used = s.state.dispatchRawDelta(move)
        val left = move - used
        if (abs(left) > 0.5f) {
            // Край: остаток уходит в упругое оттягивание с растущим сопротивлением.
            val maxOver = MAX_OVER * devicePixelRatio().toFloat()
            val cur = s.over.value
            val resist = 0.35f * (1f - (abs(cur) / maxOver).coerceIn(0f, 1f))
            val next = (cur - left * resist).coerceIn(-maxOver, maxOver)
            scope.launch { s.over.snapTo(next) }
            s.remaining = 0f; s.velocity = 0f
        }
    }
}

/** Прокрутка с пружиной и инерцией (для LazyColumn, Column и т. п.). */
@Composable
fun Modifier.springScroll(state: ScrollableState): Modifier {
    val s = remember(state) { SpringScroll.register(state) }
    DisposableEffect(s) { onDispose { SpringScroll.unregister(s) } }
    return this
        .onGloballyPositioned { s.rect = it.boundsInWindow() }
        .graphicsLayer { translationY = s.over.value }
}

/** Вертикальная прокрутка с пружиной. */
@Composable
fun Modifier.verticalScrollable(): Modifier {
    val st = rememberScrollState()
    return this.springScroll(st).verticalScroll(st)
}
