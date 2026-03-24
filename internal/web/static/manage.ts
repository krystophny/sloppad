import { apiURL, appURL } from './paths.js';

type RuntimePayload = Record<string, any>;
type HotwordStatusPayload = Record<string, any>;

type Model = {
  display_name?: string;
  phrase?: string;
  source?: string;
  file_name: string;
  created_at: string;
  size_bytes: number;
  production: boolean;
};

type CatalogEntry = {
  key: string;
  display_name: string;
  phrase: string;
  source: string;
  source_label: string;
  source_url: string;
  readme_url?: string;
  download_url: string;
  upstream_file: string;
  installed: boolean;
  installed_model?: Model;
  active: boolean;
};

const state = {
  runtime: null as RuntimePayload | null,
  hotword: null as HotwordStatusPayload | null,
  catalog: [] as CatalogEntry[],
  hotwordActionInFlight: false,
};

function activeSection() {
  const path = window.location.pathname.replace(/\/+$/, '');
  if (path.endsWith('/manage/hotword')) return 'hotword';
  if (path.endsWith('/manage/models')) return 'models';
  if (path.endsWith('/manage/voices')) return 'voices';
  return 'manage';
}

function setActiveNavigation(section: string) {
  document.querySelectorAll('[data-manage-link]').forEach((node) => {
    if (!(node instanceof HTMLAnchorElement)) return;
    const active = node.dataset.manageLink === section;
    node.setAttribute('aria-current', active ? 'page' : 'false');
  });
  document.querySelectorAll('[data-manage-section]').forEach((node) => {
    if (!(node instanceof HTMLElement)) return;
    node.hidden = node.dataset.manageSection !== section;
  });
}

function card(label: string, value: string) {
  const node = document.createElement('article');
  node.className = 'manage-card';
  node.innerHTML = `
    <p class="manage-card-label">${label}</p>
    <p class="manage-card-value">${value}</p>
  `;
  return node;
}

function row(title: string, detail: string, actionLabel = '', actionHref = '') {
  const node = document.createElement('div');
  node.className = 'manage-row';
  const info = document.createElement('div');
  info.innerHTML = `<strong>${title}</strong><span>${detail}</span>`;
  node.appendChild(info);
  if (actionLabel && actionHref) {
    const link = document.createElement('a');
    link.className = 'manage-link';
    link.href = actionHref;
    link.textContent = actionLabel;
    node.appendChild(link);
  }
  return node;
}

function byId<T extends HTMLElement>(id: string) {
  const node = document.getElementById(id);
  if (!(node instanceof HTMLElement)) {
    throw new Error(`missing element: ${id}`);
  }
  return node as T;
}

const hotwordFilterEl = byId<HTMLInputElement>('manage-hotword-filter');
const hotwordFeedbackEl = byId<HTMLParagraphElement>('manage-hotword-feedback');

function setHotwordFeedback(message = '', tone = '') {
  hotwordFeedbackEl.hidden = !message;
  hotwordFeedbackEl.textContent = message;
  hotwordFeedbackEl.dataset.tone = tone;
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

async function loadJSON(path: string, init?: RequestInit) {
  const resp = await fetch(apiURL(path), { cache: 'no-store', ...init });
  const payload = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(String(payload?.error || `HTTP ${resp.status}`));
  }
  return payload;
}

async function loadData() {
  const [runtimeResp, hotwordResp, catalogResp] = await Promise.all([
    fetch(apiURL('runtime'), { cache: 'no-store' }),
    fetch(apiURL('hotword/status'), { cache: 'no-store' }),
    fetch(apiURL('hotword/catalog'), { cache: 'no-store' }),
  ]);
  if (!runtimeResp.ok) {
    throw new Error(`runtime metadata failed: HTTP ${runtimeResp.status}`);
  }
  if (!hotwordResp.ok) {
    throw new Error(`hotword status failed: HTTP ${hotwordResp.status}`);
  }
  if (!catalogResp.ok) {
    throw new Error(`hotword catalog failed: HTTP ${catalogResp.status}`);
  }
  return {
    runtime: await runtimeResp.json(),
    hotword: await hotwordResp.json(),
    catalog: await catalogResp.json(),
  };
}

