package netpilot

// Тема Material 3 Expressive: цветовые роли трёх палитр, шрифт Roboto Flex,
// схема движения expressive (пружины) и формы.

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import netpilot.res.Res
import netpilot.res.roboto_flex
import netpilot.res.roboto_mono
import org.jetbrains.compose.resources.Font

/** Цвета состояний, гармонизированные с палитрой: «работает», «внимание». */
data class ExtraColors(val ok: Color, val okContainer: Color, val onOkContainer: Color, val warn: Color, val warnContainer: Color, val onWarnContainer: Color)

val LocalExtra = staticCompositionLocalOf { ExtraColors(Color.Green, Color.Green, Color.Black, Color.Yellow, Color.Yellow, Color.Black) }
val LocalMono = staticCompositionLocalOf<FontFamily> { FontFamily.Monospace }

private fun c(hex: Long) = Color(hex or 0xFF000000)

val SCHEMES = listOf("indigo" to "Индиго", "green" to "Изумруд", "coral" to "Коралл")
val SCHEME_SEED = mapOf("indigo" to c(0x4355B9), "green" to c(0x3B6939), "coral" to c(0x8F4C38))

private fun indigo(dark: Boolean) = if (!dark) lightColorScheme(
    primary = c(0x4355B9), onPrimary = c(0xFFFFFF), primaryContainer = c(0xDEE0FF), onPrimaryContainer = c(0x00105C),
    secondary = c(0x5B5D72), onSecondary = c(0xFFFFFF), secondaryContainer = c(0xE0E1F9), onSecondaryContainer = c(0x181A2C),
    tertiary = c(0x77536D), onTertiary = c(0xFFFFFF), tertiaryContainer = c(0xFFD7F1), onTertiaryContainer = c(0x2D1228),
    error = c(0xBA1A1A), onError = c(0xFFFFFF), errorContainer = c(0xFFDAD6), onErrorContainer = c(0x410002),
    background = c(0xFBF8FF), onBackground = c(0x1B1B21), surface = c(0xFBF8FF), onSurface = c(0x1B1B21),
    surfaceVariant = c(0xE3E1EC), onSurfaceVariant = c(0x46464F), outline = c(0x767680), outlineVariant = c(0xC7C5D0),
    inverseSurface = c(0x303036), inverseOnSurface = c(0xF2EFF7), inversePrimary = c(0xBAC3FF), surfaceTint = c(0x4355B9),
    surfaceBright = c(0xFBF8FF), surfaceDim = c(0xDBD9E0), surfaceContainerLowest = c(0xFFFFFF), surfaceContainerLow = c(0xF5F2FA),
    surfaceContainer = c(0xEFEDF4), surfaceContainerHigh = c(0xEAE7EF), surfaceContainerHighest = c(0xE4E1E9), scrim = c(0x000000),
) else darkColorScheme(
    primary = c(0xBAC3FF), onPrimary = c(0x08218A), primaryContainer = c(0x293CA0), onPrimaryContainer = c(0xDEE0FF),
    secondary = c(0xC4C5DD), onSecondary = c(0x2D2F42), secondaryContainer = c(0x434659), onSecondaryContainer = c(0xE0E1F9),
    tertiary = c(0xE6BAD7), onTertiary = c(0x44263D), tertiaryContainer = c(0x5D3C55), onTertiaryContainer = c(0xFFD7F1),
    error = c(0xFFB4AB), onError = c(0x690005), errorContainer = c(0x93000A), onErrorContainer = c(0xFFDAD6),
    background = c(0x121318), onBackground = c(0xE4E1E9), surface = c(0x121318), onSurface = c(0xE4E1E9),
    surfaceVariant = c(0x46464F), onSurfaceVariant = c(0xC7C5D0), outline = c(0x90909A), outlineVariant = c(0x46464F),
    inverseSurface = c(0xE4E1E9), inverseOnSurface = c(0x303036), inversePrimary = c(0x4355B9), surfaceTint = c(0xBAC3FF),
    surfaceBright = c(0x39393F), surfaceDim = c(0x121318), surfaceContainerLowest = c(0x0D0E13), surfaceContainerLow = c(0x1B1B21),
    surfaceContainer = c(0x1F1F25), surfaceContainerHigh = c(0x2A292F), surfaceContainerHighest = c(0x34343A), scrim = c(0x000000),
)

