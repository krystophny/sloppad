# Native Clients

This document is the release/run/verification guide for the shipped native thin-client slice.

The repo does not claim a broader finished mobile product than what is verified here. The current shipped scope is:

- iOS thin-client transport, ink capture, audio capture, and black dialogue surface wiring
- Android thin-client transport, ink capture, foreground audio capture, and black dialogue surface wiring

Boox-specific code paths exist in the Android client, but Boox hardware validation is tracked separately and is not part of the completion claim in this document.

Use [`native-clients-plan.md`](native-clients-plan.md) for the architecture decision and source-code anchors. Use this document for setup, run, verification, and documentation honesty.

## Setup and Run

1. Start a Slopshell server reachable from the device:

   ```bash
   TMP_ROOT="$(mktemp -d -t slopshell-native-XXXXXX)"
   WORKSPACE_DIR="$TMP_ROOT/workspace"
   DATA_DIR="$TMP_ROOT/data"
   go run ./cmd/slopshell server \
     --workspace-dir "$WORKSPACE_DIR" \
     --data-dir "$DATA_DIR" \
     --web-host 0.0.0.0 \
     --web-port 8420 \
     --mcp-host 127.0.0.1 \
     --mcp-port 9420
   ```

2. Fast native contract checks:

   ```bash
   npm run test:flows:ios:contract
   npm run test:flows:android:contract
   npm run test:flows:android:contract:jvm
   ```

   `npm run test:flows:android:contract:jvm` is the JVM-only subset that hosted
   CI uses; it does not require the Android SDK.

3. Full native validation:

   ```bash
   npm run test:flows:native
   ```

   This wraps [`./scripts/test-native-flows.sh`](../scripts/test-native-flows.sh).

   This command:

- refreshes the generated native flow fixtures
- runs the shared web Playwright flow suite
- runs the fast iOS and Android contract suites
- runs the Android UI harness on a local emulator
- syncs the repo to `faepmac1` and runs the iOS UI harness on a simulator there

   Environment knobs:

- `ANDROID_HOME` or `ANDROID_SDK_ROOT` must point at the local Android SDK.
- `SLOPSHELL_ANDROID_AVD` chooses the Android emulator. If unset, the first local AVD is used.
- `SLOPSHELL_IOS_SSH_HOST` defaults to `faepmac1`.
- `SLOPSHELL_IOS_REMOTE_ROOT` defaults to `~/slopshell-ci` on the macOS host.
- `SLOPSHELL_IOS_DESTINATION` overrides the `xcodebuild` simulator destination string.

4. Manual app runs:

   After the automated checks pass, build and run `SlopshellIOS` or the Android app on current hardware when a PR needs human validation beyond the scripted harness.

## Automated Verification

Use these checks before claiming the native slice is working:

1. Fast native contract suites:

   ```bash
   npm run test:flows:native:contract
   ```

   This covers dialogue presentation logic, transport URL helpers, payload encoding, Boox detection heuristics, and the shared flow contract.

   The underlying commands are:

   ```bash
   swift test --package-path platforms/ios
   ANDROID_HOME=/home/ert/android-sdk gradle -p platforms/android app:testDebugUnitTest
   gradle -p platforms/android/flow-contracts test
   ./scripts/playwright.sh
   ```

2. Release-validation path:

   ```bash
   npm run test:flows:native
   ```

   This is the required completion-evidence command for the iOS/Android thin-client slice.

3. Server/web dialogue companion wiring:

   `npm run test:flows:native` already includes the shared Playwright flow suite, so the web/native circle contract stays in one validation path.

4. Hosted CI coverage:

   `.github/workflows/test-reports.yml` runs three required jobs in parallel on
   every pull request and push to `main`:

   - `reports` (ubuntu-latest) runs the shared web flow suite via
     `npm run test:flows`.
   - `android-flow-contract` (ubuntu-latest) runs
     `npm run test:flows:android:contract:jvm`, executing the shared flows
     against the JVM/Kotlin `FlowRunner` so the Android adapter must stay green
     before merge.
   - `ios-flow-contract` (macos-latest) runs `npm run test:flows:ios:contract`,
     executing the shared flows against the Swift `FlowRunner` so the iOS
     adapter must stay green before merge.

   The simulator and emulator UI harnesses still run on the documented macOS
   host (`faepmac1`) and on a local Android emulator through
   `./scripts/test-native-flows.sh`; hosted CI cannot run those without
   dedicated mobile infrastructure.

The structural tests in `platforms/ios/project_files_test.go` and `platforms/android/project_files_test.go` are regression guards for packaging/layout. They are not completion evidence on their own.

## Manual Verification

Attach current hardware results to the PR or issue when platform hardware is involved.

1. iOS server discovery and transport

   Pass: `_slopshell._tcp` discovery finds the server or a manual URL connects, chat history loads, canvas snapshot loads, and live chat events continue after connect.

   Fail: discovery never resolves, connect succeeds without chat/canvas data, or websocket updates stop after the first turn.

2. iOS ink, audio, and dialogue surface

   Pass: ink strokes commit to the active chat session, `Black` idle surface enters the full-screen black panel, a tap starts then stops audio capture, and returning from background keeps the app responsive for the next turn.

   Fail: ink is only local, dialogue mode is only cosmetic, recording cannot be stopped cleanly, or background/foreground transition breaks the next capture cycle.

3. Android discovery, transport, ink, and foreground audio

   Pass: the Android client discovers or connects to the server, canvas/chat stay live, stylus or touch input produces `ink_stroke` messages, and the foreground microphone service starts and stops in sync with the dialogue surface.

   Fail: the client loads only static content, ink never reaches the server, or service state diverges from the UI recording state.

4. Boox raw drawing and e-ink refresh

   This is tracked separately. Do not use iOS/Android completion evidence as a proxy for Boox hardware readiness.

## Documentation Honesty

Do not describe the native clients as a broader completed product unless the automated checks above pass and the manual checklist above has current hardware results attached.

The current repo claim is limited to the iOS/Android thin-client slice documented here and in `native-clients-plan.md`. Boox-specific validation remains a separate track.
