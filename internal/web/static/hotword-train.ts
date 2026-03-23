import { apiURL, appURL } from './paths.js';
import { float32ToWav } from './app-env.js';

type Recording = {
  id: string;
  kind: string;
  created_at: string;
  file_name: string;
  size_bytes: number;
  duration_ms: number;
  audio_url: string;
};

type StatusPayload = {
  state: string;
  stage: string;
  message?: string;
  error?: string;
  progress?: number;
  generated_samples?: number;
  latest_model?: string;
  models?: Array<{ name: string; state: string; message?: string; count?: number; target?: number; output_dir?: string }>;
};

type Feedback = {
  id: string;
  recording_id: string;
  outcome: string;
  created_at: string;
};

type FeedbackSummary = {
  total: number;
  missed_triggers: number;
  false_triggers: number;
  latest_outcome?: string;
  latest_at?: string;
};

type Model = {
  name: string;
  file_name: string;
  path: string;
  created_at: string;
  size_bytes: number;
  production: boolean;
};

type GeneratorInfo = {
  id: string;
  label: string;
  command?: string;
  available: boolean;
  recommended: boolean;
  message?: string;
};

type Settings = {
  preferred_generator: string;
  sample_count: number;
  auto_deploy: boolean;
  negative_phrases: string[];
  generator_commands?: Record<string, string>;
};

type DatasetSummary = {
  hotword_clips: number;
  reference_clips: number;
  test_clips: number;
  generated_samples: number;
  generation_running: boolean;
  training_running: boolean;
  latest_model?: string;
  production_model?: string;
  feedback: FeedbackSummary;
};

type TrainUIConfig = {
  settings: Settings;
  generators: GeneratorInfo[];
  dataset: DatasetSummary;
};

const state = {
  recordingActive: false,
  audioContext: null as AudioContext | null,
  sourceNode: null as MediaStreamAudioSourceNode | null,
  processorNode: null as ScriptProcessorNode | null,
  sinkNode: null as GainNode | null,
  stream: null as MediaStream | null,
  sampleRate: 16000,
  chunks: [] as Float32Array[],
  recordings: [] as Recording[],
  feedback: [] as Feedback[],
  trainConfig: null as TrainUIConfig | null,
};

function byId<T extends HTMLElement>(id: string) {
  const node = document.getElementById(id);
  if (!(node instanceof HTMLElement)) {
    throw new Error(`missing element: ${id}`);
  }
  return node as T;
}

const bannerEl = byId<HTMLParagraphElement>('train-banner');
const pipelineBadgeEl = byId<HTMLSpanElement>('pipeline-badge');
const pipelineStatusEl = byId<HTMLParagraphElement>('pipeline-status');
const pipelineFillEl = byId<HTMLDivElement>('pipeline-progress-fill');
const pipelineProgressLabelEl = byId<HTMLParagraphElement>('pipeline-progress-label');
const datasetSummaryEl = byId<HTMLDivElement>('dataset-summary');
const configSaveEl = byId<HTMLButtonElement>('config-save');
const pipelineStartEl = byId<HTMLButtonElement>('pipeline-start');
const preferredGeneratorEl = byId<HTMLSelectElement>('trainer-preferred');
const trainerSampleCountEl = byId<HTMLInputElement>('trainer-sample-count');
const trainerAutoDeployEl = byId<HTMLInputElement>('trainer-auto-deploy');
const trainerNegativePhrasesEl = byId<HTMLTextAreaElement>('trainer-negative-phrases');
const generatorStatusListEl = byId<HTMLDivElement>('generator-status-list');
const qwenCommandEl = byId<HTMLInputElement>('generator-command-qwen3tts');
const gptSovitsCommandEl = byId<HTMLInputElement>('generator-command-gptsovits');
const kokoroCommandEl = byId<HTMLInputElement>('generator-command-kokoro');
const piperCommandEl = byId<HTMLInputElement>('generator-command-piper');

const recordingKindEl = byId<HTMLSelectElement>('recording-kind');
const recordingToggleEl = byId<HTMLButtonElement>('recording-toggle');
const recordingUploadEl = byId<HTMLInputElement>('recording-upload');
const recordingBadgeEl = byId<HTMLSpanElement>('recording-badge');
const recordingStatusEl = byId<HTMLParagraphElement>('recording-status');
const recordingListEl = byId<HTMLUListElement>('recording-list');

