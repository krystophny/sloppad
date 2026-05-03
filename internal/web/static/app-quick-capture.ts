import { apiURL } from './app-env.js';

interface QuickCaptureNodes {
  toggle: HTMLButtonElement;
  sheet: HTMLElement;
  form: HTMLFormElement;
  title: HTMLInputElement;
  kindRadios: NodeListOf<HTMLInputElement>;
  sphere: HTMLSelectElement;
  label: HTMLInputElement;
  projectItemID: HTMLInputElement;
  submit: HTMLButtonElement;
  status: HTMLElement;
  error: HTMLElement;
}

interface QuickCaptureState {
  open: boolean;
  busy: boolean;
}

const state: QuickCaptureState = {
  open: false,
  busy: false,
};

let nodes: QuickCaptureNodes | null = null;

export function initQuickCapture(): void {
  const toggle = document.getElementById('quick-capture-toggle') as HTMLButtonElement | null;
  const sheet = document.getElementById('quick-capture-sheet');
  const form = document.getElementById('quick-capture-form') as HTMLFormElement | null;
  const title = document.getElementById('quick-capture-title') as HTMLInputElement | null;
  const sphere = document.getElementById('quick-capture-sphere') as HTMLSelectElement | null;
  const label = document.getElementById('quick-capture-label') as HTMLInputElement | null;
  const projectItemID = document.getElementById('quick-capture-project-item-id') as HTMLInputElement | null;
  const submit = document.getElementById('quick-capture-submit') as HTMLButtonElement | null;
  const status = document.getElementById('quick-capture-status');
  const error = document.getElementById('quick-capture-error');
  if (!toggle || !sheet || !form || !title || !sphere || !label || !projectItemID || !submit || !status || !error) {
    return;
  }
  const kindRadios = form.querySelectorAll<HTMLInputElement>('input[name="quick-capture-kind"]');
  nodes = { toggle, sheet, form, title, kindRadios, sphere, label, projectItemID, submit, status, error };
  toggle.addEventListener('click', () => toggleSheet());
  sheet.querySelectorAll<HTMLElement>('[data-quick-capture-dismiss="true"]').forEach((node) => {
    node.addEventListener('click', () => closeSheet());
  });
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    void submitCapture();
  });
  kindRadios.forEach((radio) => {
    radio.addEventListener('change', () => syncKindAttribute());
  });
  document.addEventListener('keydown', (event) => {
    if (state.open && event.key === 'Escape') {
      event.preventDefault();
      closeSheet();
    }
  });
  syncKindAttribute();
}

function toggleSheet(): void {
  if (state.open) {
    closeSheet();
    return;
  }
  openSheet();
}

function openSheet(): void {
  if (!nodes) return;
  state.open = true;
  nodes.sheet.hidden = false;
  nodes.toggle.setAttribute('aria-expanded', 'true');
  setError('');
  setStatus('', '');
  nodes.title.focus();
}

function closeSheet(): void {
  if (!nodes) return;
  state.open = false;
  nodes.sheet.hidden = true;
  nodes.toggle.setAttribute('aria-expanded', 'false');
}

function syncKindAttribute(): void {
  if (!nodes) return;
  nodes.form.dataset.kind = currentKind();
}

function currentKind(): string {
  if (!nodes) return 'action';
  for (const radio of Array.from(nodes.kindRadios)) {
    if (radio.checked) {
      return radio.value;
    }
  }
  return 'action';
}

function setError(message: string): void {
  if (!nodes) return;
  if (!message) {
    nodes.error.textContent = '';
    nodes.error.hidden = true;
    return;
  }
  nodes.error.textContent = message;
  nodes.error.hidden = false;
}

function setStatus(message: string, tone: string): void {
  if (!nodes) return;
  nodes.status.textContent = message;
  if (tone) {
    nodes.status.dataset.tone = tone;
  } else {
    delete nodes.status.dataset.tone;
  }
}

async function submitCapture(): Promise<void> {
  if (!nodes || state.busy) return;
  const title = nodes.title.value.trim();
  if (!title) {
    setError('Title is required.');
    nodes.title.focus();
    return;
  }
  const kind = currentKind();
  const payload: Record<string, unknown> = { title, kind };
  const sphere = nodes.sphere.value.trim();
  if (sphere) payload.sphere = sphere;
  const labelName = nodes.label.value.trim();
  if (labelName) payload.label = labelName;
  if (kind === 'action') {
    const projectRaw = nodes.projectItemID.value.trim();
    if (projectRaw) {
      const projectID = Number(projectRaw);
      if (!Number.isFinite(projectID) || projectID <= 0) {
        setError('Project id must be a positive integer.');
        return;
      }
      payload.project_item_id = projectID;
    }
  }
  state.busy = true;
  nodes.submit.disabled = true;
  setError('');
  setStatus('Saving...', '');
  try {
    const response = await fetch(apiURL('items/capture'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    let data: Record<string, any> = {};
    try {
      data = await response.json();
    } catch (_) {
      data = {};
    }
    if (!response.ok) {
      throw new Error(String(data && data.error ? data.error : `HTTP ${response.status}`));
    }
    const item = (data && data.item) || {};
    const itemID = Number(item && (item as any).id);
    nodes.form.reset();
    syncKindAttribute();
    setStatus(Number.isFinite(itemID) && itemID > 0 ? `Captured #${itemID} into inbox.` : 'Captured into inbox.', 'success');
    window.setTimeout(() => closeSheet(), 600);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setError(message || 'Capture failed.');
    setStatus('', '');
  } finally {
    state.busy = false;
    nodes.submit.disabled = false;
  }
}
