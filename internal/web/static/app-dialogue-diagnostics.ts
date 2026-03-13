import { refs, state } from './app-context.js';
import { isTurnIntelligenceConnected, sendTurnConfig } from './turn-client.js';

const DIALOGUE_DIAGNOSTIC_EVENT_LIMIT = 120;

function cloneDialogueDiagnostics() {
  const diagnostics: any = state.dialogueDiagnostics || {};
  return {
    ...diagnostics,
    recentEvents: Array.isArray(diagnostics.recentEvents) ? diagnostics.recentEvents.slice() : [],
    lastAction: diagnostics.lastAction ? { ...diagnostics.lastAction } : null,
    lastMetrics: diagnostics.lastMetrics ? JSON.parse(JSON.stringify(diagnostics.lastMetrics)) : null,
  };
}

export function pushDialogueDiagnosticEvent(kind, payload: Record<string, any> = {}) {
  if (!state.dialogueDiagnostics) {
    state.dialogueDiagnostics = {
      connected: false,
      sessionId: '',
      profile: state.turnPolicyProfile || 'balanced',
      evalLoggingEnabled: state.turnEvalLoggingEnabled !== false,
      readyAt: 0,
      lastAction: null,
      lastMetrics: null,
      recentEvents: [],
    };
  }
  const entry = {
    ts: Date.now(),
    kind: String(kind || '').trim() || 'event',
    ...payload,
  };
  const events = Array.isArray(state.dialogueDiagnostics.recentEvents)
    ? state.dialogueDiagnostics.recentEvents
    : [];
  events.push(entry);
  while (events.length > DIALOGUE_DIAGNOSTIC_EVENT_LIMIT) {
    events.shift();
  }
  state.dialogueDiagnostics.recentEvents = events;
}

export function recordDialogueVoiceDiagnostic(kind, payload: Record<string, any> = {}) {
  if (!state.liveSessionActive || String(state.liveSessionMode || '').trim().toLowerCase() !== 'dialogue') {
    return;
  }
  pushDialogueDiagnosticEvent(kind, payload);
}

export function recordDialogueSTTStart(triggerSource, mimeType, usedVADBlob) {
  recordDialogueVoiceDiagnostic('stt_start', {
    trigger_source: String(triggerSource || '').trim(),
    mime_type: String(mimeType || '').trim(),
    used_vad_blob: Boolean(usedVADBlob),
  });
}

export function recordDialogueSTTEmpty(triggerSource, reason) {
  recordDialogueVoiceDiagnostic('stt_empty', {
    trigger_source: String(triggerSource || '').trim(),
    reason: String(reason || '').trim(),
  });
}

export function recordDialogueSTTResult(triggerSource, chars, durationMs, interruptedAssistant) {
  recordDialogueVoiceDiagnostic('stt_result', {
    trigger_source: String(triggerSource || '').trim(),
    chars: Math.max(0, Number(chars) || 0),
    duration_ms: Math.max(0, Number(durationMs) || 0),
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function recordDialogueTranscriptSegment(chars, durationMs, interruptedAssistant, via) {
  recordDialogueVoiceDiagnostic('turn_transcript_segment_sent', {
    chars: Math.max(0, Number(chars) || 0),
    duration_ms: Math.max(0, Number(durationMs) || 0),
    interrupted_assistant: Boolean(interruptedAssistant),
    via: String(via || '').trim(),
  });
}

export function recordDialogueVoiceError(triggerSource, message) {
  recordDialogueVoiceDiagnostic('voice_capture_error', {
    trigger_source: String(triggerSource || '').trim(),
    message: String(message || '').trim(),
  });
}

export function getDialogueDiagnostics() {
  return cloneDialogueDiagnostics();
}

export function clearDialogueDiagnostics() {
  state.dialogueDiagnostics = {
    connected: isTurnIntelligenceConnected(),
    sessionId: state.chatSessionId || '',
    profile: state.turnPolicyProfile || 'balanced',
    evalLoggingEnabled: state.turnEvalLoggingEnabled !== false,
    readyAt: 0,
    lastAction: null,
    lastMetrics: null,
    recentEvents: [],
  };
}

export async function setDialogueTurnProfile(profile) {
  const nextProfile = String(profile || '').trim().toLowerCase() || 'balanced';
  const payload = await refs.updateRuntimePreferences({
    turn_policy_profile: nextProfile,
  });
  state.turnPolicyProfile = String(payload?.turn_policy_profile || nextProfile).trim().toLowerCase() || 'balanced';
  if (state.dialogueDiagnostics) {
    state.dialogueDiagnostics.profile = state.turnPolicyProfile;
  }
  sendTurnConfig(state.turnPolicyProfile, state.turnEvalLoggingEnabled !== false);
  pushDialogueDiagnosticEvent('profile_updated', { profile: state.turnPolicyProfile });
  return state.turnPolicyProfile;
}

export async function setDialogueEvalLogging(enabled) {
  const nextEnabled = Boolean(enabled);
  const payload = await refs.updateRuntimePreferences({
    turn_eval_logging_enabled: nextEnabled,
  });
  state.turnEvalLoggingEnabled = payload?.turn_eval_logging_enabled !== false;
  if (state.dialogueDiagnostics) {
    state.dialogueDiagnostics.evalLoggingEnabled = state.turnEvalLoggingEnabled;
  }
  sendTurnConfig(state.turnPolicyProfile, state.turnEvalLoggingEnabled);
  pushDialogueDiagnosticEvent('eval_logging_updated', { enabled: state.turnEvalLoggingEnabled });
  return state.turnEvalLoggingEnabled;
}

clearDialogueDiagnostics();