const generationBadgeEl = byId<HTMLSpanElement>('generation-badge');
const generationStatusEl = byId<HTMLParagraphElement>('generation-status');
const generationListEl = byId<HTMLUListElement>('generation-list');
const generationCountEl = byId<HTMLInputElement>('generation-count');
const generationStartEl = byId<HTMLButtonElement>('generation-start');

const trainingBadgeEl = byId<HTMLSpanElement>('training-badge');
const trainingStatusEl = byId<HTMLParagraphElement>('training-status');
const trainingStartEl = byId<HTMLButtonElement>('training-start');

const testingBadgeEl = byId<HTMLSpanElement>('testing-badge');
const testingStatusEl = byId<HTMLParagraphElement>('testing-status');
const testingUploadEl = byId<HTMLInputElement>('testing-upload');
const testingListEl = byId<HTMLUListElement>('testing-list');
const feedbackStatusEl = byId<HTMLParagraphElement>('feedback-status');

const deploymentBadgeEl = byId<HTMLSpanElement>('deployment-badge');
const deploymentStatusEl = byId<HTMLParagraphElement>('deployment-status');
const modelListEl = byId<HTMLUListElement>('model-list');

function setBanner(message = '') {
  bannerEl.hidden = !message;
  bannerEl.textContent = message;
}

function setBadge(node: HTMLElement, value: string) {
  node.textContent = String(value || 'idle');
}

function setProgress(progress: number) {
  const normalized = Math.max(0, Math.min(100, Math.round(Number(progress || 0))));
  pipelineFillEl.style.width = `${normalized}%`;
  pipelineProgressLabelEl.textContent = `${normalized}%`;
}

function formatDate(value: string) {
  const date = new Date(String(value || ''));
  if (Number.isNaN(date.getTime())) return 'unknown time';
  return date.toLocaleString();
}

function formatBytes(value: number) {
  const size = Number(value || 0);
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDuration(ms: number) {
  const total = Math.max(0, Math.round(Number(ms || 0) / 1000));
  if (total < 60) return `${total}s`;
  return `${Math.floor(total / 60)}m ${String(total % 60).padStart(2, '0')}s`;
}

async function loadJSON(path: string, init?: RequestInit) {
  const resp = await fetch(apiURL(path), { cache: 'no-store', ...init });
  const payload = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(String(payload?.error || `HTTP ${resp.status}`));
  }
  return payload;
}

function selectedGenerationModels() {
  return Array.from(document.querySelectorAll<HTMLInputElement>('input[name="generator-model"]:checked'))
    .map((node) => node.value);
}

function configuredGeneratorCommands() {
  return {
    qwen3tts: qwenCommandEl.value.trim(),
    gptsovits: gptSovitsCommandEl.value.trim(),
    kokoro: kokoroCommandEl.value.trim(),
    piper: piperCommandEl.value.trim(),
  };
}

function configPayloadFromForm(): Settings {
  const negativePhrases = trainerNegativePhrasesEl.value
    .split('\n')
    .map((value) => value.trim())
    .filter(Boolean);
  return {
    preferred_generator: preferredGeneratorEl.value,
    sample_count: Math.max(250, Number(trainerSampleCountEl.value || 2000)),
    auto_deploy: trainerAutoDeployEl.checked,
    negative_phrases: negativePhrases,
    generator_commands: configuredGeneratorCommands(),
  };
}

function generationCheckbox(id: string) {
  return document.querySelector<HTMLInputElement>(`input[name="generator-model"][value="${id}"]`);
}

function renderDatasetSummary(summary: DatasetSummary) {
  datasetSummaryEl.replaceChildren();
  const cards = [
    ['Real positives', `${summary.hotword_clips || 0} hotword clips`],
    ['Reference voice', `${summary.reference_clips || 0} prompt clips`],
    ['Retry loop', `${summary.test_clips || 0} test clips`],
    ['Synthetic', `${summary.generated_samples || 0} generated samples`],
    ['Latest model', String(summary.latest_model || 'none yet')],
    ['Production', String(summary.production_model || 'not deployed')],
  ];
  for (const [title, detail] of cards) {
    const card = document.createElement('div');
    card.className = 'train-summary-card';
    card.innerHTML = `<strong>${title}</strong><span>${detail}</span>`;
    datasetSummaryEl.appendChild(card);
  }
}