function renderDashboard(runtime: Record<string, any>, hotword: Record<string, any>) {
  const host = document.getElementById('manage-dashboard-cards');
  if (!(host instanceof HTMLElement)) return;
  const activeName = String(hotword?.model?.display_name || hotword?.model?.file || 'unknown');
  host.replaceChildren(
    card('Version', String(runtime?.version || 'unknown')),
    card('Live policy', String(runtime?.live_policy || 'manual')),
    card('Silent mode', runtime?.silent_mode ? 'On' : 'Off'),
    card('Hotword', activeName),
  );
}

function renderHotwordStatus(hotword: Record<string, any>) {
  const host = document.getElementById('manage-hotword-status');
  if (!(host instanceof HTMLElement)) return;
  const missing = Array.isArray(hotword?.missing) && hotword.missing.length
    ? hotword.missing.join(', ')
    : 'All runtime assets are present.';
  const activeName = String(hotword?.model?.display_name || hotword?.model?.file || 'Unknown');
  const phrase = String(hotword?.model?.phrase || '').trim();
  const source = String(hotword?.model?.source || '').trim();
  const modelFile = String(hotword?.model?.file || 'keyword.onnx');
  const summary = [phrase, source].filter(Boolean).join(' · ');
  host.replaceChildren(
    row('Runtime status', hotword?.ready ? 'The browser hotword runtime can start immediately.' : `Missing: ${missing}`),
    row('Active wake word', summary ? `${activeName} · ${summary}` : activeName, 'Open hotword test', appURL('static/hotword-test.html')),
    row('Runtime file', modelFile),
    row('Training entry', 'Use the trainer for custom recordings and model creation.', 'Open trainer', appURL('hotword-train')),
  );
}

function renderCatalog(entries: CatalogEntry[]) {
  const host = document.getElementById('manage-hotword-catalog');
  if (!(host instanceof HTMLElement)) return;
  host.replaceChildren();

  const filter = hotwordFilterEl.value.trim().toLowerCase();
  const filtered = entries.filter((entry) => {
    if (!filter) return true;
    const haystack = [
      entry.display_name,
      entry.phrase,
      entry.source_label,
      entry.upstream_file,
    ].join(' ').toLowerCase();
    return haystack.includes(filter);
  });
  if (filtered.length === 0) {
    host.appendChild(row('No matches', 'No wake words matched the current search.'));
    return;
  }

  const grouped = new Map<string, CatalogEntry[]>();
  for (const entry of filtered) {
    const list = grouped.get(entry.source_label) || [];
    list.push(entry);
    grouped.set(entry.source_label, list);
  }

  for (const [sourceLabel, group] of grouped) {
    const section = document.createElement('section');
    section.className = 'manage-group';
    const heading = document.createElement('h3');
    heading.textContent = `${sourceLabel} (${group.length})`;
    section.appendChild(heading);

    for (const entry of group) {
      const node = document.createElement('div');
      node.className = 'manage-row';
      const meta = document.createElement('div');
      meta.className = 'manage-row-meta';
      const installed = entry.installed && entry.installed_model
        ? `Downloaded ${formatDate(entry.installed_model.created_at)} · ${formatBytes(entry.installed_model.size_bytes)}`
        : 'Not downloaded yet.';
      meta.innerHTML = `
        <strong>${entry.display_name}</strong>
        <span>${entry.phrase}${entry.active ? ' · active' : ''}</span>
        <span>${installed}</span>
      `;
      node.appendChild(meta);

      const actions = document.createElement('div');
      actions.className = 'manage-row-actions';
      if (entry.readme_url) {
        const readme = document.createElement('a');
        readme.className = 'manage-link';
        readme.href = entry.readme_url;
        readme.target = '_blank';
        readme.rel = 'noreferrer';
        readme.textContent = 'Readme';
        actions.appendChild(readme);
      }
      if (entry.active) {
        const active = document.createElement('button');
        active.className = 'manage-button is-active';
        active.type = 'button';
        active.disabled = true;
        active.textContent = 'Active';
        actions.appendChild(active);
      } else if (entry.installed && entry.installed_model) {
        const activate = document.createElement('button');
        activate.className = 'manage-button is-secondary';
        activate.type = 'button';
        activate.disabled = state.hotwordActionInFlight;
        activate.textContent = 'Use';
        activate.addEventListener('click', () => void activateCatalogModel(entry));
        actions.appendChild(activate);
      } else {
        const download = document.createElement('button');
        download.className = 'manage-button';
        download.type = 'button';
        download.disabled = state.hotwordActionInFlight;
        download.textContent = 'Download';
        download.addEventListener('click', () => void downloadCatalogModel(entry));
        actions.appendChild(download);
      }
      node.appendChild(actions);
      section.appendChild(node);
    }
    host.appendChild(section);
  }
}

