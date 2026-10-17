package netpilot

// Справочники и вычисления: группы, маршруты, имена, форматирование.

import kotlin.math.floor
import kotlin.math.ln
import kotlin.math.pow
import kotlin.math.roundToInt

val DEVICE_TYPES = listOf(
    "computer" to "Компьютер", "laptop" to "Ноутбук", "phone" to "Телефон", "tablet" to "Планшет",
    "tv" to "Телевизор", "console" to "Игровая приставка", "printer" to "Принтер",
    "iot" to "Умное устройство", "network" to "Сетевое оборудование", "other" to "Другое",
)
fun typeName(t: String) = DEVICE_TYPES.firstOrNull { it.first == t }?.second ?: "Другое"

val ROUTES = listOf("" to "По правилам", "proxy" to "Через прокси", "direct" to "Напрямую", "block" to "Без интернета")
fun routeName(r: String) = ROUTES.firstOrNull { it.first == r }?.second ?: "По правилам"

fun isClient(d: J) = d.b("client") || d.b("configured") || d.s("route").isNotEmpty()

/** Серверы группы — так же, как считает сервер (core.GroupNodes). */
fun groupMembers(g: J, nodes: List<J>): List<J> {
    fun rx(src: String) = if (src.isBlank()) null else runCatching { Regex(src, RegexOption.IGNORE_CASE) }.getOrNull()
    val re = rx(g.s("filter"))
    val ex = rx(g.s("exclude"))
    val ids = g.sl("nodeIds")
    return nodes.filter { n ->
        val nm = n.name
        ((ids.isEmpty() && g.s("filter").isBlank()) || n.id in ids || (re != null && re.containsMatchIn(nm))) &&
            !(ex != null && ex.containsMatchIn(nm))
    }
}

fun regexError(src: String): String =
    if (src.isBlank()) "" else runCatching { Regex(src); "" }.getOrElse { "Ошибка в выражении: " + (it.message ?: "") }

fun activeName(s: J, nodes: List<J>, groups: List<J>): String {
    val id = s.s("defaultTarget")
    groups.firstOrNull { it.id == id }?.let { return it.name }
    nodes.firstOrNull { it.id == id }?.let { return it.name }
    return when {
        nodes.size > 1 -> "Авто — все серверы"
        nodes.size == 1 -> nodes[0].name
        else -> "серверов нет"
    }
}

fun targetName(id: String, nodes: List<J>, groups: List<J>): String =
    if (id.isEmpty()) "" else (groups.firstOrNull { it.id == id } ?: nodes.firstOrNull { it.id == id })?.name ?: "удалён"

/** Варианты «через какой сервер»: "" — активный сервер, затем группы, затем серверы. */
fun targetOptions(nodes: List<J>, groups: List<J>): List<Pair<String, String>> =
    listOf("" to "Активный сервер") + groups.map { it.id to "Группа · " + it.name } + nodes.map { it.id to it.name }

// ---------- форматирование ----------

fun plural(n: Int, one: String, few: String, many: String): String {
    val m10 = n % 10
    val m100 = n % 100
    val w = if (m10 == 1 && m100 != 11) one else if (m10 in 2..4 && (m100 < 12 || m100 > 14)) few else many
    return "$n $w"
}

fun ago(unix: Long): String {
    if (unix <= 0) return "никогда"
    val s = nowSec() - unix
    return when {
        s < 60 -> "только что"
        s < 3600 -> "${s / 60} мин назад"
        s < 86400 -> "${s / 3600} ч назад"
        else -> "${s / 86400} дн назад"
    }
}

fun agoShort(unix: Long): String {
    val s = nowSec() - unix
    return when {
        s < 60 -> "$s с назад"
        s < 3600 -> "${s / 60} мин назад"
        else -> "${s / 3600} ч назад"
    }
}

fun uptime(unix: Long): String {
    val s = maxOf(0, nowSec() - unix)
    val d = s / 86400
    val h = s % 86400 / 3600
    val m = s % 3600 / 60
    return (if (d > 0) "$d д " else "") + (if (h > 0) "$h ч " else "") + "$m мин"
}

fun duration(unix: Long): String {
    val s = maxOf(0, nowSec() - unix)
    return when {
        s < 60 -> "$s с"
        s < 3600 -> "${s / 60}:${(s % 60).toString().padStart(2, '0')}"
        else -> "${s / 3600} ч ${s % 3600 / 60} мин"
    }
}

private fun fmt(v: Double, digits: Int): String {
    if (digits == 0) return v.roundToInt().toString()
    val p = 10.0.pow(digits)
    val r = (v * p).roundToInt() / p
    val s = r.toString()
    val t = if (s.contains('.')) s.trimEnd('0').trimEnd('.') else s
    return t.replace('.', ',')
}

fun bytes(n: Long): String {
    if (n <= 0) return "0 Б"
    val u = listOf("Б", "КБ", "МБ", "ГБ", "ТБ")
    val i = minOf(u.size - 1, floor(ln(n.toDouble()) / ln(1024.0)).toInt())
    return fmt(n / 1024.0.pow(i), if (i == 0) 0 else 1) + " " + u[i]
}

/** Скорость числом в МБ/с: «12,4». */
fun mbps(n: Double): String = fmt(n / 1048576.0, if (n >= 10485760) 0 else 1)

fun formatThousands(n: Int): String {
    val s = n.toString()
    return s.reversed().chunked(3).joinToString(" ").reversed()
}

/** a новее b: «1.14.3» > «1.14.2». */
fun newer(a: String, b: String): Boolean {
    fun p(v: String) = v.split('.', '-').map { it.toIntOrNull() ?: 0 }
    val x = p(a)
    val y = p(b)
    for (i in 0 until maxOf(x.size, y.size)) {
        val xi = x.getOrElse(i) { 0 }
        val yi = y.getOrElse(i) { 0 }
        if (xi != yi) return xi > yi
    }
    return false
}

fun lines(s: String): List<String> = s.split('\n', ',').map { it.trim() }.filter { it.isNotEmpty() }

fun ipKey(ip: String): Long = (if (ip.isBlank()) "999.999.999.999" else ip).split('.').fold(0L) { a, o -> a * 256 + (o.toLongOrNull() ?: 0) }

private fun hourNow(): Int = js("new Date().getHours()")

fun greeting(): String {
    val h = hourNow()
    return when {
        h < 5 -> "Доброй ночи"
        h < 12 -> "Доброе утро"
        h < 18 -> "Добрый день"
        else -> "Добрый вечер"
    }
}

fun clockTime(unix: Long): String {
    val d = unix.toDouble() * 1000
    return timeOf(d)
}

private fun timeOf(ms: Double): String = js("new Date(ms).toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'})")

fun dateTime(unix: Long): String = dateOf(unix.toDouble() * 1000)
private fun dateOf(ms: Double): String = js("new Date(ms).toLocaleString('ru-RU', {day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit'})")