function renderGeneratorInfos(generators: GeneratorInfo[]) {
  generatorStatusListEl.replaceChildren();
  for (const generator of generators) {
    const item = document.createElement('div');
    item.className = 'train-generator-item';
    item.innerHTML = `<strong>${generator.label}</strong><span>${generator.available ? 'Ready' : 'Missing'}${generator.recommended ? ' · recommended' : ''}</span><code>${generator.command || 'not configured'}</code><span>${generator.message || ''}</span>`;
    generatorStatusListEl.appendChild(item);
    const checkbox = generationCheckbox(generator.id);
    if (checkbox) {
      checkbox.disabled = !generator.available;
      if (generator.available && generator.recommended && generator.id !== 'piper') {
        checkbox.checked = true;
      }
      if (!generator.available && generator.id !== 'piper') {
        checkbox.checked = false;
      }
    }
  }
}

function renderTrainConfig(config: TrainUIConfig) {
  state.trainConfig = config;
  preferredGeneratorEl.value = String(config.settings?.preferred_generator || 'qwen3tts');
  trainerSampleCountEl.value = String(Number(config.settings?.sample_count || 2000));
  trainerAutoDeployEl.checked = Boolean(config.settings?.auto_deploy);
  trainerNegativePhrasesEl.value = Array.isArray(config.settings?.negative_phrases)
    ? config.settings.negative_phrases.join('\n')
    : '';
  qwenCommandEl.value = String(config.settings?.generator_commands?.qwen3tts || config.generators.find((item) => item.id === 'qwen3tts')?.command || '');
  gptSovitsCommandEl.value = String(config.settings?.generator_commands?.gptsovits || config.generators.find((item) => item.id === 'gptsovits')?.command || '');
  kokoroCommandEl.value = String(config.settings?.generator_commands?.kokoro || config.generators.find((item) => item.id === 'kokoro')?.command || '');
  piperCommandEl.value = String(config.settings?.generator_commands?.piper || config.generators.find((item) => item.id === 'piper')?.command || '');
  generationCountEl.value = String(Math.max(1, Number(config.settings?.sample_count || 250)));
  renderDatasetSummary(config.dataset || {} as DatasetSummary);
  renderGeneratorInfos(Array.isArray(config.generators) ? config.generators : []);
}

function preferredPipelineModels() {
  const config = state.trainConfig;
  if (!config) return [];
  const generatorMap = new Map((config.generators || []).map((item) => [item.id, item]));
  const preferred = String(config.settings?.preferred_generator || 'qwen3tts').trim();
  if (preferred && preferred !== 'piper' && generatorMap.get(preferred)?.available) {
    return [preferred];
  }
  return [];
}

function renderRecordings(recordings: Recording[]) {
  recordingListEl.replaceChildren();
  if (!Array.isArray(recordings) || recordings.length === 0) {
    const empty = document.createElement('li');
    empty.className = 'train-list-item';
    empty.textContent = 'No recordings yet.';
    recordingListEl.appendChild(empty);
    return;
  }
  for (const recording of recordings) {
    const item = document.createElement('li');
    item.className = 'train-list-item';
    const title = document.createElement('div');
    title.className = 'train-list-head';
    title.innerHTML = `<strong>${recording.kind}</strong><span>${formatDate(recording.created_at)}</span>`;
    const meta = document.createElement('p');
    meta.className = 'train-list-meta';
    meta.textContent = `${recording.file_name} | ${formatDuration(recording.duration_ms)} | ${formatBytes(recording.size_bytes)}`;
    const audio = document.createElement('audio');
    audio.controls = true;
    audio.src = appURL(recording.audio_url);
    const actions = document.createElement('div');
    actions.className = 'train-list-actions';
    const remove = document.createElement('button');
    remove.className = 'train-list-button';
    remove.type = 'button';
    remove.textContent = 'Delete';
    remove.addEventListener('click', async () => {
      remove.disabled = true;
      try {
        await fetch(apiURL(`hotword/train/recordings/${encodeURIComponent(recording.id)}`), { method: 'DELETE' });
        await Promise.all([refreshRecordings(), refreshTrainConfig()]);
      } catch (err: any) {
        setBanner(String(err?.message || err || 'delete failed'));
      } finally {
        remove.disabled = false;
      }
    });
    actions.appendChild(remove);
    item.append(title, meta, audio, actions);
    recordingListEl.appendChild(item);
  }
}

