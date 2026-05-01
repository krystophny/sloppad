package com.slopshell.android

import android.content.ComponentName
import android.content.Intent
import android.content.ServiceConnection
import android.os.Bundle
import android.os.IBinder
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat

private const val extraFlowHarnessEnabled = "slopshell.flow_harness"
private const val extraFlowPreconditionsJSON = "slopshell.flow_preconditions_json"

class MainActivity : ComponentActivity(), SlopshellAudioCaptureService.Listener {
    private val model by viewModels<SlopshellAppModel>()

    private var audioService: SlopshellAudioCaptureService? = null
    private var audioBound = false

    private val audioConnection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, service: IBinder) {
            val binder = service as? SlopshellAudioCaptureService.LocalBinder ?: return
            audioService = binder.service().also {
                it.setListener(this@MainActivity)
                model.updateRecordingState(it.isRunning())
            }
            audioBound = true
        }

        override fun onServiceDisconnected(name: ComponentName) {
            audioService?.setListener(null)
            audioService = null
            audioBound = false
            model.updateRecordingState(false)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        bindAudioService()
        val flowHarnessEnabled = intent?.getBooleanExtra(extraFlowHarnessEnabled, false) == true
        val flowHarnessPreconditions = parseSlopshellFlowHarnessPreconditions(
            intent?.getStringExtra(extraFlowPreconditionsJSON),
        )
        setContent {
            MaterialTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    if (flowHarnessEnabled) {
                        SlopshellFlowHarnessScreen(preconditions = flowHarnessPreconditions)
                        return@Surface
                    }
                    val state by model.state.collectAsState()
                    SideEffect {
                        if (state.dialoguePresentation.keepScreenAwake) {
                            window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                        } else {
                            window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                        }
                    }
                    SlopshellAndroidApp(
                        state = state,
                        onServerUrlChanged = model::updateServerUrl,
                        onPasswordChanged = model::updatePassword,
                        onUseDiscoveredServer = model::useDiscoveredServer,
                        onConnect = model::connect,
                        onSwitchWorkspace = model::switchWorkspace,
                        onComposerChanged = model::updateComposerText,
                        onSendComposer = model::sendComposerMessage,
                        onToggleRecording = ::toggleRecording,
                        onToggleDialogue = model::toggleDialogueMode,
                        onSetDialogueIdleSurface = model::setDialogueIdleSurface,
                        onInkCommit = model::submitInk,
                        onInkRequestsResponseChanged = model::setInkRequestsResponse,
                    )
                }
            }
        }
    }

    override fun onStart() {
        super.onStart()
        bindAudioService()
    }

    override fun onStop() {
        audioService?.setListener(null)
        if (audioBound) {
            unbindService(audioConnection)
            audioBound = false
        }
        super.onStop()
    }

    override fun onAudioChunk(data: ByteArray) {
        model.sendAudioChunk(data)
    }

    override fun onRecordingStateChanged(active: Boolean) {
        model.updateRecordingState(active)
        if (!active) {
            model.stopAudio()
        }
    }

    override fun onAudioError(message: String) {
        model.updateRecordingState(audioService?.isRunning() == true, message)
    }

    private fun bindAudioService() {
        if (audioBound) {
            return
        }
        bindService(
            Intent(this, SlopshellAudioCaptureService::class.java),
            audioConnection,
            BIND_AUTO_CREATE,
        )
    }

    private fun toggleRecording() {
        val service = audioService ?: return
        if (service.isRunning()) {
            service.stopStreaming()
            model.stopAudio()
            return
        }
        ContextCompat.startForegroundService(this, Intent(this, SlopshellAudioCaptureService::class.java))
        service.startStreaming()
    }
}

