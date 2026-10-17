package netpilot

// JSON и запросы к API. Объекты сервера храним как JsonObject целиком:
// при сохранении меняем только нужные поля, остальные возвращаются как были.

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.*

typealias J = JsonObject

val JsonFmt = Json { ignoreUnknownKeys = true; isLenient = true }

fun J.s(k: String, d: String = ""): String = (this[k] as? JsonPrimitive)?.contentOrNull ?: d
fun J.i(k: String): Int = (this[k] as? JsonPrimitive)?.let { it.intOrNull ?: it.doubleOrNull?.toInt() } ?: 0
fun J.l(k: String): Long = (this[k] as? JsonPrimitive)?.let { it.longOrNull ?: it.doubleOrNull?.toLong() } ?: 0L
fun J.d(k: String): Double = (this[k] as? JsonPrimitive)?.doubleOrNull ?: 0.0
fun J.b(k: String): Boolean = (this[k] as? JsonPrimitive)?.booleanOrNull ?: false
fun J.o(k: String): J? = this[k] as? JsonObject
fun J.a(k: String): List<J> = (this[k] as? JsonArray)?.mapNotNull { it as? JsonObject } ?: emptyList()
fun J.sl(k: String): List<String> = (this[k] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull } ?: emptyList()
fun J.il(k: String): List<Int> = (this[k] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.intOrNull } ?: emptyList()
fun J.has(k: String): Boolean = this[k] != null && this[k] !is JsonNull
val J.id: String get() = s("id")
val J.name: String get() = s("name")

fun toJson(v: Any?): JsonElement = when (v) {
    null -> JsonNull
    is JsonElement -> v
    is String -> JsonPrimitive(v)
    is Number -> JsonPrimitive(v)
    is Boolean -> JsonPrimitive(v)
    is List<*> -> JsonArray(v.map { toJson(it) })
    is Set<*> -> JsonArray(v.map { toJson(it) })
    is Map<*, *> -> JsonObject(v.entries.associate { it.key.toString() to toJson(it.value) })
    else -> JsonPrimitive(v.toString())
}

fun jobj(vararg pairs: Pair<String, Any?>): J = JsonObject(pairs.associate { it.first to toJson(it.second) })

/** Копия объекта с изменёнными полями. */
fun J.with(vararg pairs: Pair<String, Any?>): J = JsonObject(this + pairs.associate { it.first to toJson(it.second) })

/** Изменение вложенного объекта: rule.withIn("what", "apps" to listOf(...)). */
fun J.withIn(key: String, vararg pairs: Pair<String, Any?>): J = with(key to (o(key) ?: jobj()).with(*pairs))

class ApiError(message: String, val status: Int = 0) : Exception(message)

object Api {
    var onAuthLost: () -> Unit = {}
    var onMutation: () -> Unit = {}

    // Запросы, после которых не нужно применять конфигурацию к ядру.
    private val noApply = Regex("^/(core|auth|nodes/test|network/scan|lists|router|backup|restore|update|devices/cleanup)")

    suspend fun call(method: String, path: String, body: JsonElement? = null, rawBody: String? = null): JsonElement {
        val (status, text) = try {
            http(method, "/api$path", rawBody ?: body?.toString())
        } catch (e: NetworkError) {
            throw ApiError(e.message ?: "Нет связи", 0)
        }
        val data = if (text.isBlank()) JsonNull else runCatching { JsonFmt.parseToJsonElement(text) }.getOrDefault(JsonNull)
        if (status == 401 && !path.startsWith("/auth/")) {
            onAuthLost()
            throw ApiError("Требуется вход", 401)
        }
        if (status !in 200..299) {
            val msg = (data as? JsonObject)?.s("error").orEmpty().ifEmpty { "Ошибка $status" }
            throw ApiError(msg, status)
        }
        if (method != "GET" && !noApply.containsMatchIn(path)) onMutation()
        return data
    }

    suspend fun get(path: String) = call("GET", path)
    suspend fun obj(path: String): J = get(path) as? JsonObject ?: jobj()
    suspend fun list(path: String): List<J> = (get(path) as? JsonArray)?.mapNotNull { it as? JsonObject } ?: emptyList()
    suspend fun post(path: String, body: Any? = null) = call("POST", path, body?.let { toJson(it) })
    suspend fun put(path: String, body: Any?) = call("PUT", path, toJson(body))
    suspend fun delete(path: String) = call("DELETE", path)

    suspend fun objOrNull(path: String): J? = try { obj(path) } catch (e: ApiError) { if (e.status == 401) throw e; null }
    suspend fun listOrEmpty(path: String): List<J> = try { list(path) } catch (e: ApiError) { if (e.status == 401) throw e; emptyList() }
}

/** Применение к ядру после сохранения: серия запросов — одно применение. */
class Applier(private val scope: CoroutineScope, private val onError: (String) -> Unit, private val after: suspend () -> Unit) {
    private var job: Job? = null
    private var applying = false

    fun schedule() {
        job?.cancel()
        job = scope.launch {
            delay(600)
            while (applying) delay(200)
            val st = try { Api.obj("/status") } catch (e: Throwable) { return@launch }
            if (!st.b("running") || !st.b("dirty")) return@launch
            applying = true
            try {
                Api.post("/core/apply")
            } catch (e: ApiError) {
                onError("Сохранено, но не применено: ${e.message}")
            } finally {
                applying = false
                after()
            }
        }
    }
}
