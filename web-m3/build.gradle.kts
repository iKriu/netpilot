import org.jetbrains.kotlin.gradle.ExperimentalWasmDsl

// Интерфейс NetPilot в оформлении Material 3 Expressive на Compose Multiplatform
// (Kotlin/Wasm): настоящие компоненты, формы и пружинная анимация Google.
// Сборка кладёт готовые файлы в web/static/m3 — их встраивает Go-программа.
plugins {
    kotlin("multiplatform") version "2.4.20"
    kotlin("plugin.compose") version "2.4.20"
    kotlin("plugin.serialization") version "2.4.20"
    id("org.jetbrains.compose") version "1.12.1"
}

val composeVersion = "1.12.1"

kotlin {
    @OptIn(ExperimentalWasmDsl::class)
    wasmJs {
        outputModuleName.set("netpilot")
        browser {
            commonWebpackConfig { outputFileName = "netpilot.js" }
        }
        binaries.executable()
    }
    sourceSets {
        all {
            languageSettings.optIn("androidx.compose.material3.ExperimentalMaterial3Api")
            languageSettings.optIn("androidx.compose.material3.ExperimentalMaterial3ExpressiveApi")
            languageSettings.optIn("androidx.compose.ui.ExperimentalComposeUiApi")
        }
        commonMain.dependencies {
            implementation("org.jetbrains.compose.runtime:runtime:$composeVersion")
            implementation("org.jetbrains.compose.foundation:foundation:$composeVersion")
            implementation("org.jetbrains.compose.ui:ui:$composeVersion")
            implementation("org.jetbrains.compose.material3:material3:1.12.0-alpha03")
            implementation("org.jetbrains.compose.material:material-icons-extended:1.7.3")
            implementation("org.jetbrains.compose.components:components-resources:$composeVersion")
            implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.11.0")
            implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.11.0")
        }
        wasmJsMain.dependencies {
            implementation("org.jetbrains.kotlinx:kotlinx-browser:0.5.0")
        }
    }
}

compose.resources {
    packageOfResClass = "netpilot.res"
    publicResClass = false
}

// Готовая сборка → web/static/m3 (встраивается в программу).
val publishM3 by tasks.registering(Sync::class) {
    dependsOn("wasmJsBrowserDistribution")
    from(layout.buildDirectory.dir("dist/wasmJs/productionExecutable"))
    exclude("**/*.map")
    into(rootDir.resolve("../web/static/m3"))
}

// Быстрая сборка для проверки (без оптимизаций).
val publishM3Dev by tasks.registering(Sync::class) {
    dependsOn("wasmJsBrowserDevelopmentExecutableDistribution")
    from(layout.buildDirectory.dir("dist/wasmJs/developmentExecutable"))
    into(rootDir.resolve("../web/static/m3"))
}
