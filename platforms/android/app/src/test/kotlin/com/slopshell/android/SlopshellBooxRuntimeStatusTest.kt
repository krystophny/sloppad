package com.slopshell.android

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class SlopshellBooxRuntimeStatusTest {
    @Before
    fun resetProbe() {
        SlopshellBooxRuntimeProbe.reset()
    }

    @After
    fun resetProbeClock() {
        SlopshellBooxRuntimeProbe.resetClockForTesting()
        SlopshellBooxRuntimeProbe.reset()
    }

    @Test
    fun detectionSignalsReportsBooxOnExplicitOnyxManufacturer() {
        val signals = SlopshellBooxDetectionSignals(
            manufacturer = "Onyx",
            onyxSdkPackagePresent = false,
            touchHelperClassPresent = false,
            epdControllerClassPresent = false,
        )
        assertTrue(signals.detectedAsBoox)
    }

    @Test
    fun detectionSignalsReportsBooxOnSdkPackageOrTouchHelperOnly() {
        val viaSdk = SlopshellBooxDetectionSignals(
            manufacturer = "Acme",
            onyxSdkPackagePresent = true,
            touchHelperClassPresent = false,
            epdControllerClassPresent = false,
        )
        val viaTouchHelper = SlopshellBooxDetectionSignals(
            manufacturer = "Acme",
            onyxSdkPackagePresent = false,
            touchHelperClassPresent = true,
            epdControllerClassPresent = false,
        )
        assertTrue(viaSdk.detectedAsBoox)
        assertTrue(viaTouchHelper.detectedAsBoox)
    }

    @Test
    fun detectionSignalsRejectsNonOnyxWithoutSignals() {
        val signals = SlopshellBooxDetectionSignals(
            manufacturer = "Acme",
            onyxSdkPackagePresent = false,
            touchHelperClassPresent = false,
            epdControllerClassPresent = false,
        )
        assertFalse(signals.detectedAsBoox)
    }

    @Test
    fun probeStartsIdleWithZeroCounters() {
        val metrics = SlopshellBooxRuntimeProbe.metrics.value
        assertFalse(metrics.rawDrawingActive)
        assertEquals(0L, metrics.inkStrokeCount)
        assertEquals(0L, metrics.einkRefreshAttemptCount)
        assertEquals(0L, metrics.einkRefreshSuccessCount)
        assertEquals(0L, metrics.lastInkStrokeAtMs)
        assertEquals(0L, metrics.lastEinkRefreshAtMs)
    }

    @Test
    fun rawDrawingActiveFlagTracksLastSetValue() {
        SlopshellBooxRuntimeProbe.setRawDrawingActive(true)
        assertTrue(SlopshellBooxRuntimeProbe.metrics.value.rawDrawingActive)
        SlopshellBooxRuntimeProbe.setRawDrawingActive(false)
        assertFalse(SlopshellBooxRuntimeProbe.metrics.value.rawDrawingActive)
    }

    @Test
    fun recordInkStrokeIncrementsCounterAndStampsClock() {
        SlopshellBooxRuntimeProbe.setClockForTesting { 1_700_000_000_001L }
        SlopshellBooxRuntimeProbe.recordInkStroke()
        SlopshellBooxRuntimeProbe.recordInkStroke()
        val metrics = SlopshellBooxRuntimeProbe.metrics.value
        assertEquals(2L, metrics.inkStrokeCount)
        assertEquals(1_700_000_000_001L, metrics.lastInkStrokeAtMs)
    }

    @Test
    fun recordEinkRefreshSeparatesAttemptsFromSuccesses() {
        SlopshellBooxRuntimeProbe.setClockForTesting { 1_700_000_000_010L }
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = false)
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = true)
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = true)
        val metrics = SlopshellBooxRuntimeProbe.metrics.value
        assertEquals(3L, metrics.einkRefreshAttemptCount)
        assertEquals(2L, metrics.einkRefreshSuccessCount)
        assertEquals(1_700_000_000_010L, metrics.lastEinkRefreshAtMs)
    }

    @Test
    fun failedRefreshDoesNotAdvanceLastSuccessTimestamp() {
        SlopshellBooxRuntimeProbe.setClockForTesting { 100L }
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = true)
        SlopshellBooxRuntimeProbe.setClockForTesting { 200L }
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = false)
        val metrics = SlopshellBooxRuntimeProbe.metrics.value
        assertEquals(2L, metrics.einkRefreshAttemptCount)
        assertEquals(1L, metrics.einkRefreshSuccessCount)
        assertEquals(100L, metrics.lastEinkRefreshAtMs)
    }

    @Test
    fun resetClearsCountersAndRawDrawingFlag() {
        SlopshellBooxRuntimeProbe.setClockForTesting { 42L }
        SlopshellBooxRuntimeProbe.setRawDrawingActive(true)
        SlopshellBooxRuntimeProbe.recordInkStroke()
        SlopshellBooxRuntimeProbe.recordEinkRefresh(success = true)

        SlopshellBooxRuntimeProbe.reset()

        val metrics = SlopshellBooxRuntimeProbe.metrics.value
        assertFalse(metrics.rawDrawingActive)
        assertEquals(0L, metrics.inkStrokeCount)
        assertEquals(0L, metrics.einkRefreshAttemptCount)
        assertEquals(0L, metrics.einkRefreshSuccessCount)
        assertEquals(0L, metrics.lastInkStrokeAtMs)
        assertEquals(0L, metrics.lastEinkRefreshAtMs)
    }
}