@Composable
private fun SlopshellAndroidApp(
    state: SlopshellAppModel.UiState,
    onServerUrlChanged: (String) -> Unit,
    onPasswordChanged: (String) -> Unit,
    onUseDiscoveredServer: (SlopshellDiscoveredServer) -> Unit,
    onConnect: () -> Unit,
    onSwitchWorkspace: (String) -> Unit,
    onComposerChanged: (String) -> Unit,
    onSendComposer: () -> Unit,
    onToggleRecording: () -> Unit,
    onToggleDialogue: () -> Unit,
    onSetDialogueIdleSurface: (SlopshellCompanionIdleSurface) -> Unit,
    onInkCommit: (List<SlopshellInkStroke>) -> Unit,
    onInkRequestsResponseChanged: (Boolean) -> Unit,
) {
    val context = LocalContext.current
    val displayProfile = remember(context) {
        detectSlopshellDisplayProfile(context)
    }
    val dialoguePresentation = state.dialoguePresentation

    if (dialoguePresentation.usesBlackScreen) {
        BlackScreenDialogueSurface(
            presentation = dialoguePresentation,
            errorText = state.lastError,
            onToggleRecording = onToggleRecording,
            onExitDialogue = onToggleDialogue,
        )
        return
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("Slopshell Android", style = MaterialTheme.typography.headlineMedium)

        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                modifier = Modifier.fillMaxWidth(),
                value = state.serverUrl,
                onValueChange = onServerUrlChanged,
                label = { Text("Server URL") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
            OutlinedTextField(
                modifier = Modifier.fillMaxWidth(),
                value = state.password,
                onValueChange = onPasswordChanged,
                label = { Text("Password") },
                singleLine = true,
            )
            if (state.discoveredServers.isNotEmpty()) {
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(state.discoveredServers, key = { it.id }) { server ->
                        Button(onClick = { onUseDiscoveredServer(server) }) {
                            Text(server.name)
                        }
                    }
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Button(onClick = onConnect) {
                    Text("Connect")
                }
                Text(state.statusText, style = MaterialTheme.typography.bodySmall)
            }
            if (displayProfile.isBoox) {
                BooxRuntimeStatusPanel(signals = displayProfile.signals)
            }
            if (state.lastError.isNotBlank()) {
                Text(state.lastError, color = MaterialTheme.colorScheme.error)
            }
        }

        if (state.workspaces.isNotEmpty()) {
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(state.workspaces, key = { it.id }) { workspace ->
                    val selected = workspace.id == state.selectedWorkspaceId
                    Button(onClick = { onSwitchWorkspace(workspace.id) }) {
                        Text(if (selected) "• ${workspace.name}" else workspace.name)
                    }
                }
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Switch(
                checked = state.inkRequestsResponse,
                onCheckedChange = onInkRequestsResponseChanged,
            )
            Text("Ink asks Slopshell")
        }

        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Dialogue Surface", style = MaterialTheme.typography.titleMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { onSetDialogueIdleSurface(SlopshellCompanionIdleSurface.ROBOT) }) {
                    Text(if (state.companionIdleSurface == SlopshellCompanionIdleSurface.ROBOT.wireValue) "• Robot" else "Robot")
                }
                Button(onClick = { onSetDialogueIdleSurface(SlopshellCompanionIdleSurface.BLACK) }) {
                    Text(if (state.companionIdleSurface == SlopshellCompanionIdleSurface.BLACK.wireValue) "• Black" else "Black")
                }
                Button(onClick = onToggleDialogue) {
                    Text(if (state.isDialogueModeActive) "Stop Dialogue" else "Start Dialogue")
                }
            }
            Text(dialoguePresentation.primaryLabel, style = MaterialTheme.typography.titleSmall)
            Text(
                dialoguePresentation.secondaryLabel,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(280.dp)
                .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(20.dp))
                .background(Color.White, RoundedCornerShape(20.dp))
                .padding(8.dp),
        ) {
            SlopshellCanvasWebView(
                html = state.canvas.html,
                baseUrl = state.serverUrl,
                isEinkDisplay = displayProfile.isBoox,
                modifier = Modifier.fillMaxSize(),
            )
            AndroidView(
                modifier = Modifier.fillMaxSize(),
                factory = { context ->
                    if (displayProfile.isBoox) {
                        SlopshellBooxInkSurfaceView(context).apply {
                            setOnCommit(onInkCommit)
                        }
                    } else {
                        SlopshellInkSurfaceView(context).apply {
                            setOnCommit(onInkCommit)
                        }
                    }
                },
                update = { view ->
                    when (view) {
                        is SlopshellBooxInkSurfaceView -> view.setOnCommit(onInkCommit)
                        is SlopshellInkSurfaceView -> view.setOnCommit(onInkCommit)
                    }
                },
            )
        }

        LazyColumn(
            modifier = Modifier
                .fillMaxWidth()
                .height(220.dp)
                .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(20.dp))
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            items(state.messages, key = { it.id }) { message ->
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(
                            if (message.role == "user") MaterialTheme.colorScheme.primary.copy(alpha = 0.08f)
                            else MaterialTheme.colorScheme.secondary.copy(alpha = 0.08f),
                            RoundedCornerShape(14.dp),
                        )
                        .padding(12.dp),
                ) {
                    Text(message.role.replaceFirstChar { it.uppercase() }, style = MaterialTheme.typography.labelSmall)
                    Spacer(modifier = Modifier.size(4.dp))
                    Text(message.text)
                }
            }
        }

        OutlinedTextField(
            modifier = Modifier
                .fillMaxWidth()
                .height(120.dp),
            value = state.composerText,
            onValueChange = onComposerChanged,
            label = { Text("Message") },
        )

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Button(onClick = onToggleRecording) {
                Text(if (state.isRecording) "Stop Mic" else "Record Mic")
            }
            Button(onClick = onSendComposer) {
                Text("Send")
            }
            Text(
                text = context.packageName,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun BooxRuntimeStatusPanel(signals: SlopshellBooxDetectionSignals) {
    val metrics by SlopshellBooxRuntimeProbe.metrics.collectAsState()
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text("Boox E-Ink mode active", style = MaterialTheme.typography.bodySmall)
        Text(
            "Detection: manufacturer=${signals.manufacturer}, " +
                "sdk=${signals.onyxSdkPackagePresent}, " +
                "TouchHelper=${signals.touchHelperClassPresent}, " +
                "EpdController=${signals.epdControllerClassPresent}",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            "Raw drawing: ${if (metrics.rawDrawingActive) "active" else "idle"} | strokes=${metrics.inkStrokeCount}",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            "E-ink refresh: ${metrics.einkRefreshSuccessCount}/${metrics.einkRefreshAttemptCount} applied",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun BlackScreenDialogueSurface(
    presentation: SlopshellDialogueModePresentation,
    errorText: String,
    onToggleRecording: () -> Unit,
    onExitDialogue: () -> Unit,
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color.Black),
        contentAlignment = Alignment.Center,
    ) {
        Button(
            modifier = Modifier.fillMaxSize(),
            onClick = onToggleRecording,
        ) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Text(presentation.primaryLabel, style = MaterialTheme.typography.headlineLarge)
                Text(presentation.secondaryLabel, style = MaterialTheme.typography.titleMedium)
                Text(presentation.tapActionLabel, style = MaterialTheme.typography.titleSmall)
                if (errorText.isNotBlank()) {
                    Text(errorText, color = MaterialTheme.colorScheme.error)
                }
            }
        }
        Button(
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .padding(24.dp),
            onClick = onExitDialogue,
        ) {
            Text("Exit Dialogue")
        }
    }
}
