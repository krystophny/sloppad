/**
 * ParticipantCapture: browser mic capture for meeting transcription.
 * Streams speech segments to the server via WebSocket using Silero VAD.
 *
 * Privacy: no audio is persisted to localStorage or indexedDB.
 * All buffers are zeroized on stop/destroy. See docs/meeting-notes-privacy.md.
 */

import { initVAD, float32ToWav } from './vad.js';

export class ParticipantCapture {
  constructor(options = {}) {
    this._ws = null;
    this._stream = null;
    this._vadInstance = null;
    this._active = false;
    this._sessionId = null;
    this._onSegment = null;
    this._onStarted = null;
    this._onStopped = null;
    this._onError = null;
    this._sampleRate = 16000;
    this._maxSegmentDurationMS = normalizePositiveNumber(options.maxSegmentDurationMS, 30_000);
    this._sessionRamCapBytes = normalizeBytesCap(options.sessionRamCapMB, 64 * 1024 * 1024);
    this._rollingSamples = null;
    this._sessionChunks = [];
    this._sessionBufferedBytes = 0;
  }

  get active() {
    return this._active;
  }

  get sessionId() {
    return this._sessionId;
  }

  get pendingSegmentSamples() {
    return this._rollingSamples ? this._rollingSamples.length : 0;
  }

  get sessionBufferedChunks() {
    return this._sessionChunks.length;
  }

  get sessionBufferedBytes() {
    return this._sessionBufferedBytes;
  }

  set onSegment(fn) {
    this._onSegment = typeof fn === 'function' ? fn : null;
  }

  set onStarted(fn) {
    this._onStarted = typeof fn === 'function' ? fn : null;
  }

  set onStopped(fn) {
    this._onStopped = typeof fn === 'function' ? fn : null;
  }

  set onError(fn) {
    this._onError = typeof fn === 'function' ? fn : null;
  }

  async start(ws) {
    if (this._active) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      this._emitError('WebSocket not connected');
      return;
    }

    this._ws = ws;
    this._clearAudioBuffers();

    try {
      this._stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (err) {
      this._emitError('Microphone access denied: ' + err.message);
      return;
    }

    this._active = true;
    ws.send(JSON.stringify({ type: 'participant_start' }));
    await this._startSileroCapture();
  }

  async _startSileroCapture() {
    try {
      const instance = await initVAD({
        stream: this._stream,
        positiveSpeechThreshold: 0.5,
        negativeSpeechThreshold: 0.3,
        redemptionMs: 800,
        minSpeechMs: 300,
        preSpeechPadMs: 300,
        onSpeechEnd: (audio) => {
          void this._handleSpeechEnd(audio);
        },
        onError: (err) => this._handleCaptureError(err),
      });

      if (!this._active) {
        if (instance) instance.destroy();
        return;
      }
      if (!instance) {
        this._handleCaptureError(new Error('Silero VAD unavailable'));
        return;
      }

      this._vadInstance = instance;
      instance.start();
    } catch (err) {
      this._handleCaptureError(err);
    }
  }

  stop() {
    if (!this._active) return;
    this._active = false;
    this._clearAudioBuffers();

    if (this._vadInstance) {
      try { this._vadInstance.destroy(); } catch (_) {}
      this._vadInstance = null;
    }

    if (this._stream) {
      for (const track of this._stream.getTracks()) {
        track.stop();
      }
      this._stream = null;
    }

    if (this._ws && this._ws.readyState === WebSocket.OPEN) {
      this._ws.send(JSON.stringify({ type: 'participant_stop' }));
    }
    this._ws = null;
  }

  handleMessage(msg) {
    if (!msg || typeof msg.type !== 'string') return false;
    switch (msg.type) {
      case 'participant_started':
        this._sessionId = msg.session_id || null;
        if (this._onStarted) this._onStarted(msg);
        return true;
      case 'participant_segment_text':
        if (this._onSegment) this._onSegment(msg);
        return true;
      case 'participant_stopped':
        this._sessionId = null;
        this._cleanup();
        if (this._onStopped) this._onStopped(msg);
        return true;
      case 'participant_error':
        this._sessionId = null;
        this._cleanup();
        this._emitError(msg.error || 'unknown participant error');
        return true;
      default:
        return false;
    }
  }