private fun green(dark: Boolean) = if (!dark) lightColorScheme(
    primary = c(0x3B6939), onPrimary = c(0xFFFFFF), primaryContainer = c(0xBCF0B4), onPrimaryContainer = c(0x002204),
    secondary = c(0x52634F), onSecondary = c(0xFFFFFF), secondaryContainer = c(0xD5E8CF), onSecondaryContainer = c(0x101F0F),
    tertiary = c(0x38656A), onTertiary = c(0xFFFFFF), tertiaryContainer = c(0xBCEBF0), onTertiaryContainer = c(0x002023),
    error = c(0xBA1A1A), onError = c(0xFFFFFF), errorContainer = c(0xFFDAD6), onErrorContainer = c(0x410002),
    background = c(0xF7FBF1), onBackground = c(0x191D17), surface = c(0xF7FBF1), onSurface = c(0x191D17),
    surfaceVariant = c(0xDEE5D8), onSurfaceVariant = c(0x424940), outline = c(0x72796F), outlineVariant = c(0xC2C8BD),
    inverseSurface = c(0x2E322B), inverseOnSurface = c(0xEFF2E9), inversePrimary = c(0xA1D39A), surfaceTint = c(0x3B6939),
    surfaceBright = c(0xF7FBF1), surfaceDim = c(0xD8DBD2), surfaceContainerLowest = c(0xFFFFFF), surfaceContainerLow = c(0xF1F5EC),
    surfaceContainer = c(0xEBEFE6), surfaceContainerHigh = c(0xE6E9E0), surfaceContainerHighest = c(0xE0E4DB), scrim = c(0x000000),
) else darkColorScheme(
    primary = c(0xA1D39A), onPrimary = c(0x0A390F), primaryContainer = c(0x235024), onPrimaryContainer = c(0xBCF0B4),
    secondary = c(0xB9CCB4), onSecondary = c(0x253423), secondaryContainer = c(0x3A4B39), onSecondaryContainer = c(0xD5E8CF),
    tertiary = c(0xA0CFD4), onTertiary = c(0x00363B), tertiaryContainer = c(0x1F4D52), onTertiaryContainer = c(0xBCEBF0),
    error = c(0xFFB4AB), onError = c(0x690005), errorContainer = c(0x93000A), onErrorContainer = c(0xFFDAD6),
    background = c(0x10140F), onBackground = c(0xE0E4DB), surface = c(0x10140F), onSurface = c(0xE0E4DB),
    surfaceVariant = c(0x424940), onSurfaceVariant = c(0xC2C8BD), outline = c(0x8C9388), outlineVariant = c(0x424940),
    inverseSurface = c(0xE0E4DB), inverseOnSurface = c(0x2E322B), inversePrimary = c(0x3B6939), surfaceTint = c(0xA1D39A),
    surfaceBright = c(0x363A34), surfaceDim = c(0x10140F), surfaceContainerLowest = c(0x0B0F0A), surfaceContainerLow = c(0x191D17),
    surfaceContainer = c(0x1D211B), surfaceContainerHigh = c(0x272B25), surfaceContainerHighest = c(0x323630), scrim = c(0x000000),
)

private fun coral(dark: Boolean) = if (!dark) lightColorScheme(
    primary = c(0x8F4C38), onPrimary = c(0xFFFFFF), primaryContainer = c(0xFFDBD1), onPrimaryContainer = c(0x3A0B01),
    secondary = c(0x77574E), onSecondary = c(0xFFFFFF), secondaryContainer = c(0xFFDBD1), onSecondaryContainer = c(0x2C150F),
    tertiary = c(0x6C5D2F), onTertiary = c(0xFFFFFF), tertiaryContainer = c(0xF5E1A7), onTertiaryContainer = c(0x231B00),
    error = c(0xBA1A1A), onError = c(0xFFFFFF), errorContainer = c(0xFFDAD6), onErrorContainer = c(0x410002),
    background = c(0xFFF8F6), onBackground = c(0x231917), surface = c(0xFFF8F6), onSurface = c(0x231917),
    surfaceVariant = c(0xF5DED8), onSurfaceVariant = c(0x53433F), outline = c(0x85736E), outlineVariant = c(0xD8C2BC),
    inverseSurface = c(0x392E2B), inverseOnSurface = c(0xFFEDE8), inversePrimary = c(0xFFB5A0), surfaceTint = c(0x8F4C38),
    surfaceBright = c(0xFFF8F6), surfaceDim = c(0xE8D6D2), surfaceContainerLowest = c(0xFFFFFF), surfaceContainerLow = c(0xFFF1ED),
    surfaceContainer = c(0xFCEAE5), surfaceContainerHigh = c(0xF7E4E0), surfaceContainerHighest = c(0xF1DFDA), scrim = c(0x000000),
) else darkColorScheme(
    primary = c(0xFFB5A0), onPrimary = c(0x561F0F), primaryContainer = c(0x723523), onPrimaryContainer = c(0xFFDBD1),
    secondary = c(0xE7BDB2), onSecondary = c(0x442A22), secondaryContainer = c(0x5D4037), onSecondaryContainer = c(0xFFDBD1),
    tertiary = c(0xD8C58D), onTertiary = c(0x3B2F05), tertiaryContainer = c(0x534619), onTertiaryContainer = c(0xF5E1A7),
    error = c(0xFFB4AB), onError = c(0x690005), errorContainer = c(0x93000A), onErrorContainer = c(0xFFDAD6),
    background = c(0x1A110F), onBackground = c(0xF1DFDA), surface = c(0x1A110F), onSurface = c(0xF1DFDA),
    surfaceVariant = c(0x53433F), onSurfaceVariant = c(0xD8C2BC), outline = c(0xA08C87), outlineVariant = c(0x53433F),
    inverseSurface = c(0xF1DFDA), inverseOnSurface = c(0x392E2B), inversePrimary = c(0x8F4C38), surfaceTint = c(0xFFB5A0),
    surfaceBright = c(0x423734), surfaceDim = c(0x1A110F), surfaceContainerLowest = c(0x140C0A), surfaceContainerLow = c(0x231917),
    surfaceContainer = c(0x271D1B), surfaceContainerHigh = c(0x322825), surfaceContainerHighest = c(0x3D322F), scrim = c(0x000000),
)