function feedbackForRecording(recordingID: string) {
  return state.feedback.filter((entry) => entry.recording_id === recordingID);
}

async function submitFeedback(recordingID: string, outcome: string) {
  const payload = await loadJSON('hotword/train/feedback', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      recording_id: recordingID,
      outcome,
    }),
  });
  testingStatusEl.textContent = outcome === 'missed_trigger'
    ? 'Marked clip as a missed trigger for the next round.'
    : 'Marked clip as a false trigger for the next round.';
  setBadge(testingBadgeEl, 'saved');
  renderFeedbackSummary(payload?.summary || {});
  await Promise.all([refreshFeedback(), refreshTrainConfig()]);
}

function renderFeedbackSummary(summary: FeedbackSummary) {
  const missed = Number(summary?.missed_triggers || 0);
  const falseTriggers = Number(summary?.false_triggers || 0);
  const total = Number(summary?.total || 0);
  if (total === 0) {
    feedbackStatusEl.textContent = 'No retry feedback captured yet.';
    return;
  }
  feedbackStatusEl.textContent = `${missed} missed-trigger clip(s), ${falseTriggers} false-trigger clip(s) saved for the next guided training round.`;
}

function renderTestingList() {
  testingListEl.replaceChildren();
  const recordings = state.recordings.filter((recording) => recording.kind === 'test');
  if (recordings.length === 0) {
    const empty = document.createElement('li');
    empty.className = 'train-list-item';
    empty.textContent = 'No test clips yet. Upload one here or record a Test retry clip in Step 1.';
    testingListEl.appendChild(empty);
    setBadge(testingBadgeEl, 'idle');
    if (!feedbackStatusEl.textContent) {
      feedbackStatusEl.textContent = 'No retry feedback captured yet.';
    }
    return;
  }
  setBadge(testingBadgeEl, state.feedback.length > 0 ? 'reviewing' : 'ready');
  for (const recording of recordings) {
    const item = document.createElement('li');
    item.className = 'train-list-item';
    const title = document.createElement('div');
    title.className = 'train-list-head';
    title.innerHTML = `<strong>${recording.file_name}</strong><span>${formatDate(recording.created_at)}</span>`;
    const meta = document.createElement('p');
    meta.className = 'train-list-meta';
    const feedbackEntries = feedbackForRecording(recording.id);
    const feedbackLabel = feedbackEntries.length > 0
      ? ` | feedback: ${feedbackEntries.map((entry) => entry.outcome.replace(/_/g, ' ')).join(', ')}`
      : '';
    meta.textContent = `${formatDuration(recording.duration_ms)} | ${formatBytes(recording.size_bytes)}${feedbackLabel}`;
    const audio = document.createElement('audio');
    audio.controls = true;
    audio.src = appURL(recording.audio_url);
    const actions = document.createElement('div');
    actions.className = 'train-list-actions';
    const missed = document.createElement('button');
    missed.className = 'train-list-button';
    missed.type = 'button';
    missed.textContent = 'This should have triggered';
    missed.addEventListener('click', async () => {
      missed.disabled = true;
      falseTrigger.disabled = true;
      try {
        await submitFeedback(recording.id, 'missed_trigger');
      } catch (err: any) {
        testingStatusEl.textContent = String(err?.message || err || 'feedback failed');
        setBadge(testingBadgeEl, 'error');
      } finally {
        missed.disabled = false;
        falseTrigger.disabled = false;
      }
    });
    const falseTrigger = document.createElement('button');
    falseTrigger.className = 'train-list-button';
    falseTrigger.type = 'button';
    falseTrigger.textContent = 'This was a false trigger';
    falseTrigger.addEventListener('click', async () => {
      missed.disabled = true;
      falseTrigger.disabled = true;
      try {
        await submitFeedback(recording.id, 'false_trigger');
      } catch (err: any) {
        testingStatusEl.textContent = String(err?.message || err || 'feedback failed');
        setBadge(testingBadgeEl, 'error');
      } finally {
        missed.disabled = false;
        falseTrigger.disabled = false;
      }
    });
    actions.append(missed, falseTrigger);
    item.append(title, meta, audio, actions);
    testingListEl.appendChild(item);
  }
}

