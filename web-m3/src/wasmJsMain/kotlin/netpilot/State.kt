package netpilot

// Общее состояние интерфейса: раздел, статус шлюза, черновики, уведомления, оформление.

import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarVisuals
import androidx.compose.runtime.*
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

enum class Page(val key: String, val title: String) {
    Overview("", "Обзор"), Servers("nodes", "Серверы"), Rules("rules", "Правила"), Devices("devices", "Устройства"),
    Connections("connections", "Соединения"), Internet("internet", "Интернет"), Settings("settings", "Настройки");

    companion object {
        fun fromHash(h: String): Page {
            val k = h.removePrefix("#").removePrefix("/").substringBefore('/')
            return entries.firstOrNull { it.key == k } ?: Overview
        }
    }
}

enum class Kind { Ok, Err, Warn, Info }

class Toast(override val message: String, val kind: Kind, val title: String?) : SnackbarVisuals {
    override val actionLabel: String? = null
    override val withDismissAction: Boolean = true
    override val duration: SnackbarDuration = if (kind == Kind.Err) SnackbarDuration.Long else SnackbarDuration.Short
}

/** Форма страницы целиком: несохранённые правки, сохранение, черновик для перерисовки. */
class FormGuard(val isDirty: () -> Boolean, val save: suspend () -> String?)

data class Design(val design: String = "m3", val scheme: String = "indigo", val mode: String = "system") {
    companion object {
        fun of(s: J?) = Design(
            design = if (s?.s("design") == "classic") "classic" else "m3",
            scheme = s?.s("designScheme").orEmpty().ifEmpty { "indigo" },
            mode = s?.s("designMode").orEmpty().ifEmpty { "system" },
        )
    }
}

data class Latency(val ms: Int, val method: String, val error: String = "", val waiting: Boolean = false) {
    companion object {
        fun of(j: J) = Latency(j.i("ms"), j.s("method"), j.s("error"))
        val Wait = Latency(0, "", waiting = true)
    }
}

data class UpdateProgress(val target: String?, val stage: String = "prepare", val done: Long = 0, val total: Long = 0, val restarting: Boolean = false, val note: String? = null)

enum class Auth { Checking, Login, Setup, In }

class AppState(val scope: CoroutineScope) {
    var page by mutableStateOf(Page.fromHash(currentHash()))
    var auth by mutableStateOf(Auth.Checking)
    var status by mutableStateOf<J?>(null)
    var update by mutableStateOf<J?>(null)
    var activeName by mutableStateOf("…")
    var counts by mutableStateOf(mapOf<String, Int>())
    val latency = mutableStateMapOf<String, Latency>()
    val snackbar = SnackbarHostState()

    /** Сохранённое оформление и черновик из настроек (до нажатия «Сохранить»). */
    var savedDesign by mutableStateOf(Design.of(null).copy(scheme = storageGet("np-m3-scheme") ?: "indigo", mode = storageGet("np-m3-mode") ?: "system"))
    var previewDesign by mutableStateOf<Design?>(null)
    val design: Design get() = previewDesign ?: savedDesign

    /** Отложенные правки: ключ → сохранение. Последняя правка по ключу побеждает. */
    val pending = mutableStateMapOf<String, suspend () -> Unit>()
    var form by mutableStateOf<FormGuard?>(null)
    val dirty: Boolean get() = pending.isNotEmpty() || form?.isDirty?.invoke() == true

    /** Увеличивается, когда текущую страницу нужно загрузить заново. */
    var reload by mutableIntStateOf(0)
    var leaveTo by mutableStateOf<(() -> Unit)?>(null)
    var paletteOpen by mutableStateOf(false)
    var moreOpen by mutableStateOf(false)
    var pickerOpen by mutableStateOf(false)
    var updating by mutableStateOf<UpdateProgress?>(null)
    var explainHost by mutableStateOf<String?>(null)

    val applier = Applier(scope, { toast(it, Kind.Err) }) { refreshStatus() }

    fun toast(msg: String, kind: Kind = Kind.Info, title: String? = null) {
        scope.launch {
            val r = snackbar.showSnackbar(Toast(msg, kind, title))
            if (r == SnackbarResult.Dismissed) Unit
        }
    }

    fun launch(block: suspend () -> Unit) = scope.launch {
        try { block() } catch (e: ApiError) { if (e.status != 401) toast(e.message ?: "Ошибка", Kind.Err) }
    }

    fun stage(key: String, fn: suspend () -> Unit) { pending[key] = fn }

    suspend fun saveAll(): String {
        var msg: String? = null
        for ((k, fn) in pending.entries.toList()) {
            fn()
            pending.remove(k)
        }
        val f = form
        if (f != null && f.isDirty()) msg = f.save()
        return msg ?: "Изменения сохранены"
    }

    fun discardAll() {
        pending.clear()
        form = null
        previewDesign = null
        refreshPage()
    }

    /** Перерисовать страницу с данными сервера (после явного сохранения). */
    fun refreshPage() {
        reload++
    }

    /** Переход на раздел: с проверкой несохранённого. */
    fun go(p: Page, force: Boolean = false) {
        if (p == page && !force) return
        if (dirty && !force) {
            leaveTo = { navigate(p) }
            return
        }
        navigate(p)
    }

    private fun navigate(p: Page) {
        pending.clear()
        form = null
        previewDesign = null
        page = p
        val h = "#/" + p.key
        if (currentHash() != h && !(p == Page.Overview && currentHash().isEmpty())) setHash(h)
        setTitle(p.title + " — NetPilot")
    }

    /** Смена адреса извне (кнопка «назад»). */
    fun onHash() {
        val p = Page.fromHash(currentHash())
        if (p == page) return
        if (dirty) {
            val back = page
            setHash("#/" + back.key)
            leaveTo = { navigate(p) }
            return
        }
        navigate(p)
    }

    suspend fun refreshStatus() {
        try { status = Api.obj("/status") } catch (_: Throwable) {}
    }

    suspend fun refreshActive() {
        try {
            val s = Api.obj("/settings")
            val nodes = Api.list("/nodes")
            val groups = Api.list("/groups")
            activeName = activeName(s, nodes, groups)
            savedDesign = Design.of(s)
            storageSet("np-m3-scheme", savedDesign.scheme)
            storageSet("np-m3-mode", savedDesign.mode)
            if (savedDesign.design == "m3") storageSet("np-design", "{\"design\":\"m3\"}")
            if (savedDesign.design == "classic" && previewDesign == null) {
                // Выбрано классическое оформление — его отдаёт основной адрес.
                storageSet("np-design", "{\"design\":\"classic\"}")
                redirect("/" + currentHash())
            }
        } catch (_: Throwable) {}
    }

    suspend fun refreshCounts() {
        val net = Api.objOrNull("/network")
        val nodes = Api.listOrEmpty("/nodes")
        val rules = Api.listOrEmpty("/rules")
        counts = mapOf(
            "devices" to (net?.a("devices")?.count { isClient(it) } ?: 0),
            "nodes" to nodes.size,
            "rules" to rules.size,
        )
    }

    suspend fun loadUpdate() {
        try { update = Api.obj("/update") } catch (_: Throwable) {}
    }

    fun startPolling() {
        scope.launch {
            while (true) {
                refreshStatus()
                delay(5000)
            }
        }
        scope.launch {
            while (true) {
                delay(30 * 60 * 1000L)
                loadUpdate()
            }
        }
    }
}

val LocalApp = staticCompositionLocalOf<AppState> { error("нет AppState") }