private val extraLight = ExtraColors(c(0x2D6A3E), c(0xB2F2BB), c(0x00210B), c(0x7C5800), c(0xFFDEA6), c(0x271900))
private val extraDark = ExtraColors(c(0x96D5A0), c(0x12512A), c(0xB2F2BB), c(0xF6BE48), c(0x5E4200), c(0xFFDEA6))

fun colorSchemeFor(scheme: String, dark: Boolean): ColorScheme = when (scheme) {
    "green" -> green(dark)
    "coral" -> coral(dark)
    else -> indigo(dark)
}

@Composable
private fun flex(weight: Int) = Font(
    Res.font.roboto_flex, FontWeight(weight),
    variationSettings = FontVariation.Settings(FontVariation.weight(weight)),
)

@Composable
fun robotoFlex(): FontFamily = FontFamily(flex(300), flex(400), flex(500), flex(600), flex(700), flex(800))

@Composable
fun robotoMono(): FontFamily = FontFamily(
    Font(Res.font.roboto_mono, FontWeight.Normal, variationSettings = FontVariation.Settings(FontVariation.weight(400))),
    Font(Res.font.roboto_mono, FontWeight.Medium, variationSettings = FontVariation.Settings(FontVariation.weight(500))),
)

@Composable
private fun typography(ff: FontFamily): Typography {
    val b = Typography()
    fun TextStyle.f() = copy(fontFamily = ff)
    return Typography(
        displayLarge = b.displayLarge.f(), displayMedium = b.displayMedium.f(), displaySmall = b.displaySmall.f(),
        headlineLarge = b.headlineLarge.f(), headlineMedium = b.headlineMedium.f(), headlineSmall = b.headlineSmall.f(),
        titleLarge = b.titleLarge.f(), titleMedium = b.titleMedium.f(), titleSmall = b.titleSmall.f(),
        bodyLarge = b.bodyLarge.f(), bodyMedium = b.bodyMedium.f(), bodySmall = b.bodySmall.f(),
        labelLarge = b.labelLarge.f(), labelMedium = b.labelMedium.f(), labelSmall = b.labelSmall.f(),
        displayLargeEmphasized = b.displayLargeEmphasized.f(), displayMediumEmphasized = b.displayMediumEmphasized.f(),
        displaySmallEmphasized = b.displaySmallEmphasized.f(), headlineLargeEmphasized = b.headlineLargeEmphasized.f(),
        headlineMediumEmphasized = b.headlineMediumEmphasized.f(), headlineSmallEmphasized = b.headlineSmallEmphasized.f(),
        titleLargeEmphasized = b.titleLargeEmphasized.f(), titleMediumEmphasized = b.titleMediumEmphasized.f(),
        titleSmallEmphasized = b.titleSmallEmphasized.f(), bodyLargeEmphasized = b.bodyLargeEmphasized.f(),
        bodyMediumEmphasized = b.bodyMediumEmphasized.f(), bodySmallEmphasized = b.bodySmallEmphasized.f(),
        labelLargeEmphasized = b.labelLargeEmphasized.f(), labelMediumEmphasized = b.labelMediumEmphasized.f(),
        labelSmallEmphasized = b.labelSmallEmphasized.f(),
    )
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun NetPilotTheme(design: Design, content: @Composable () -> Unit) {
    val systemDark = isSystemInDarkTheme()
    val dark = when (design.mode) { "dark" -> true; "light" -> false; else -> systemDark }
    val scheme = colorSchemeFor(design.scheme, dark)
    val ff = robotoFlex()
    val mono = robotoMono()
    LaunchedEffect(scheme.surfaceContainer) {
        val argb = scheme.surfaceContainer.toArgb()
        setThemeColor("#" + (argb and 0xFFFFFF).toString(16).padStart(6, '0'))
    }
    CompositionLocalProvider(LocalExtra provides if (dark) extraDark else extraLight, LocalMono provides mono) {
        MaterialExpressiveTheme(
            colorScheme = scheme,
            motionScheme = MotionScheme.expressive(),
            typography = typography(ff),
            content = content,
        )
    }
}
