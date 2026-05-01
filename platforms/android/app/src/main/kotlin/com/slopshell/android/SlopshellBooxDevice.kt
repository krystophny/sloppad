package com.slopshell.android

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.view.View
import android.webkit.WebView

data class SlopshellDisplayProfile(
    val signals: SlopshellBooxDetectionSignals,
) {
    val isBoox: Boolean
        get() = signals.detectedAsBoox
}

fun detectSlopshellDisplayProfile(context: Context): SlopshellDisplayProfile {
    return SlopshellDisplayProfile(signals = detectSlopshellBooxDetectionSignals(context))
}

fun detectSlopshellBooxDetectionSignals(context: Context): SlopshellBooxDetectionSignals {
    val isOnyxManufacturer = Build.MANUFACTURER.lowercase() == "onyx"
    return SlopshellBooxDetectionSignals(
        manufacturer = Build.MANUFACTURER,
        onyxSdkPackagePresent = hasOnyxSdkPackage(context) || isOnyxManufacturer,
        touchHelperClassPresent = hasClass("com.onyx.android.sdk.pen.TouchHelper"),
        epdControllerClassPresent = hasClass("com.onyx.android.sdk.api.device.epd.EpdController"),
    )
}

internal fun shouldTreatAsBooxDevice(
    manufacturer: String,
    hasOnyxSdkPackage: Boolean,
    hasTouchHelperClass: Boolean,
): Boolean {
    val normalizedManufacturer = manufacturer.trim().lowercase()
    return normalizedManufacturer == "onyx" ||
        hasOnyxSdkPackage ||
        hasTouchHelperClass
}

private fun hasOnyxSdkPackage(context: Context): Boolean {
    val packageManager = context.packageManager
    return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        runCatching {
            packageManager.getPackageInfo(
                "com.onyx.android.sdk",
                PackageManager.PackageInfoFlags.of(0),
            )
            true
        }.getOrDefault(false)
    } else {
        @Suppress("DEPRECATION")
        runCatching {
            packageManager.getPackageInfo("com.onyx.android.sdk", 0)
            true
        }.getOrDefault(false)
    }
}

private fun hasClass(name: String): Boolean {
    return runCatching {
        Class.forName(name)
        true
    }.getOrDefault(false)
}

object SlopshellBooxEinkController {
    fun configureInkView(view: View) {
        setViewDefaultUpdateMode(view, "DU")
    }

    fun configureContentView(view: View) {
        setViewDefaultUpdateMode(view, "GC16", "GC")
    }

    fun refreshContentView(view: View) {
        val applied = invokeController("applyGCOnce", arrayOf(View::class.java), arrayOf(view)) ||
            invokeController("applyGCOnce", emptyArray<Class<*>>(), emptyArray<Any>()) ||
            invalidateView(view, "GC16", "GC")
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = applied)
    }

    fun setWebViewContrastOptimize(view: WebView, enabled: Boolean) {
        invokeController(
            "setWebViewContrastOptimize",
            arrayOf(WebView::class.java, Boolean::class.javaPrimitiveType ?: Boolean::class.java),
            arrayOf(view, enabled),
        )
    }

    private fun setViewDefaultUpdateMode(view: View, vararg modeNames: String) {
        val mode = resolveUpdateMode(*modeNames) ?: return
        invokeController(
            "setViewDefaultUpdateMode",
            arrayOf(View::class.java, mode.javaClass),
            arrayOf(view, mode),
        )
    }

    private fun invalidateView(view: View, vararg modeNames: String): Boolean {
        val mode = resolveUpdateMode(*modeNames) ?: return false
        return invokeController(
            "invalidate",
            arrayOf(View::class.java, mode.javaClass),
            arrayOf(view, mode),
        )
    }

    private fun resolveUpdateMode(vararg modeNames: String): Any? {
        val enumClass = runCatching {
            Class.forName("com.onyx.android.sdk.api.device.epd.EpdController\$UpdateMode")
        }.getOrNull() ?: return null
        val constants = enumClass.enumConstants ?: return null
        for (modeName in modeNames) {
            constants.firstOrNull { (it as Enum<*>).name == modeName }?.let { return it }
        }
        return null
    }

    private fun invokeController(
        methodName: String,
        parameterTypes: Array<Class<*>>,
        args: Array<Any>,
    ): Boolean {
        val controllerClass = runCatching {
            Class.forName("com.onyx.android.sdk.api.device.epd.EpdController")
        }.getOrNull() ?: return false
        return runCatching {
            controllerClass.getMethod(methodName, *parameterTypes).invoke(null, *args)
            true
        }.getOrDefault(false)
    }
}
