// Slopshell Playwright harness — WebSocket, AudioContext, getUserMedia, MediaRecorder mocks.
    // Mock WebSocket with STT message handling and chat events
    const _NativeWebSocket = window.WebSocket;
    function normalizeHarnessTurnText(value) {
      return String(value || '').replace(/\s+/g, ' ').trim();
    }

    function decideHarnessTurnAction(turnState, message) {
      const text = normalizeHarnessTurnText(message?.text);
      if (!text) {
        return { type: 'turn_action', action: 'backchannel', reason: 'empty' };
      }
      const pendingText = normalizeHarnessTurnText(turnState?.pendingText);
      const combinedText = normalizeHarnessTurnText([pendingText, text].filter(Boolean).join(' '));
      const lowered = combinedText.toLowerCase();
      if (!pendingText && message?.interrupted_assistant === true && ['ok', 'okay', 'yes', 'right', 'sure', 'thanks'].includes(lowered)) {
        return { type: 'turn_action', action: 'backchannel', text: combinedText, reason: 'assistant_backchannel' };
      }
      if (/[,:;-]$/.test(combinedText) || !/[.!?]$/.test(combinedText)) {
        const words = combinedText.split(/\s+/).filter(Boolean);
        if (words.length <= 2 || combinedText.length < 18) {
          turnState.pendingText = combinedText;
          return { type: 'turn_action', action: 'continue_listening', text: combinedText, reason: 'fragment', wait_ms: 650 };
        }
      }
      turnState.pendingText = '';
      return { type: 'turn_action', action: 'finalize_user_turn', text: combinedText, reason: /[.!?]$/.test(combinedText) ? 'terminal_punctuation' : 'semantic_completion' };
    }

    class MockWebSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;
      constructor(url) {
        this.url = url;
        this.readyState = MockWebSocket.OPEN;
        this.binaryType = 'blob';
        this.onopen = null;
        this.onmessage = null;
        this.onclose = null;
        this.__turnState = {
          pendingText: '',
          playing: false,
          playedMs: 0,
          speechFrames: 0,
          profile: runtimeState.turn_policy_profile,
          evalLoggingEnabled: runtimeState.turn_eval_logging_enabled,
        };
        // Track all WS instances for test injection
        if (!window.__mockWsSessions) window.__mockWsSessions = [];
        window.__mockWsSessions.push(this);
        setTimeout(() => {
          if (this.onopen) this.onopen({});
          if (this.url.includes('/ws/turn/') && this.onmessage) {
            this.onmessage({ data: JSON.stringify({
              type: 'turn_ready',
              session_id: 'chat-local',
              profile: this.__turnState.profile,
              eval_logging_enabled: this.__turnState.evalLoggingEnabled,
              metrics: {
                profile: this.__turnState.profile,
                eval_logging_enabled: this.__turnState.evalLoggingEnabled,
                actions: {},
                playback_active: false,
                played_audio_ms: 0,
                pending_text_chars: 0,
              },
            }) });
          }
        }, 10);
      }
      send(data) {
        if (data instanceof ArrayBuffer || data instanceof Blob) {
          window.__harnessLog.push({ type: 'stt', action: 'append' });
          return;
        }
        try {
          const msg = JSON.parse(String(data));
          if (this.url.includes('/ws/turn/')) {
            const ws = this;
            const turnState = this.__turnState;
            if (msg.type === 'turn_playback') {
              turnState.playing = Boolean(msg.playing);
              turnState.playedMs = Math.max(0, Number(msg.played_ms || 0));
              if (!turnState.playing) {
                turnState.speechFrames = 0;
              }
              window.__harnessLog.push({ type: 'turn', action: 'playback', playing: turnState.playing, played_ms: turnState.playedMs });
            } else if (msg.type === 'turn_reset') {
              turnState.pendingText = '';
              turnState.speechFrames = 0;
              window.__harnessLog.push({ type: 'turn', action: 'reset' });
              if (ws.onmessage) {
                ws.onmessage({ data: JSON.stringify({
                  type: 'turn_metrics',
                  metrics: {
                    profile: turnState.profile,
                    eval_logging_enabled: turnState.evalLoggingEnabled,
                    actions: {},
                    playback_active: turnState.playing,
                    played_audio_ms: turnState.playedMs,
                    pending_text_chars: 0,
                    metadata: { last_update: 'reset' },
                  },
                }) });
              }
            } else if (msg.type === 'turn_config') {
              if (typeof msg.profile === 'string') {
                const normalized = String(msg.profile).trim().toLowerCase();
                turnState.profile = ['balanced', 'patient', 'assertive'].includes(normalized) ? normalized : 'balanced';
              }
              if (Object.prototype.hasOwnProperty.call(msg, 'eval_logging_enabled')) {
                turnState.evalLoggingEnabled = Boolean(msg.eval_logging_enabled);
              }
              window.__harnessLog.push({ type: 'turn', action: 'config', profile: turnState.profile, eval_logging_enabled: turnState.evalLoggingEnabled });
              if (ws.onmessage) {
                ws.onmessage({ data: JSON.stringify({
                  type: 'turn_metrics',
                  metrics: {
                    profile: turnState.profile,
                    eval_logging_enabled: turnState.evalLoggingEnabled,
                    actions: {},
                    playback_active: turnState.playing,
                    played_audio_ms: turnState.playedMs,
                    pending_text_chars: normalizeHarnessTurnText(turnState.pendingText).length,
                    metadata: { last_update: 'profile' },
                  },
                }) });
              }
            } else if (msg.type === 'turn_listen_state') {
              if (!msg.active) {
                turnState.pendingText = '';
              }
              window.__harnessLog.push({ type: 'turn', action: 'listen_state', active: Boolean(msg.active) });
            } else if (msg.type === 'turn_speech_start') {
              window.__harnessLog.push({ type: 'turn', action: 'speech_start', interrupted_assistant: Boolean(msg.interrupted_assistant) });
              if (turnState.playing || msg.interrupted_assistant) {
                setTimeout(() => {
                  if (ws.onmessage) {
                    ws.onmessage({ data: JSON.stringify({
                      type: 'turn_action',
                      action: 'yield',
                      reason: 'speech_start',
                      interrupt_assistant: true,
                      rollback_audio_ms: Math.min(350, Math.max(0, Number(turnState.playedMs || 0))),
                    }) });
                  }
                }, 5);
              }
            } else if (msg.type === 'turn_speech_prob') {
              const prob = Number(msg.speech_prob || 0);
              if (turnState.playing && prob >= 0.75) {
                turnState.speechFrames += 1;
                if (turnState.speechFrames >= 3) {
                  turnState.speechFrames = 0;
                  setTimeout(() => {
                    if (ws.onmessage) {
                      ws.onmessage({ data: JSON.stringify({
                        type: 'turn_action',
                        action: 'yield',
                        reason: 'speech_overlap',
                        interrupt_assistant: true,
                        rollback_audio_ms: Math.min(350, Math.max(0, Number(turnState.playedMs || 0))),
                      }) });
                    }
                  }, 5);
                }
              } else {
                turnState.speechFrames = 0;
              }
            } else if (msg.type === 'turn_transcript_segment') {
              window.__harnessLog.push({ type: 'turn', action: 'segment', text: String(msg.text || '') });
              const actionPayload = decideHarnessTurnAction(turnState, msg);
              setTimeout(() => {
                if (ws.onmessage) {
                  ws.onmessage({ data: JSON.stringify(actionPayload) });
                }
              }, 5);
            }
            return;
          }
          if (msg.type === 'stt_start') {
            window.__harnessLog.push({ type: 'stt', action: 'start', mime_type: msg.mime_type });
          } else if (msg.type === 'stt_stop') {
            window.__harnessLog.push({ type: 'stt', action: 'stop' });
            const queued = Array.isArray(sttTranscribeResponsesQueue) && sttTranscribeResponsesQueue.length > 0
              ? sttTranscribeResponsesQueue.shift()
              : null;
            const payload = queued?.payload && typeof queued.payload === 'object'
              ? queued.payload
              : sttTranscribePayload;
            const ws = this;
            setTimeout(() => {
              if (ws.onmessage) {
                if (payload?.text) {
                  ws.onmessage({ data: JSON.stringify({ type: 'stt_result', text: String(payload.text) }) });
                } else {
                  ws.onmessage({ data: JSON.stringify({ type: 'stt_empty', reason: String(payload?.reason || 'empty_transcript') }) });
                }
              }
            }, 5);
          } else if (msg.type === 'stt_cancel') {
            window.__harnessLog.push({ type: 'stt', action: 'cancel' });
          } else if (msg.type === 'participant_start') {
            const ws = this;
            if (!window.__participantConfig?.companion_enabled) {
              window.__harnessLog.push({ type: 'participant', action: 'blocked' });
              setTimeout(() => {
                if (ws.onmessage) {
                  ws.onmessage({ data: JSON.stringify({ type: 'participant_error', error: 'meeting mode is disabled' }) });
                }
              }, 5);
              return;
            }
            window.__harnessLog.push({ type: 'participant', action: 'start' });
            setTimeout(() => {
              if (ws.onmessage) {
                ws.onmessage({ data: JSON.stringify({ type: 'participant_started', session_id: 'psess-harness-001' }) });
              }
            }, 5);
          } else if (msg.type === 'participant_stop') {
            window.__harnessLog.push({ type: 'participant', action: 'stop' });
            const ws = this;
            setTimeout(() => {
              if (ws.onmessage) {
                ws.onmessage({ data: JSON.stringify({ type: 'participant_stopped', session_id: 'psess-harness-001' }) });
              }
            }, 5);
          } else if (msg.type === 'tts_speak') {
            window.__harnessLog.push({ type: 'tts', text: msg.text, lang: msg.lang });
            const ws = this;
            // Return minimal WAV as binary ArrayBuffer
            const wavHeader = new Uint8Array([
              0x52,0x49,0x46,0x46, 0x24,0x00,0x00,0x00, 0x57,0x41,0x56,0x45,
              0x66,0x6D,0x74,0x20, 0x10,0x00,0x00,0x00, 0x01,0x00,0x01,0x00,
              0x44,0xAC,0x00,0x00, 0x88,0x58,0x01,0x00, 0x02,0x00,0x10,0x00,
              0x64,0x61,0x74,0x61, 0x00,0x00,0x00,0x00,
            ]);
            setTimeout(() => {
              if (ws.onmessage) {
                ws.onmessage({ data: wavHeader.buffer });
              }
            }, 5);
          } else if (msg.type === 'canvas_position') {
            window.__harnessLog.push({
              type: 'canvas_position',
              gesture: msg.gesture,
              request_response: Boolean(msg.request_response),
              output_mode: msg.output_mode,
              cursor: msg.cursor || null,
              snapshot_data_url: String(msg.snapshot_data_url || ''),
            });
          } else if (msg.type === 'canvas_ink') {
            window.__harnessLog.push({
              type: 'canvas_ink',
              request_response: Boolean(msg.request_response),
              output_mode: msg.output_mode,
              artifact_kind: msg.artifact_kind,
              cursor: msg.cursor || null,
              total_strokes: Number(msg.total_strokes || 0),
              bounding_box: msg.bounding_box || null,
              overlapping_lines: msg.overlapping_lines || null,
              overlapping_text: String(msg.overlapping_text || ''),
              snapshot_data_url: String(msg.snapshot_data_url || ''),
              strokes: Array.isArray(msg.strokes) ? msg.strokes : [],
            });
          }
        } catch (_) {}
      }
      close() { this.readyState = MockWebSocket.CLOSED; }
      // Helper: inject a chat event from tests
      injectEvent(payload) {
        if (this.onmessage) {
          this.onmessage({ data: JSON.stringify(payload) });
        }
      }
    }
    window.WebSocket = MockWebSocket;

    // Mock AudioContext for TTS playback and VAD analyser input.
    function fillTimeDomainForDb(target, db) {
      if (!(target instanceof Uint8Array)) return;
      const clampedDb = Number.isFinite(db) ? db : -20;
      if (clampedDb <= -95) {
        target.fill(128);
        return;
      }
      const amplitude = Math.max(1, Math.min(127, Math.round(128 * Math.pow(10, clampedDb / 20))));
      for (let i = 0; i < target.length; i += 1) {
        target[i] = (i % 2 === 0) ? 128 + amplitude : 128 - amplitude;
      }
    }
    window.__setVadDbFrames = (frames) => {
      window.__vadDbFrames = Array.isArray(frames) ? frames.slice() : [];
    };
    window.__setVadDbFrames([]);
    window.__mediaRecorderMimeType = 'audio/webm;codecs=opus';
    window.__setMediaRecorderMimeType = (mimeType) => {
      if (mimeType === null || typeof mimeType === 'undefined') {
        window.__mediaRecorderMimeType = 'audio/webm;codecs=opus';
      } else {
        window.__mediaRecorderMimeType = String(mimeType).trim();
      }
    };
    window.__mediaRecorderChunkMimeType = null;
    window.__setMediaRecorderChunkMimeType = (mimeType) => {
      if (mimeType === null || typeof mimeType === 'undefined') {
        window.__mediaRecorderChunkMimeType = null;
      } else {
        window.__mediaRecorderChunkMimeType = String(mimeType).trim();
      }
    };
    window.__mediaRecorderChunkBytes = [1, 2, 3, 4, 5];
    window.__setMediaRecorderChunkBytes = (bytes) => {
      if (Array.isArray(bytes) && bytes.length > 0) {
        window.__mediaRecorderChunkBytes = bytes
          .map((n) => Number(n))
          .filter((n) => Number.isFinite(n))
          .map((n) => Math.max(0, Math.min(255, Math.trunc(n))));
      } else {
        window.__mediaRecorderChunkBytes = [1, 2, 3, 4, 5];
      }
    };
    window.__mediaRecorderRequestDataEnabled = true;
    window.__setMediaRecorderRequestDataEnabled = (enabled) => {
      window.__mediaRecorderRequestDataEnabled = Boolean(enabled);
    };

    const MockAudioContext = class {
      constructor() {
        this.currentTime = 0;
        this.destination = {};
        this.state = 'running';
      }
      async resume() {
        this.state = 'running';
      }
      async decodeAudioData(buf) {
        const length = 4410;
        const channel = new Float32Array(length);
        for (let i = 0; i < length; i += 1) {
          const phase = (i / 44100) * 2 * Math.PI * 440;
          channel[i] = Math.sin(phase) * 0.1;
        }
        return {
          duration: 0.1,
          length,
          sampleRate: 44100,
          numberOfChannels: 1,
          getChannelData(index) {
            if (index !== 0) return new Float32Array(length);
            return channel;
          },
        };
      }
      createMediaStreamSource() {
        return {
          connect() {},
          disconnect() {},
        };
      }
      createGain() {
        return {
          gain: { value: 1 },
          connect() {},
          disconnect() {},
        };
      }
      createAnalyser() {
        return {
          fftSize: 1024,
          smoothingTimeConstant: 0,
          frequencyBinCount: 512,
          connect() {},
          disconnect() {},
          getByteTimeDomainData(target) {
            const script = Array.isArray(window.__vadDbFrames) ? window.__vadDbFrames : [];
            const nextDb = script.length > 0 ? Number(script.shift()) : -20;
            fillTimeDomainForDb(target, nextDb);
          },
        };
      }
      createBufferSource() {
        const node = {
          buffer: null,
          onended: null,
          connect() {},
          start() {
            setTimeout(() => { if (node.onended) node.onended(); }, ttsPlaybackDelayMs);
          },
          stop() {},
        };
        return node;
      }
    };
    window.AudioContext = MockAudioContext;
    window.webkitAudioContext = MockAudioContext;

    // Mock getUserMedia
    let getUserMediaCallCount = 0;
    let currentMicTrack = null;
    let currentMicStream = null;

    function createMockMicTrack() {
      const track = new EventTarget();
      track.kind = 'audio';
      track.enabled = true;
      track.muted = false;
      track.readyState = 'live';
      track.stop = () => {
        if (track.readyState === 'ended') return;
        track.readyState = 'ended';
        track.dispatchEvent(new Event('ended'));
      };
      return track;
    }

    function createMockMicStream() {
      const stream = new EventTarget();
      const track = createMockMicTrack();
      stream.active = true;
      stream.getTracks = () => [track];
      stream.getAudioTracks = () => [track];
      stream.clone = () => {
        const cloned = createMockMicStream();
        return cloned.stream;
      };
      const endTrack = () => {
        if (track.readyState !== 'ended') {
          track.readyState = 'ended';
          track.dispatchEvent(new Event('ended'));
        }
        stream.active = false;
        stream.dispatchEvent(new Event('inactive'));
      };
      return { stream, track, endTrack };
    }

    window.__triggerMicTrackEnded = () => {
      if (currentMicTrack && currentMicTrack.readyState !== 'ended') {
        currentMicTrack.readyState = 'ended';
        currentMicTrack.dispatchEvent(new Event('ended'));
      }
      if (currentMicStream) {
        currentMicStream.active = false;
        currentMicStream.dispatchEvent(new Event('inactive'));
      }
    };

    window.__triggerMicDeviceChange = () => {
      const md = navigator.mediaDevices;
      if (md && typeof md.dispatchEvent === 'function') {
        md.dispatchEvent(new Event('devicechange'));
      }
    };

    if (!navigator.mediaDevices) {
      navigator.mediaDevices = new EventTarget();
    }
    if (typeof navigator.mediaDevices.addEventListener !== 'function') {
      const mdEvents = new EventTarget();
      navigator.mediaDevices.addEventListener = (...args) => mdEvents.addEventListener(...args);
      navigator.mediaDevices.removeEventListener = (...args) => mdEvents.removeEventListener(...args);
      navigator.mediaDevices.dispatchEvent = (...args) => mdEvents.dispatchEvent(...args);
    }
    const _mockGetUserMedia = async () => {
      const next = createMockMicStream();
      currentMicTrack = next.track;
      currentMicStream = next.stream;
      getUserMediaCallCount += 1;
      window.__harnessLog.push({ type: 'media', action: 'get_user_media', call: getUserMediaCallCount });
      return next.stream;
    };
    navigator.mediaDevices.getUserMedia = _mockGetUserMedia;

    // Mock MediaRecorder
    window.MediaRecorder = class MockMediaRecorder {
      constructor() {
        this.state = 'inactive';
        this.mimeType = typeof window.__mediaRecorderMimeType === 'string'
          ? window.__mediaRecorderMimeType
          : 'audio/webm;codecs=opus';
        this._handlers = {};
        this._chunkTimer = null;
      }
      static isTypeSupported() { return true; }
      addEventListener(type, fn, opts) {
        if (!this._handlers[type]) this._handlers[type] = [];
        this._handlers[type].push({ fn, once: !!(opts && opts.once) });
      }
      removeEventListener(type, fn) {
        if (!this._handlers[type]) return;
        this._handlers[type] = this._handlers[type].filter(h => h.fn !== fn);
      }
      _emit(type, ev) {
        const list = (this._handlers[type] || []).slice();
        const keep = [];
        for (const h of list) {
          h.fn(ev);
          if (!h.once) keep.push(h);
        }
        this._handlers[type] = keep;
      }
      start(timeslice) {
        this.state = 'recording';
        this._timeslice = timeslice;
        window.__harnessLog.push({ type: 'recorder', action: 'start', timeslice });
        const intervalMs = Math.max(20, Number(timeslice) || 100);
        this._chunkTimer = setInterval(() => {
          if (this.state !== 'recording') return;
          const bytes = Array.isArray(window.__mediaRecorderChunkBytes)
            ? window.__mediaRecorderChunkBytes
            : [1, 2, 3, 4, 5];
          const chunkMime = typeof window.__mediaRecorderChunkMimeType === 'string'
            ? window.__mediaRecorderChunkMimeType
            : this.mimeType;
          const blob = new Blob([new Uint8Array(bytes)], chunkMime ? { type: chunkMime } : undefined);
          this._emit('dataavailable', { data: blob });
        }, intervalMs);
      }
      stop() {
        if (this._chunkTimer) {
          clearInterval(this._chunkTimer);
          this._chunkTimer = null;
        }
        this.state = 'inactive';
        window.__harnessLog.push({ type: 'recorder', action: 'stop' });
        if (safariRecorderBroken) {
          // Safari fires stop before dataavailable with empty blob data.
          setTimeout(() => {
            this._emit('stop', {});
            this._emit('dataavailable', { data: new Blob([], { type: this.mimeType }) });
          }, 10);
        } else {
          const bytes = Array.isArray(window.__mediaRecorderChunkBytes)
            ? window.__mediaRecorderChunkBytes
            : [1, 2, 3, 4, 5];
          const chunkMime = typeof window.__mediaRecorderChunkMimeType === 'string'
            ? window.__mediaRecorderChunkMimeType
            : this.mimeType;
          const blob = new Blob([new Uint8Array(bytes)], chunkMime ? { type: chunkMime } : undefined);
          setTimeout(() => {
            this._emit('dataavailable', { data: blob });
            this._emit('stop', {});
          }, 10);
        }
      }
      requestData() {
        if (!window.__mediaRecorderRequestDataEnabled) return;
        window.__harnessLog.push({ type: 'recorder', action: 'requestData' });
        if (this.state !== 'recording') return;
        const bytes = Array.isArray(window.__mediaRecorderChunkBytes)
          ? window.__mediaRecorderChunkBytes
          : [1, 2, 3, 4, 5];
        const chunkMime = typeof window.__mediaRecorderChunkMimeType === 'string'
          ? window.__mediaRecorderChunkMimeType
          : this.mimeType;
        const blob = new Blob([new Uint8Array(bytes)], chunkMime ? { type: chunkMime } : undefined);
        this._emit('dataavailable', { data: blob });
      }
    };
