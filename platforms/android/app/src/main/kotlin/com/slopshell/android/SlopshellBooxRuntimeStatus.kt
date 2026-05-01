package com.slopshell.android

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

data class SlopshellBooxDetectionSignals(
    val manufacturer: String,
    val onyxSdkPackagePresent: Boolean,
    val touchHelperClassPresent: Boolean,
    val epdControllerClassPresent: Boolean,
) {
    val detectedAsBoox: Boolean
        get() = shouldTreatAsBooxDevice(
            manufacturer = manufacturer,
            hasOnyxSdkPackage = onyxSdkPackagePresent,
            hasTouchHelperClass = touchHelperClassPresent,
        )
}

data class SlopshellBooxRuntimeMetrics(
    val rawDrawingActive: Boolean = false,
    val inkStrokeCount: Long = 0L,
    val lastInkStrokeAtMs: Long = 0L,
    val einkRefreshAttemptCount: Long = 0L,
    val einkRefreshSuccessCount: Long = 0L,
    val lastEinkRefreshAtMs: Long = 0L,
)

data class SlopshellBooxRuntimeStatus(
    val detectionSignals: SlopshellBooxDetectionSignals,
    val metrics: SlopshellBooxRuntimeMetrics,
)

object SlopshellBooxRuntimeProbe {
    @Volatile
    private var clock: () -> Long = { System.currentTimeMillis() }

    private val _metrics = MutableStateFlow(SlopshellBooxRuntimeMetrics())
    val metrics: StateFlow<SlopshellBooxRuntimeMetrics> = _metrics.asStateFlow()

    fun setRawDrawingActive(active: Boolean) {
        _metrics.update { current ->
            if (current.rawDrawingActive == active) current else current.copy(rawDrawingActive = active)
        }
    }

    fun recordInkStroke() {
        val now = clock()
        _metrics.update { current ->
            current.copy(
                inkStrokeCount = current.inkStrokeCount + 1,
                lastInkStrokeAtMs = now,
            )
        }
    }

    fun recordEinkRefresh(success: Boolean) {
        val now = clock()
        _metrics.update { current ->
            current.copy(
                einkRefreshAttemptCount = current.einkRefreshAttemptCount + 1,
                einkRefreshSuccessCount = current.einkRefreshSuccessCount + if (success) 1L else 0L,
                lastEinkRefreshAtMs = if (success) now else current.lastEinkRefreshAtMs,
            )
        }
    }

    fun reset() {
        _metrics.value = SlopshellBooxRuntimeMetrics()
    }

    internal fun setClockForTesting(clock: () -> Long) {
        this.clock = clock
    }

    internal fun resetClockForTesting() {
        this.clock = { System.currentTimeMillis() }
    }
}