function renderGenerationStatus(status: StatusPayload) {
  setBadge(generationBadgeEl, status.state || 'idle');
  generationStatusEl.textContent = String(status.error || status.message || 'Idle.');
  generationListEl.replaceChildren();
  const models = Array.isArray(status.models) ? status.models : [];
  if (models.length === 0) {
    const empty = document.createElement('li');
    empty.className = 'train-list-item';
    empty.textContent = 'No generation job has started yet.';
    generationListEl.appendChild(empty);
    return;
  }
  for (const model of models) {
    const item = document.createElement('li');
    item.className = 'train-list-item';
    item.innerHTML = `
      <div class="train-list-head"><strong>${model.name}</strong><span>${model.state}</span></div>
      <p class="train-list-meta">${model.count || 0}/${model.target || 0} samples${model.output_dir ? ` | ${model.output_dir}` : ''}</p>
      <p class="train-list-meta">${model.message || 'Waiting.'}</p>
    `;
    generationListEl.appendChild(item);
  }
}

function syncTrainingStatus(status: StatusPayload) {
  const label = String(status.error || status.message || 'Idle.');
  setBadge(trainingBadgeEl, status.state || 'idle');
  trainingStatusEl.textContent = label;
  setBadge(pipelineBadgeEl, status.state || 'idle');
  pipelineStatusEl.textContent = label;
  setProgress(Number(status.progress || 0));
}

function renderModels(models: Model[]) {
  modelListEl.replaceChildren();
  if (!Array.isArray(models) || models.length === 0) {
    const empty = document.createElement('li');
    empty.className = 'train-list-item';
    empty.textContent = 'No trained models yet.';
    modelListEl.appendChild(empty);
    return;
  }
  for (const model of models) {
    const item = document.createElement('li');
    item.className = 'train-list-item';
    const head = document.createElement('div');
    head.className = 'train-list-head';
    head.innerHTML = `<strong>${model.file_name}</strong><span>${model.production ? 'production' : 'trained'}</span>`;
    const meta = document.createElement('p');
    meta.className = 'train-list-meta';
    meta.textContent = `${formatDate(model.created_at)} | ${formatBytes(model.size_bytes)} | ${model.path}`;
    item.append(head, meta);
    if (!model.production) {
      const actions = document.createElement('div');
      actions.className = 'train-list-actions';
      const deploy = document.createElement('button');
      deploy.className = 'train-list-button';
      deploy.type = 'button';
      deploy.textContent = 'Deploy';
      deploy.addEventListener('click', async () => {
        deploy.disabled = true;
        try {
          const payload = await loadJSON('hotword/train/deploy', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ model: model.file_name }),
          });
          const revision = String(payload?.hotword_status?.model?.revision || '').trim();
          deploymentStatusEl.textContent = revision
            ? `Deployed ${payload?.model?.file_name || model.file_name}. Connected clients will reload revision ${revision}.`
            : `Deployed ${payload?.model?.file_name || model.file_name}.`;
          setBadge(deploymentBadgeEl, 'deployed');
          await Promise.all([refreshModels(), refreshTrainConfig()]);
        } catch (err: any) {
          deploymentStatusEl.textContent = String(err?.message || err || 'deploy failed');
          setBadge(deploymentBadgeEl, 'error');
        } finally {
          deploy.disabled = false;
        }
      });
      actions.appendChild(deploy);
      item.appendChild(actions);
    }
    modelListEl.appendChild(item);
  }
}

async function refreshRecordings() {
  const payload = await loadJSON('hotword/train/recordings');
  state.recordings = Array.isArray(payload?.recordings) ? payload.recordings as Recording[] : [];
  renderRecordings(state.recordings);
  renderTestingList();
  recordingStatusEl.textContent = state.recordings.length > 0
    ? `${state.recordings.filter((item) => item.kind === 'hotword').length} hotword clip(s), ${state.recordings.filter((item) => item.kind === 'reference').length} reference clip(s), ${state.recordings.filter((item) => item.kind === 'test').length} test clip(s).`
    : 'Record or upload a WAV file to start.';
}

async function refreshModels() {
  const payload = await loadJSON('hotword/train/models');
  renderModels(Array.isArray(payload?.models) ? payload.models as Model[] : []);
}

async function refreshFeedback() {
  const payload = await loadJSON('hotword/train/feedback');
  state.feedback = Array.isArray(payload?.feedback) ? payload.feedback as Feedback[] : [];
  renderFeedbackSummary(payload?.summary || {});
  renderTestingList();
}

