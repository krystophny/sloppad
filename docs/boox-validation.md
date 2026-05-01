# Boox Validation

This document is the closure standard for the Onyx Boox path inside the Android
client. It is intentionally separate from
[`native-clients.md`](native-clients.md): generic Android validation does not
prove Boox readiness, and Boox claims must rest on Boox-specific evidence.

## Boox SDK and Tooling Reality

The Onyx SDK requirements are pinned in
[`platforms/android/app/build.gradle.kts`](../platforms/android/app/build.gradle.kts):

- `com.onyx.android.sdk:onyxsdk-device:1.1.11`
- `com.onyx.android.sdk:onyxsdk-pen:1.2.1`

The artifacts are resolved from Onyx's Maven repository, declared in
[`platforms/android/settings.gradle.kts`](../platforms/android/settings.gradle.kts):

```text
https://repo.boox.com/repository/maven-public/
```

The Onyx SDK's `TouchHelper`, `RawInputCallback`, and `EpdController`
classes are the entry points used by the Boox path:

- raw stylus drawing: `com.onyx.android.sdk.pen.TouchHelper.create`
- e-ink update modes: `com.onyx.android.sdk.api.device.epd.EpdController$UpdateMode`
- WebView contrast: `EpdController.setWebViewContrastOptimize`

There is no official Boox emulator and no vendor-supplied virtual device for
these APIs. Generic Android emulators do not implement the `EpdController`
class or the `TouchHelper` raw drawing surface. Closure of the Boox path
therefore requires current Boox hardware. Treat any claim of Boox readiness
that rests only on emulator or generic Android evidence as unsubstantiated.

## Runtime Observability

When the Onyx-specific path is active, the Android client surfaces a Boox
status block under the connect controls instead of the previous one-line
indicator. The block reports:

- the four detection signals (manufacturer, Onyx SDK package, `TouchHelper`
  class, `EpdController` class)
- whether raw drawing is active right now
- the cumulative ink-stroke count emitted from the raw drawing surface
- the cumulative e-ink refresh attempt and success counts

The dynamic counters are owned by `SlopshellBooxRuntimeProbe` in
[`platforms/android/app/src/main/kotlin/com/slopshell/android/SlopshellBooxRuntimeStatus.kt`](../platforms/android/app/src/main/kotlin/com/slopshell/android/SlopshellBooxRuntimeStatus.kt).
The probe is updated from:

- `SlopshellBooxInkSurfaceView.ensureTouchHelper` and `shutdownTouchHelper`
  (raw drawing active flag)
- `SlopshellBooxInkSurfaceView.emitStroke` (ink stroke counter)
- `SlopshellBooxEinkController.refreshContentView` (refresh attempts and
  successes)

The detection signals come from `detectSlopshellBooxDetectionSignals` in
[`SlopshellBooxDevice.kt`](../platforms/android/app/src/main/kotlin/com/slopshell/android/SlopshellBooxDevice.kt).

## Off-Device Automated Checks

These run on a JVM with no Onyx hardware and no Android emulator:

```bash
gradle -p platforms/android app:testDebugUnitTest
```

The JVM unit suite covers:

- `SlopshellBooxRuntimeStatusTest` for detection-signal interpretation, the
  raw-drawing flag, ink-stroke counter, refresh attempt/success counters, and
  reset behavior.
- `SlopshellModelContractTest.booxDetectionAcceptsManufacturerOrSdkSignals` for
  the `shouldTreatAsBooxDevice` rule.
- `SlopshellInkStrokeBuilderTest` for the shared stroke normalization that the
  Boox raw path also uses.

The structural Go test
[`platforms/android/project_files_test.go`](../platforms/android/project_files_test.go)
also asserts that the Boox source files exist and contain the required Onyx
SDK call sites (`TouchHelper.create`, `setRawInputReaderEnable(true)`,
`openRawDrawing`, `closeRawDrawing`, `applyGCOnce`,
`setWebViewContrastOptimize`).

What these checks cannot prove:

- raw stylus drawing actually emits `ink_stroke` payloads on Onyx hardware
- the e-ink controller reduces ghosting on a real e-ink panel
- the WebView contrast hook produces a readable canvas on a Boox screen

Those require hardware.

## Hardware Validation Script

```bash
./scripts/test-boox-hardware.sh
```

The script orchestrates the parts that can be automated against an attached
Boox device:

1. Verifies `adb` is available and exactly one USB device is attached.
2. Reads `ro.product.manufacturer`, `ro.product.model`, and Android SDK level
   from the device and aborts unless the manufacturer is `onyx` (case
   insensitive).
3. Builds and installs the debug APK with `gradle :app:installDebug`.
4. Launches `MainActivity`.
5. Captures a screenshot of the running app to `artifacts/boox-validation/`
   for evidence.
6. Prints the manual checklist below and waits for the operator to record
   pass/fail.

The script is the only sanctioned path for closing a Boox-readiness PR. Run
the off-device checks first; the script is the hardware leg, not a
replacement for the unit tests.

## Manual Hardware Checklist

Attach current results from this checklist when claiming Boox readiness.
Treat any missing item as a fail.

1. Detection signals on the Boox status panel:
   - manufacturer matches the device (e.g. `onyx`/`Onyx`)
   - `sdk=true`
   - `TouchHelper=true`
   - `EpdController=true`
2. Raw stylus drawing on the canvas:
   - status panel reports `Raw drawing: active`
   - drawing a stroke increments the strokes counter
   - the same stroke arrives at the server as an `ink_stroke` payload (verify
     in the server log or the active chat session)
3. E-ink content rendering:
   - canvas content uses the e-ink CSS (white background, black text, no
     animations or shadows)
   - the status panel reports `E-ink refresh: M/N applied` with `M >= 1` after
     the canvas first renders
4. E-ink refresh follow-up:
   - a second canvas update increments both `M` and `N`
   - no persistent ghosting remains after the refresh runs
5. WebView contrast:
   - text remains readable when the canvas contains gradients or low-contrast
     backgrounds (e.g. plot artifacts)
6. Dialogue surfaces still work on Boox:
   - black-screen dialogue mode is still entered/exited normally
   - the foreground microphone service starts and stops in sync with the
     dialogue surface

## Closure Evidence

A PR or issue may not claim Boox readiness without:

- the off-device unit suite passing on the change
- the hardware script's installer step succeeding against a current Boox
  device
- a checklist run against the same device, attached either in the PR body or
  linked from it
- the Boox device's `ro.product.model` and Android SDK level recorded in the
  evidence

## Documentation Honesty

Boox completion claims are kept separate from generic Android claims:

- [`native-clients-plan.md`](native-clients-plan.md) and
  [`native-clients.md`](native-clients.md) describe the Android slice and
  point at this document for Boox closure evidence.
- This document owns the Boox SDK reality, runtime observability,
  off-device coverage, and hardware checklist.
- Product docs do not describe Boox as complete unless the closure evidence
  above is attached to the matching PR or issue.
