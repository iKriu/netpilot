package netpilot

// Связь с браузером: запросы, адрес страницы, хранилище, файлы, буфер обмена.

import kotlinx.browser.document
import kotlinx.browser.localStorage
import kotlinx.browser.window
import kotlinx.coroutines.await
import kotlinx.coroutines.suspendCancellableCoroutine
import org.w3c.dom.HTMLInputElement
import org.w3c.dom.events.Event
import org.w3c.dom.events.KeyboardEvent
import org.w3c.fetch.Response
import org.w3c.files.FileReader
import org.w3c.files.get
import kotlin.coroutines.resume

class NetworkError(message: String) : Exception(message)

// fetch напрямую: обёртка RequestInit передаёт null в необязательные поля, и браузер отказывает.
private fun jsFetch(method: String, url: String, body: String?): kotlin.js.Promise<Response> =
    js("fetch(url, body == null ? { method: method, credentials: 'same-origin' } : { method: method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: body })")

suspend fun http(method: String, url: String, body: String?): Pair<Int, String> {
    val res: Response = try {
        jsFetch(method, url, body).await()
    } catch (e: Throwable) {
        println("NetPilot: запрос $method $url не удался: $e")
        throw NetworkError("Нет связи с NetPilot")
    }
    val text = try { res.text().await<JsString>().toString() } catch (e: Throwable) { "" }
    return res.status.toInt() to text
}

fun nowMs(): Double = js("Date.now()")
fun nowSec(): Long = (nowMs() / 1000).toLong()

// ---------- адрес страницы ----------

fun currentHash(): String = window.location.hash

fun setHash(h: String) {
    window.location.hash = h
}

fun onHashChange(cb: () -> Unit) {
    window.addEventListener("hashchange", { _: Event -> cb() })
}

fun redirect(url: String) = window.location.replace(url)
fun reloadPage() = window.location.reload()
fun openUrl(url: String) { window.location.href = url }

fun onCtrlK(cb: () -> Unit) {
    window.addEventListener("keydown", { e: Event ->
        val k = e as KeyboardEvent
        if ((k.ctrlKey || k.metaKey) && k.key.lowercase() == "k") {
            k.preventDefault()
            cb()
        }
    })
}

private var beforeUnloadDirty: () -> Boolean = { false }
fun guardUnload(dirty: () -> Boolean) {
    beforeUnloadDirty = dirty
    window.addEventListener("beforeunload", { e: Event ->
        if (beforeUnloadDirty()) e.preventDefault()
    })
}

// ---------- хранилище ----------

fun storageGet(key: String): String? = try { localStorage.getItem(key) } catch (e: Throwable) { null }
fun storageSet(key: String, value: String?) {
    try { if (value == null) localStorage.removeItem(key) else localStorage.setItem(key, value) } catch (_: Throwable) {}
}
fun sessionGet(key: String): String? = try { window.sessionStorage.getItem(key) } catch (e: Throwable) { null }
fun sessionSet(key: String, value: String?) {
    try { if (value == null) window.sessionStorage.removeItem(key) else window.sessionStorage.setItem(key, value) } catch (_: Throwable) {}
}

fun prefersDark(): Boolean = window.matchMedia("(prefers-color-scheme: dark)").matches

fun setThemeColor(hex: String) {
    document.querySelector("meta[name=theme-color]")?.setAttribute("content", hex)
    document.body?.style?.background = hex
    document.documentElement?.let { (it as? org.w3c.dom.HTMLElement)?.style?.background = hex }
}

fun setTitle(t: String) { document.title = t }

// ---------- файлы и буфер обмена ----------

fun copyText(text: String) {
    try { window.navigator.clipboard.writeText(text) } catch (_: Throwable) {}
}

/** Диалог выбора файла; возвращает имя и содержимое (текст) или null. */
suspend fun pickTextFile(accept: String): Pair<String, String>? = suspendCancellableCoroutine { cont ->
    val input = document.createElement("input") as HTMLInputElement
    input.type = "file"
    input.accept = accept
    input.style.display = "none"
    var done = false
    input.onchange = { _: Event ->
        done = true // дальше результат даст чтение файла, а не ожидание фокуса
        val file = input.files?.get(0)
        if (file == null) {
            if (cont.isActive) cont.resume(null)
        } else {
            val reader = FileReader()
            reader.onload = { _: Event ->
                if (cont.isActive) cont.resume(file.name to (reader.result?.toString() ?: ""))
                null
            }
            reader.onerror = { _: Event -> if (cont.isActive) cont.resume(null); null }
            reader.readAsText(file)
        }
        input.remove()
    }
    document.body?.appendChild(input)
    input.click()
    // Окно выбора закрыли без файла — браузер не сообщает; ждём фокуса страницы.
    window.addEventListener("focus", { _: Event ->
        window.setTimeout({
            if (!done) { done = true; input.remove(); if (cont.isActive) cont.resume(null) }
            null
        }, 1500)
    })
}

// ---------- колесо и трекпад ----------

fun devicePixelRatio(): Double = window.devicePixelRatio

private fun wheelNum(e: JsAny, k: String): Double = js("(+e[k]) || 0")
private fun wheelBool(e: JsAny, k: String): Boolean = js("!!e[k]")

// Перехват раньше Compose (capture на window), чтобы он не запускал свою анимацию.
private fun addWheelListener(cb: (JsAny) -> Boolean): Unit =
    js("window.addEventListener('wheel', function (e) { if (cb(e)) { e.preventDefault(); e.stopImmediatePropagation(); } }, { capture: true, passive: false })")

/**
 * Колесо: координаты в CSS-пикселях и дельты в пикселях. cb возвращает true,
 * если событие обработано (тогда браузер и Compose его не получают).
 */
fun onWheel(cb: (x: Double, y: Double, dx: Double, dy: Double, trackpad: Boolean) -> Boolean) {
    addWheelListener { e ->
        if (wheelBool(e, "ctrlKey")) return@addWheelListener false // масштаб щипком
        val mode = wheelNum(e, "deltaMode")
        val k = when (mode) { 1.0 -> 40.0; 2.0 -> 800.0; else -> 1.0 }
        val dy = wheelNum(e, "deltaY") * k
        val dx = wheelNum(e, "deltaX") * k
        // Трекпад: мелкие дробные шаги; у Safari и Chrome wheelDeltaY = −3·deltaY.
        val wdy = wheelNum(e, "wheelDeltaY")
        val trackpad = mode == 0.0 && (kotlin.math.abs(wdy) == kotlin.math.abs(dy) * 3 || dy % 1.0 != 0.0)
        cb(wheelNum(e, "clientX"), wheelNum(e, "clientY"), dx, dy, trackpad)
    }
}