function renderModels(runtime: Record<string, any>) {
  const host = document.getElementById('manage-models-status');
  if (!(host instanceof HTMLElement)) return;
  const efforts = Array.isArray(runtime?.available_reasoning_efforts)
    ? runtime.available_reasoning_efforts.join(', ')
    : 'low, medium, high, xhigh';
  host.replaceChildren(
    row('Runtime version', String(runtime?.version || 'unknown')),
    row('Live policy', String(runtime?.live_policy || 'dialogue')),
    row('Reasoning efforts', efforts),
  );
}

function renderVoices(runtime: Record<string, any>) {
  const host = document.getElementById('manage-voices-status');
  if (!(host instanceof HTMLElement)) return;
  host.replaceChildren(
    row('TTS service', runtime?.tts_enabled ? 'Enabled' : 'Disabled'),
    row('Voice output', runtime?.silent_mode ? 'Silent mode is active.' : 'Voice playback is active.'),
    row('Hotword monitor', 'Use the hotword page to inspect wake-word readiness and open the current browser-side test harness.', 'Hotword tools', appURL('manage/hotword')),
  );
}

function renderAll() {
  if (!state.runtime || !state.hotword) return;
  renderDashboard(state.runtime, state.hotword);
  renderHotwordStatus(state.hotword);
  renderCatalog(state.catalog);
  renderModels(state.runtime);
  renderVoices(state.runtime);
}

async function refreshManage() {
  const payload = await loadData();
  state.runtime = payload.runtime;
  state.hotword = payload.hotword;
  state.catalog = Array.isArray(payload.catalog?.catalog) ? payload.catalog.catalog as CatalogEntry[] : [];
  renderAll();
}

async function downloadCatalogModel(entry: CatalogEntry) {
  state.hotwordActionInFlight = true;
  setHotwordFeedback(`Downloading ${entry.display_name}...`);
  renderCatalog(state.catalog);
  try {
    await loadJSON('hotword/catalog/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: entry.key }),
    });
    setHotwordFeedback(`Downloaded ${entry.display_name}.`);
    await refreshManage();
  } catch (err) {
    setHotwordFeedback(String((err as any)?.message || err || 'download failed'), 'error');
  } finally {
    state.hotwordActionInFlight = false;
    renderCatalog(state.catalog);
  }
}

async function activateCatalogModel(entry: CatalogEntry) {
  const fileName = String(entry.installed_model?.file_name || '').trim();
  if (!fileName) return;
  state.hotwordActionInFlight = true;
  setHotwordFeedback(`Activating ${entry.display_name}...`);
  renderCatalog(state.catalog);
  try {
    const payload = await loadJSON('hotword/train/deploy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: fileName }),
    });
    const revision = String(payload?.hotword_status?.model?.revision || '').trim();
    setHotwordFeedback(revision
      ? `Activated ${entry.display_name}. Clients will reload revision ${revision}.`
      : `Activated ${entry.display_name}.`);
    await refreshManage();
  } catch (err) {
    setHotwordFeedback(String((err as any)?.message || err || 'activation failed'), 'error');
  } finally {
    state.hotwordActionInFlight = false;
    renderCatalog(state.catalog);
  }
}

async function bootstrapManage() {
  const section = activeSection();
  setActiveNavigation(section);
  hotwordFilterEl.addEventListener('input', () => renderCatalog(state.catalog));
  try {
    await refreshManage();
  } catch (err) {
    const message = String((err as any)?.message || err || 'unknown error');
    const host = document.getElementById(`manage-${section === 'manage' ? 'dashboard-cards' : `${section}-status`}`);
    if (host instanceof HTMLElement) {
      host.replaceChildren(row('Load failed', message));
    }
  }
}

void bootstrapManage();
