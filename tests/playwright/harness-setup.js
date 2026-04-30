// Slopshell Playwright harness — runtime setup. See harness.html for entry.
// This file declares lexical state shared by harness-fixtures.js,
// harness-pr-fixtures.js, harness-routes-*.js, and harness-runtime.js,
// installs the touch/TouchEvent polyfill for headless WebKit, and wires
// the fetch dispatcher that walks __harnessRouteHandlers.
    // Touch + TouchEvent polyfill for WebKit headless (throws "Illegal
    // constructor" for Touch and rejects non-native Touch objects in
    // TouchEvent). All touch-based Playwright tests need this.
    (function() {
      var needsTouch = false;
      if (typeof Touch === 'undefined') {
        needsTouch = true;
      } else {
        try { new Touch({ identifier: 0, target: document.body }); } catch { needsTouch = true; }
      }
      if (needsTouch) {
        function TouchPolyfill(init) {
          this.identifier = init.identifier ?? 0;
          this.target = init.target ?? document.body;
          this.clientX = init.clientX ?? 0;
          this.clientY = init.clientY ?? 0;
          this.pageX = init.pageX ?? init.clientX ?? 0;
          this.pageY = init.pageY ?? init.clientY ?? 0;
          this.screenX = init.screenX ?? init.clientX ?? 0;
          this.screenY = init.screenY ?? init.clientY ?? 0;
          this.radiusX = init.radiusX ?? 0;
          this.radiusY = init.radiusY ?? 0;
          this.rotationAngle = init.rotationAngle ?? 0;
          this.force = init.force ?? 0;
        }
        Object.defineProperty(window, 'Touch', {
          value: TouchPolyfill, writable: true, configurable: true,
        });
      }
      var needsTouchEvent = typeof TouchEvent === 'undefined';
      if (!needsTouchEvent && needsTouch) {
        try {
          var t = new Touch({ identifier: 0, target: document.body });
          new TouchEvent('touchstart', { touches: [t], changedTouches: [t], bubbles: true });
        } catch { needsTouchEvent = true; }
      }
      if (needsTouchEvent) {
        function TouchEventPolyfill(type, init) {
          var ev = new Event(type, init);
          ev.touches = (init && init.touches) || [];
          ev.changedTouches = (init && init.changedTouches) || [];
          ev.targetTouches = (init && init.targetTouches) || [];
          return ev;
        }
        Object.defineProperty(window, 'TouchEvent', {
          value: TouchEventPolyfill, writable: true, configurable: true,
        });
      }
    })();

    // Harness log for test assertions
    window.__harnessLog = [];
    const DEFAULT_CANCEL_RESPONSE = { ok: true, canceled: 2, active_canceled: 1, queued_canceled: 1 };
    let cancelResponsesQueue = [];
    let activityResponse = { ok: true, active_turns: 0, queued_turns: 0 };
    let messagePostDelayMs = 0;
    let sttTranscribeStatus = 200;
    let sttTranscribePayload = { text: 'hello world' };
    let sttTranscribeResponsesQueue = [];
    window.__setCancelResponses = (responses) => {
      cancelResponsesQueue = Array.isArray(responses) ? responses.map((entry) => ({ ...entry })) : [];
    };
    window.__setActivityResponse = (response) => {
      activityResponse = {
        ok: true,
        active_turns: Number(response?.active_turns || 0),
        queued_turns: Number(response?.queued_turns || 0),
      };
    };
    window.__setMessagePostDelay = (ms) => {
      const n = Number(ms);
      messagePostDelayMs = Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
    };
    window.__setSTTTranscribeResponse = (payload, status = 200) => {
      sttTranscribePayload = payload && typeof payload === 'object' ? { ...payload } : {};
      const n = Number(status);
      sttTranscribeStatus = Number.isFinite(n) ? Math.max(100, Math.min(599, Math.floor(n))) : 200;
    };
    window.__queueSTTTranscribeResponses = (responses) => {
      sttTranscribeResponsesQueue = Array.isArray(responses)
        ? responses.map((entry) => ({
            payload: entry?.payload && typeof entry.payload === 'object' ? { ...entry.payload } : {},
            status: Number.isFinite(Number(entry?.status))
              ? Math.max(100, Math.min(599, Math.floor(Number(entry.status))))
              : 200,
          }))
        : [];
    };
    window.__setCancelResponses([]);
    window.__setActivityResponse({ active_turns: 0, queued_turns: 0 });
    window.__setMessagePostDelay(0);
    window.__setSTTTranscribeResponse({ text: 'hello world' }, 200);
    window.__queueSTTTranscribeResponses([]);
    window.__confirmResponse = true;
    window.confirm = (message) => {
      window.__harnessLog.push({ type: 'confirm', message: String(message || '') });
      return Boolean(window.__confirmResponse);
    };
    const harnessParams = new URL(window.location.href).searchParams;
    const runtimeState = {
      dev_mode: false,
      boot_id: 'test',
      version: '0.1.8',
      started_at: '2026-03-08T14:00:00Z',
      tts_enabled: true,
      turn_intelligence_enabled: true,
      turn_policy_profile: 'balanced',
      turn_eval_logging_enabled: true,
      silent_mode: false,
      fast_mode: false,
      live_policy: harnessParams.get('live_policy') === 'meeting' ? 'meeting' : 'dialogue',
      tool: 'pointer',
      startup_behavior: 'resume_active',
      active_sphere: 'private',
    };
    let dictationState = {
      active: false,
      target_kind: 'document_section',
      target_label: 'Document Section',
      prompt: '',
      artifact_title: '',
      transcript: '',
      draft_text: '',
      scratch_path: '',
    };
    function dictationLabel(targetKind) {
      if (targetKind === 'email_draft') return 'Email Draft';
      if (targetKind === 'email_reply') return 'Email Reply';
      if (targetKind === 'review_comment') return 'Review Comment';
      return 'Document Section';
    }
    function dictationTargetForPrompt(prompt, artifactTitle) {
      const combined = String(`${prompt || ''}\n${artifactTitle || ''}`).toLowerCase();
      if (combined.includes('review') || combined.includes('.diff') || combined.includes('pr ')) return 'review_comment';
      if (combined.includes('reply') || combined.includes('thread')) return 'email_reply';
      if (combined.includes('email') || combined.includes('letter')) return 'email_draft';
      return 'document_section';
    }
    function dictationDraftForState(current) {
      const transcript = String(current?.transcript || '').trim();
      const title = String(current?.artifact_title || '').trim();
      const paragraphs = transcript
        ? transcript.split(/\n\s*\n/).map((part) => part.trim().replace(/\s+/g, ' ')).filter(Boolean)
        : [];
      if (current?.target_kind === 'email_draft') {
        return `# Email Draft\n\n${title ? `Subject: ${title}\n\n` : ''}${paragraphs.join('\n\n') || 'Start speaking to build the message.'}`;
      }
      if (current?.target_kind === 'email_reply') {
        return `# Email Reply Draft\n\n${title ? `Thread: ${title}\n\n` : ''}${paragraphs.join('\n\n') || 'Start speaking to build the reply.'}`;
      }
      if (current?.target_kind === 'review_comment') {
        const bullets = paragraphs.length > 0 ? paragraphs.map((part) => `- ${part}`).join('\n') : 'Start speaking to build the comment.';
        return `# Review Comment Draft\n\n${title ? `Target: ${title}\n\n` : ''}${bullets}`;
      }
      return `# Document Section Draft\n\n${title ? `Working title: ${title}\n\n` : ''}${paragraphs.join('\n\n') || 'Start speaking to build the section.'}`;
    }
    const TEST_BUG_REPORT_PNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5W8xkAAAAASUVORK5CYII=';
    window.__slopshellBugReportTestEnv = { screenshotDataURL: TEST_BUG_REPORT_PNG };
    window.__bugReportRequests = [];
    window.__setBugReportScreenshotDataURL = (value) => {
      window.__slopshellBugReportTestEnv = { screenshotDataURL: String(value || TEST_BUG_REPORT_PNG) };
    };
    window.__setRuntimeState = (patch) => {
      const next = patch && typeof patch === 'object' ? patch : {};
      if (Object.prototype.hasOwnProperty.call(next, 'boot_id')) {
        runtimeState.boot_id = String(next.boot_id || '').trim() || runtimeState.boot_id;
      }
      if (Object.prototype.hasOwnProperty.call(next, 'dev_mode')) {
        runtimeState.dev_mode = Boolean(next.dev_mode);
      }
      if (Object.prototype.hasOwnProperty.call(next, 'silent_mode')) {
        runtimeState.silent_mode = Boolean(next.silent_mode);
      }
      if (Object.prototype.hasOwnProperty.call(next, 'fast_mode')) {
        runtimeState.fast_mode = Boolean(next.fast_mode);
      }
      if (Object.prototype.hasOwnProperty.call(next, 'live_policy')) {
        runtimeState.live_policy = String(next.live_policy || '').trim().toLowerCase() === 'meeting' ? 'meeting' : 'dialogue';
      }
      if (Object.prototype.hasOwnProperty.call(next, 'tool')) {
        const normalized = String(next.tool || '').trim().toLowerCase();
        runtimeState.tool = ['pointer', 'highlight', 'ink', 'text_note', 'prompt'].includes(normalized)
          ? normalized
          : 'pointer';
      }
      if (Object.prototype.hasOwnProperty.call(next, 'startup_behavior')) {
        runtimeState.startup_behavior = 'resume_active';
      }
      if (Object.prototype.hasOwnProperty.call(next, 'active_sphere')) {
        runtimeState.active_sphere = String(next.active_sphere || '').trim().toLowerCase() === 'work' ? 'work' : 'private';
      }
      if (Object.prototype.hasOwnProperty.call(next, 'turn_policy_profile')) {
        const normalized = String(next.turn_policy_profile || '').trim().toLowerCase();
        runtimeState.turn_policy_profile = ['balanced', 'patient', 'assertive'].includes(normalized)
          ? normalized
          : 'balanced';
      }
      if (Object.prototype.hasOwnProperty.call(next, 'turn_eval_logging_enabled')) {
        runtimeState.turn_eval_logging_enabled = Boolean(next.turn_eval_logging_enabled);
      }
    };
    window.__getRuntimeState = () => ({ ...runtimeState });
    let hotwordInitEnabled = harnessParams.get('hotword') !== 'fail';
    const safariRecorderBroken = harnessParams.get('safari-recorder') === 'broken';
    let ttsPlaybackDelayMs = 10;
    let hotwordStatus = {
      ok: true,
      ready: true,
      missing: [],
    };
    window.__setHotwordStatus = (next) => {
      const incoming = next && typeof next === 'object' ? next : {};
      hotwordStatus = { ...hotwordStatus, ...incoming };
      if (!Array.isArray(hotwordStatus.missing)) {
        hotwordStatus.missing = [];
      }
      hotwordStatus.ready = Boolean(hotwordStatus.ready);
    };
    window.__setHotwordInitEnabled = (enabled) => {
      hotwordInitEnabled = Boolean(enabled);
    };
    window.__setTTSPlaybackDelayMs = (ms) => {
      const n = Number(ms);
      ttsPlaybackDelayMs = Number.isFinite(n) && n >= 0 ? Math.floor(n) : 10;
    };
    window.__setTTSPlaybackDelayMs(10);
    window.__slopshellHotwordMock = {
      active: false,
      onDetect: null,
      threshold: 0.3,
      init() {
        window.__harnessLog.push({ type: 'hotword', action: 'init', enabled: hotwordInitEnabled });
        return hotwordInitEnabled;
      },
      start(_stream, onDetect) {
        this.active = true;
        this.onDetect = typeof onDetect === 'function' ? onDetect : null;
        window.__harnessLog.push({ type: 'hotword', action: 'start' });
      },
      stop() {
        if (this.active) {
          window.__harnessLog.push({ type: 'hotword', action: 'stop' });
        }
        this.active = false;
        this.onDetect = null;
      },
      setThreshold(value) {
        this.threshold = Number(value);
        window.__harnessLog.push({ type: 'hotword', action: 'threshold', value: this.threshold });
      },
      trigger() {
        if (!this.active || typeof this.onDetect !== 'function') return;
        window.__harnessLog.push({ type: 'hotword', action: 'detect' });
        this.onDetect();
      },
    };
    window.__triggerHotwordDetection = () => {
      if (window.__slopshellHotwordMock) {
        window.__slopshellHotwordMock.trigger();
      }
    };
    window.__isHotwordActive = () => Boolean(window.__slopshellHotwordMock?.active);

    // Mock Silero VAD: interprets __vadDbFrames with adaptive noise floor.
    // Speech detection uses relative thresholding (2 dB above calibrated floor).
    const VAD_MOCK_FRAME_MS = 40;
    const VAD_MOCK_CALIBRATION_FRAMES = 8;
    const VAD_MOCK_SPEECH_OFFSET_DB = 2;
    const VAD_MOCK_SPEECH_START_FRAMES = 3;
    const VAD_MOCK_REDEMPTION_FRAMES = 8;
    window.__slopshellVadMock = {
      init() { return true; },
      create(callbacks) {
        let running = false;
        let timer = null;
        let speechActive = false;
        let speechFrameCount = 0;
        let silenceAfterSpeechCount = 0;
        let calibrationSamples = [];
        let noiseFloorDb = null;
        return {
          start() {
            if (running) return;
            running = true;
            speechActive = false;
            speechFrameCount = 0;
            silenceAfterSpeechCount = 0;
            calibrationSamples = [];
            noiseFloorDb = null;
            timer = setInterval(() => {
              if (!running) return;
              const script = Array.isArray(window.__vadDbFrames) ? window.__vadDbFrames : [];
              const db = script.length > 0 ? Number(script.shift()) : -80;

              if (noiseFloorDb === null) {
                calibrationSamples.push(db);
                if (calibrationSamples.length >= VAD_MOCK_CALIBRATION_FRAMES) {
                  const sorted = calibrationSamples.slice().sort((a, b) => a - b);
                  noiseFloorDb = sorted[Math.floor(sorted.length * 0.35)] || -60;
                }
                return;
              }

              const speechThreshold = noiseFloorDb + VAD_MOCK_SPEECH_OFFSET_DB;
              const isSpeech = db > speechThreshold;
              const speechDelta = Math.max(0, db - speechThreshold);
              const speechProb = Math.max(0, Math.min(1, isSpeech ? 0.78 + (speechDelta / 20) : 0.08));

              if (callbacks.onFrameProcessed) {
                callbacks.onFrameProcessed({ isSpeech: speechProb });
              }

              if (!speechActive) {
                if (isSpeech) {
                  speechFrameCount += 1;
                  if (speechFrameCount >= VAD_MOCK_SPEECH_START_FRAMES) {
                    speechActive = true;
                    silenceAfterSpeechCount = 0;
                    if (callbacks.onSpeechStart) callbacks.onSpeechStart();
                  }
                } else {
                  speechFrameCount = 0;
                }
              } else {
                if (isSpeech) {
                  silenceAfterSpeechCount = 0;
                } else {
                  silenceAfterSpeechCount += 1;
                  if (silenceAfterSpeechCount >= VAD_MOCK_REDEMPTION_FRAMES) {
                    speechActive = false;
                    speechFrameCount = 0;
                    silenceAfterSpeechCount = 0;
                    if (callbacks.onSpeechEnd) callbacks.onSpeechEnd(new Float32Array(160));
                  }
                }
              }
            }, VAD_MOCK_FRAME_MS);
          },
          pause() {
            running = false;
            if (timer) { clearInterval(timer); timer = null; }
          },
          destroy() {
            running = false;
            if (timer) { clearInterval(timer); timer = null; }
          },
        };
      },
    };

    window.__injectParticipantSegment = (segment) => {
      const sessions = window.__mockWsSessions || [];
      const chatWs = sessions.find((ws) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
      if (chatWs?.onmessage) {
        chatWs.onmessage({ data: JSON.stringify({
          type: 'participant_segment_text',
          session_id: segment?.session_id || 'psess-harness-001',
          text: segment?.text || 'test segment',
          segment_id: segment?.segment_id || 1,
          start_ts: segment?.start_ts || Math.floor(Date.now() / 1000),
          end_ts: segment?.end_ts || Math.floor(Date.now() / 1000),
          latency_ms: segment?.latency_ms || 100,
        }) });
      }
    };
    const _realFetch = window.fetch;
    const __harnessRouteHandlers = [];
    window.fetch = async function(url, opts) {
      const u = String(url);
      for (const handler of __harnessRouteHandlers) {
        const result = await handler(u, opts);
        if (result !== undefined) return result;
      }
      return _realFetch.call(this, url, opts);
    };