  destroy() {
    this.stop();
    this._sessionId = null;
    this._onSegment = null;
    this._onStarted = null;
    this._onStopped = null;
    this._onError = null;
  }

  _cleanup() {
    this._active = false;
    this._clearAudioBuffers();
    if (this._vadInstance) {
      try { this._vadInstance.destroy(); } catch (_) {}
      this._vadInstance = null;
    }
    if (this._stream) {
      for (const track of this._stream.getTracks()) {
        track.stop();
      }
      this._stream = null;
    }
    this._ws = null;
  }

  _emitError(message) {
    if (this._onError) {
      this._onError(message);
    }
  }

  async _handleSpeechEnd(audio) {
    if (!this._active || !this._ws) return;
    const samples = normalizeSegmentSamples(audio, this._sampleRate, this._maxSegmentDurationMS);
    if (!samples) return;

    this._clearRollingSamples();
    this._rollingSamples = samples;
    const wavBlob = float32ToWav(samples, this._sampleRate);
    if (!(wavBlob instanceof Blob) || wavBlob.size <= 44) {
      this._clearRollingSamples();
      return;
    }

    let tempBytes = null;
    try {
      tempBytes = new Uint8Array(await wavBlob.arrayBuffer());
      this._retainSessionChunk(tempBytes);
      if (this._active && this._ws?.readyState === WebSocket.OPEN) {
        this._ws.send(wavBlob);
      }
    } catch (err) {
      this._handleCaptureError(err);
    } finally {
      zeroizeByteArray(tempBytes);
      this._clearRollingSamples();
    }
  }

  _retainSessionChunk(bytes) {
    if (!(bytes instanceof Uint8Array) || bytes.length === 0) return;
    if (bytes.length > this._sessionRamCapBytes) {
      this._clearSessionChunks();
      return;
    }
    while (this._sessionBufferedBytes + bytes.length > this._sessionRamCapBytes && this._sessionChunks.length > 0) {
      const dropped = this._sessionChunks.shift();
      zeroizeByteArray(dropped);
      this._sessionBufferedBytes -= dropped ? dropped.length : 0;
    }
    const copy = new Uint8Array(bytes.length);
    copy.set(bytes);
    this._sessionChunks.push(copy);
    this._sessionBufferedBytes += copy.length;
  }

  _handleCaptureError(err) {
    this._cleanup();
    const message = err && typeof err === 'object' && 'message' in err
      ? String(err.message || 'unknown participant error')
      : String(err || 'unknown participant error');
    this._emitError(message);
  }

  _clearAudioBuffers() {
    this._clearRollingSamples();
    this._clearSessionChunks();
  }

  _clearRollingSamples() {
    if (this._rollingSamples instanceof Float32Array) {
      this._rollingSamples.fill(0);
    }
    this._rollingSamples = null;
  }

  _clearSessionChunks() {
    for (const chunk of this._sessionChunks) {
      zeroizeByteArray(chunk);
    }
    this._sessionChunks = [];
    this._sessionBufferedBytes = 0;
  }
}

function normalizePositiveNumber(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function normalizeBytesCap(sessionRamCapMB, fallback) {
  const mb = Number(sessionRamCapMB);
  if (!Number.isFinite(mb) || mb <= 0) return fallback;
  return Math.max(1, Math.floor(mb * 1024 * 1024));
}

function normalizeSegmentSamples(audio, sampleRate, maxSegmentDurationMS) {
  if (!(audio instanceof Float32Array) || audio.length === 0) return null;
  const maxSamples = Math.max(1, Math.floor(sampleRate * (maxSegmentDurationMS / 1000)));
  const start = audio.length > maxSamples ? audio.length - maxSamples : 0;
  return new Float32Array(audio.subarray(start));
}

function zeroizeByteArray(bytes) {
  if (bytes instanceof Uint8Array) {
    bytes.fill(0);
  }
}