async function refreshTrainConfig() {
  const payload = await loadJSON('hotword/train/config');
  renderTrainConfig(payload?.config || {} as TrainUIConfig);
}

function mergeChunks() {
  const total = state.chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const merged = new Float32Array(total);
  let offset = 0;
  for (const chunk of state.chunks) {
    merged.set(chunk, offset);
    offset += chunk.length;
  }
  return merged;
}

async function uploadBlob(blob: Blob, kind: string) {
  const form = new FormData();
  form.append('kind', kind);
  form.append('file', blob, `${kind}-${Date.now()}.wav`);
  const resp = await fetch(apiURL('hotword/train/recordings'), {
    method: 'POST',
    body: form,
  });
  const payload = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(String(payload?.error || `HTTP ${resp.status}`));
  }
  return payload;
}

async function startRecording() {
  if (state.recordingActive) return;
  const AudioContextCtor = window.AudioContext || (window as any).webkitAudioContext;
  if (!AudioContextCtor) {
    throw new Error('AudioContext is unavailable in this browser');
  }
  const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  const audioContext = new AudioContextCtor();
  const sourceNode = audioContext.createMediaStreamSource(stream);
  const processorNode = audioContext.createScriptProcessor(4096, 1, 1);
  const sinkNode = audioContext.createGain();
  sinkNode.gain.value = 0;
  state.audioContext = audioContext;
  state.sourceNode = sourceNode;
  state.processorNode = processorNode;
  state.sinkNode = sinkNode;
  state.stream = stream;
  state.sampleRate = Number(audioContext.sampleRate) || 16000;
  state.chunks = [];
  processorNode.onaudioprocess = (event) => {
    const input = event.inputBuffer?.getChannelData?.(0);
    if (!(input instanceof Float32Array) || input.length === 0) return;
    const copy = new Float32Array(input.length);
    copy.set(input);
    state.chunks.push(copy);
  };
  sourceNode.connect(processorNode);
  processorNode.connect(sinkNode);
  sinkNode.connect(audioContext.destination);
  state.recordingActive = true;
  setBadge(recordingBadgeEl, 'recording');
  recordingToggleEl.textContent = 'Stop recording';
  recordingStatusEl.textContent = 'Recording... say the hotword or reference speech, then stop.';
}

async function stopRecording() {
  if (!state.recordingActive) return;
  const samples = mergeChunks();
  state.recordingActive = false;
  try {
    state.processorNode?.disconnect();
    state.sourceNode?.disconnect();
    state.sinkNode?.disconnect();
    state.stream?.getTracks().forEach((track) => track.stop());
    await state.audioContext?.close();
  } catch (_) {}
  state.processorNode = null;
  state.sourceNode = null;
  state.sinkNode = null;
  state.stream = null;
  state.audioContext = null;
  state.chunks = [];
  setBadge(recordingBadgeEl, 'uploading');
  recordingToggleEl.textContent = 'Start recording';
  if (samples.length === 0) {
    recordingStatusEl.textContent = 'No audio captured.';
    setBadge(recordingBadgeEl, 'idle');
    return;
  }
  const blob = float32ToWav(samples, state.sampleRate);
  await uploadBlob(blob, recordingKindEl.value);
  recordingStatusEl.textContent = 'Recording uploaded.';
  setBadge(recordingBadgeEl, 'saved');
  await Promise.all([refreshRecordings(), refreshTrainConfig()]);
}

async function connectStatusStream(path: string, render: (status: StatusPayload) => void) {
  const source = new EventSource(apiURL(path));
  source.addEventListener('status', (event) => {
    try {
      render(JSON.parse((event as MessageEvent<string>).data));
    } catch (_) {}
  });
  source.onerror = () => {
    source.close();
    window.setTimeout(() => {
      void connectStatusStream(path, render);
    }, 1200);
  };
}

async function saveTrainerConfig() {
  const payload = await loadJSON('hotword/train/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(configPayloadFromForm()),
  });
  renderTrainConfig(payload?.config || {} as TrainUIConfig);
  pipelineStatusEl.textContent = 'Trainer settings saved.';
}

