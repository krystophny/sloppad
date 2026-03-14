import { wsURL } from './paths.js';

type TurnActionPayload = {
  type?: string;
  action?: string;
  text?: string;
  reason?: string;
  wait_ms?: number;
  interrupt_assistant?: boolean;
  rollback_audio_ms?: number;
};

type TurnMetricsPayload = {
  type?: string;
  metrics?: Record<string, any>;
};

type TurnReadyPayload = {
  type?: string;
  session_id?: string;
  profile?: string;
  eval_logging_enabled?: boolean;
  metrics?: Record<string, any>;
};

type TurnClientConfig = {
  onAction?: (payload: TurnActionPayload) => void;
  onMetrics?: (payload: TurnMetricsPayload) => void;
  onReady?: (payload: TurnReadyPayload) => void;
  profile?: string;
  evalLoggingEnabled?: boolean;
};

const state = {
  ws: null as WebSocket | null,
  token: 0,
  sessionId: '',
  connected: false,
  onAction: null as ((payload: TurnActionPayload) => void) | null,
  onMetrics: null as ((payload: TurnMetricsPayload) => void) | null,
  onReady: null as ((payload: TurnReadyPayload) => void) | null,
  profile: 'balanced',
  evalLoggingEnabled: true,
};

export function configureTurnIntelligence(config: TurnClientConfig = {}) {
  state.onAction = typeof config?.onAction === 'function' ? config.onAction : null;
  state.onMetrics = typeof config?.onMetrics === 'function' ? config.onMetrics : null;
  state.onReady = typeof config?.onReady === 'function' ? config.onReady : null;
  if (typeof config?.profile === 'string' && config.profile.trim()) {
    state.profile = config.profile.trim().toLowerCase();
  }
  if (typeof config?.evalLoggingEnabled === 'boolean') {
    state.evalLoggingEnabled = config.evalLoggingEnabled;
  }
  if (state.connected) {
    sendTurnConfig(state.profile, state.evalLoggingEnabled);
  }
}

export function openTurnWs(sessionId: string) {
  const targetSessionId = String(sessionId || '').trim();
  closeTurnWs();
  if (!targetSessionId) return;
  const token = state.token + 1;
  state.token = token;
  state.sessionId = targetSessionId;
  const ws = new WebSocket(wsURL(`turn/${encodeURIComponent(targetSessionId)}`));
  state.ws = ws;
  ws.onopen = () => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    state.connected = true;
    sendTurnConfig(state.profile, state.evalLoggingEnabled);
  };
  ws.onmessage = (event) => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    if (typeof event.data !== 'string') return;
    let payload: Record<string, any> | null = null;
    try { payload = JSON.parse(event.data); } catch (_) { return; }
    if (!payload || typeof payload.type !== 'string') return;
    if (payload.type === 'turn_ready') {
      if (typeof payload.profile === 'string' && payload.profile.trim()) {
        state.profile = payload.profile.trim().toLowerCase();
      }
      if (typeof payload.eval_logging_enabled === 'boolean') {
        state.evalLoggingEnabled = payload.eval_logging_enabled;
      }
      if (typeof state.onReady === 'function') {
        state.onReady(payload);
      }
      return;
    }
    if (payload.type === 'turn_metrics') {
      const metrics = payload.metrics as Record<string, any> | undefined;
      if (metrics && typeof metrics.profile === 'string' && metrics.profile.trim()) {
        state.profile = metrics.profile.trim().toLowerCase();
      }
      if (metrics && typeof metrics.eval_logging_enabled === 'boolean') {
        state.evalLoggingEnabled = metrics.eval_logging_enabled;
      }
      if (typeof state.onMetrics === 'function') {
        state.onMetrics(payload);
      }
      return;
    }
    if (payload.type !== 'turn_action') return;
    if (typeof state.onAction === 'function') {
      state.onAction(payload);
    }
  };
  ws.onclose = () => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    state.connected = false;
    state.ws = null;
  };
  ws.onerror = () => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    state.connected = false;
  };
}

export function closeTurnWs() {
  state.token += 1;
  state.connected = false;
  if (state.ws) {
    try { state.ws.close(); } catch (_) {}
  }
  state.ws = null;
  state.sessionId = '';
}

export function isTurnIntelligenceConnected() {
  return Boolean(state.connected && state.ws && state.ws.readyState === WebSocket.OPEN);
}

export function sendTurnEvent(payload: Record<string, any>) {
  const ws = state.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN) return false;
  ws.send(JSON.stringify(payload || {}));
  return true;
}

export function sendTurnConfig(profile = state.profile, evalLoggingEnabled = state.evalLoggingEnabled) {
  const normalizedProfile = String(profile || state.profile || 'balanced').trim().toLowerCase() || 'balanced';
  state.profile = normalizedProfile;
  state.evalLoggingEnabled = Boolean(evalLoggingEnabled);
  return sendTurnEvent({
    type: 'turn_config',
    profile: normalizedProfile,
    eval_logging_enabled: state.evalLoggingEnabled,
  });
}

export function sendTurnListenState(active: boolean) {
  return sendTurnEvent({ type: 'turn_listen_state', active: Boolean(active) });
}

export function sendTurnSpeechStart(interruptedAssistant = false) {
  return sendTurnEvent({
    type: 'turn_speech_start',
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function sendTurnSpeechProbability(speechProb: number, interruptedAssistant = false) {
  const prob = Number.isFinite(Number(speechProb)) ? Number(speechProb) : 0;
  return sendTurnEvent({
    type: 'turn_speech_prob',
    speech_prob: prob,
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function sendTurnTranscriptSegment(text: string, durationMs: number, interruptedAssistant = false) {
  return sendTurnEvent({
    type: 'turn_transcript_segment',
    text: String(text || ''),
    duration_ms: Math.max(0, Number(durationMs) || 0),
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function sendTurnPlaybackProgress(playing: boolean, playedMs: number) {
  return sendTurnEvent({
    type: 'turn_playback',
    playing: Boolean(playing),
    played_ms: Math.max(0, Number(playedMs) || 0),
  });
}

export function sendTurnClientDiagnostic(kind: string, payload: Record<string, any> = {}) {
  const normalizedKind = String(kind || '').trim();
  if (!normalizedKind) return false;
  return sendTurnEvent({
    type: 'turn_client_diag',
    kind: normalizedKind,
    payload,
  });
}

export function resetTurnIntelligence() {
  return sendTurnEvent({ type: 'turn_reset' });
}