async function bootstrap() {
  recordingToggleEl.addEventListener('click', async () => {
    try {
      if (state.recordingActive) {
        await stopRecording();
      } else {
        await startRecording();
      }
    } catch (err: any) {
      setBadge(recordingBadgeEl, 'error');
      recordingStatusEl.textContent = String(err?.message || err || 'recording failed');
    }
  });

  recordingUploadEl.addEventListener('change', async () => {
    const file = recordingUploadEl.files?.[0];
    recordingUploadEl.value = '';
    if (!(file instanceof File)) return;
    try {
      setBadge(recordingBadgeEl, 'uploading');
      await uploadBlob(file, recordingKindEl.value);
      setBadge(recordingBadgeEl, 'saved');
      recordingStatusEl.textContent = `Uploaded ${file.name}.`;
      await Promise.all([refreshRecordings(), refreshTrainConfig()]);
    } catch (err: any) {
      setBadge(recordingBadgeEl, 'error');
      recordingStatusEl.textContent = String(err?.message || err || 'upload failed');
    }
  });

  testingUploadEl.addEventListener('change', async () => {
    const file = testingUploadEl.files?.[0];
    testingUploadEl.value = '';
    if (!(file instanceof File)) return;
    try {
      setBadge(testingBadgeEl, 'uploading');
      await uploadBlob(file, 'test');
      testingStatusEl.textContent = `Uploaded ${file.name}.`;
      setBadge(testingBadgeEl, 'saved');
      await Promise.all([refreshRecordings(), refreshTrainConfig()]);
    } catch (err: any) {
      setBadge(testingBadgeEl, 'error');
      testingStatusEl.textContent = String(err?.message || err || 'test upload failed');
    }
  });

  generationStartEl.addEventListener('click', async () => {
    const models = selectedGenerationModels();
    if (models.length === 0) {
      generationStatusEl.textContent = 'Select at least one generator.';
      return;
    }
    generationStartEl.disabled = true;
    try {
      const payload = await loadJSON('hotword/train/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          models,
          sample_count: Number(generationCountEl.value || 250),
        }),
      });
      renderGenerationStatus(payload?.status || {});
    } catch (err: any) {
      generationStatusEl.textContent = String(err?.message || err || 'generation failed');
      setBadge(generationBadgeEl, 'error');
    } finally {
      generationStartEl.disabled = false;
    }
  });

  trainingStartEl.addEventListener('click', async () => {
    trainingStartEl.disabled = true;
    try {
      const payload = await loadJSON('hotword/train/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(configPayloadFromForm()),
      });
      syncTrainingStatus(payload?.status || {});
    } catch (err: any) {
      trainingStatusEl.textContent = String(err?.message || err || 'training failed');
      setBadge(trainingBadgeEl, 'error');
    } finally {
      trainingStartEl.disabled = false;
    }
  });

  configSaveEl.addEventListener('click', async () => {
    configSaveEl.disabled = true;
    try {
      await saveTrainerConfig();
    } catch (err: any) {
      pipelineStatusEl.textContent = String(err?.message || err || 'save failed');
      setBadge(pipelineBadgeEl, 'error');
    } finally {
      configSaveEl.disabled = false;
    }
  });

  pipelineStartEl.addEventListener('click', async () => {
    pipelineStartEl.disabled = true;
    try {
      await saveTrainerConfig();
      const payload = await loadJSON('hotword/train/pipeline', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          models: preferredPipelineModels(),
        }),
      });
      syncTrainingStatus(payload?.status || {});
    } catch (err: any) {
      pipelineStatusEl.textContent = String(err?.message || err || 'pipeline failed');
      setBadge(pipelineBadgeEl, 'error');
    } finally {
      pipelineStartEl.disabled = false;
    }
  });

  try {
    const hotwordStatus = await loadJSON('hotword/status');
    if (hotwordStatus?.ready === false) {
      setBanner('Wake word assets are not fully deployed yet. Use the guided trainer here to record, clone, train, and deploy a better Sloppy model.');
    }
  } catch (_) {}

  await Promise.all([refreshRecordings(), refreshModels(), refreshFeedback(), refreshTrainConfig()]);
  void connectStatusStream('hotword/train/generate/status', async (status) => {
    renderGenerationStatus(status);
    if (status.state === 'completed' || status.state === 'failed') {
      await refreshTrainConfig();
    }
  });
  void connectStatusStream('hotword/train/status', async (status) => {
    syncTrainingStatus(status);
    if (status.latest_model || status.state === 'completed' || status.state === 'failed') {
      await Promise.all([refreshModels(), refreshTrainConfig()]);
    }
  });
}

void bootstrap();
