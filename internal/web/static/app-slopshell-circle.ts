import { TOOL_PALETTE_POSITION_STORAGE_KEY, refs, state } from './app-context.js';
import { appURL } from './paths.js';
import {
  SLOPSHELL_CIRCLE_CORNERS,
  SLOPSHELL_CIRCLE_LAYOUT,
  SLOPSHELL_CIRCLE_SEGMENTS,
  normalizeSlopshellCircleCorner,
  slopshellCircleToolIcon,
  slopshellCircleToolIconID,
  type SlopshellCircleCorner,
} from './slopshell-circle-contract.js';

let circleBound = false;
let circleExpanded = false;
let longPressTimer: number | null = null;
let longPressTriggered = false;
let layoutBound = false;

const selectInteractionTool = (...args) => refs.selectInteractionTool(...args);
const activateLiveSession = (...args) => refs.activateLiveSession(...args);
const deactivateLiveSession = (...args) => refs.deactivateLiveSession(...args);
const toggleTTSSilentMode = (...args) => refs.toggleTTSSilentMode(...args);
const toggleFastMode = (...args) => refs.toggleFastMode(...args);
const handleStopAction = (...args) => refs.handleStopAction(...args);
const showStatus = (...args) => refs.showStatus(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);

function circleRoot() {
  const node = document.getElementById('slopshell-circle');
  return node instanceof HTMLElement ? node : null;
}

function circleDot() {
  const node = document.getElementById('slopshell-circle-dot');
  return node instanceof HTMLButtonElement ? node : null;
}

function circleMenu() {
  const node = document.getElementById('slopshell-circle-menu');
  return node instanceof HTMLElement ? node : null;
}

function cornerControlsRoot() {
  const node = document.getElementById('slopshell-circle-corner-controls');
  return node instanceof HTMLElement ? node : null;
}

function currentSession() {
  if (!state.liveSessionActive) return 'none';
  const mode = String(state.liveSessionMode || '').trim().toLowerCase();
  return mode === 'meeting' ? 'meeting' : 'dialogue';
}

function sessionDisplayName(session: string) {
  if (session === 'dialogue') return 'Dialogue';
  if (session === 'meeting') return 'Meeting';
  return 'Manual';
}

function toolDisplayName(tool: string) {
  return String(tool || 'pointer').trim().replaceAll('_', ' ') || 'pointer';
}

function clearLongPressTimer() {
  if (longPressTimer !== null) {
    window.clearTimeout(longPressTimer);
    longPressTimer = null;
  }
}

function dotPositionForCorner(shellSize: number, dotSize: number, corner: SlopshellCircleCorner) {
  const top = corner.startsWith('top_') ? 0 : shellSize - dotSize;
  const left = corner.endsWith('_left') ? 0 : shellSize - dotSize;
  return { top, left };
}

function dotCenterForCorner(shellSize: number, dotSize: number, corner: SlopshellCircleCorner) {
  const dotPosition = dotPositionForCorner(shellSize, dotSize, corner);
  const half = dotSize / 2;
  return {
    x: dotPosition.left + half,
    y: dotPosition.top + half,
  };
}

function cornerSigns(corner: SlopshellCircleCorner) {
  return {
    x: corner.endsWith('_left') ? 1 : -1,
    y: corner.startsWith('top_') ? 1 : -1,
  };
}

function storageCorner() {
  try {
    return normalizeSlopshellCircleCorner(window.localStorage.getItem(TOOL_PALETTE_POSITION_STORAGE_KEY) || '');
  } catch (_) {
    return 'bottom_right';
  }
}

function currentCorner(): SlopshellCircleCorner {
  const current = normalizeSlopshellCircleCorner(String(state.toolPalettePosition || '').trim());
  if (current !== 'bottom_right' || String(state.toolPalettePosition || '').trim()) {
    return current;
  }
  const stored = storageCorner();
  state.toolPalettePosition = stored;
  return stored;
}

function persistCorner(next: SlopshellCircleCorner) {
  state.toolPalettePosition = next;
  try {
    window.localStorage.setItem(TOOL_PALETTE_POSITION_STORAGE_KEY, next);
  } catch (_) {}
}

function setCircleExpanded(next: boolean) {
  circleExpanded = Boolean(next);
  document.body.classList.toggle('slopshell-circle-expanded', circleExpanded);
  renderSlopshellCircle();
}

function toggleCircleExpanded() {
  setCircleExpanded(!circleExpanded);
}

export function collapseSlopshellCircle() {
  if (!circleExpanded) return;
  setCircleExpanded(false);
}

function handleOutsideCircleClick(event: MouseEvent) {
  if (!circleExpanded) return;
  const root = circleRoot();
  const target = event.target;
  if (!(root instanceof HTMLElement) || !(target instanceof Node)) return;
  if (root.contains(target)) return;
  collapseSlopshellCircle();
}

async function selectCircleTool(tool: string) {
  try {
    await selectInteractionTool(tool);
    showStatus(`${String(tool || '').replace('_', ' ')} tool on`);
  } catch (err) {
    showStatus(`tool switch failed: ${String(err?.message || err || 'unknown error')}`);
  }
}

async function selectCircleSession(session: string) {
  const next = String(session || '').trim().toLowerCase();
  if (next !== 'dialogue' && next !== 'meeting') return;
  if (!state.activeWorkspaceId) return;
  try {
    if (state.liveSessionActive && currentSession() === next) {
      const stopped = await deactivateLiveSession({
        disableMeetingConfig: next === 'meeting',
        finalizeMeeting: next === 'meeting',
      });
      if (stopped) {
        showStatus(next === 'meeting' ? 'live meeting off' : 'live dialogue off');
      }
      return;
    }
    const started = await activateLiveSession(next);
    if (started) {
      showStatus(next === 'meeting' ? 'live meeting on' : 'live dialogue on');
    }
  } catch (err) {
    const message = String(err?.message || err || `live ${next} failed`);
    if (next === 'meeting' && typeof appendPlainMessage === 'function') {
      appendPlainMessage('system', `Live meeting failed: ${message}`);
    }
    showStatus(`live ${next} failed: ${message}`);
  }
}

function onSegmentClick(event: Event) {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const segment = target.closest('.slopshell-circle-segment');
  if (!(segment instanceof HTMLButtonElement)) return;
  const kind = String(segment.dataset.kind || '').trim().toLowerCase();
  const name = String(segment.dataset.segment || '').trim().toLowerCase();
  if (kind === 'tool') {
    void selectCircleTool(name);
    return;
  }
  if (kind === 'session') {
    void selectCircleSession(name);
    return;
  }
  if (kind === 'toggle') {
    if (name === 'fast') {
      toggleFastMode();
      return;
    }
    toggleTTSSilentMode();
  }
}

function scheduleLongPressManage() {
  clearLongPressTimer();
  longPressTriggered = false;
  longPressTimer = window.setTimeout(() => {
    longPressTimer = null;
    longPressTriggered = true;
    openManagementPage();
  }, 460);
}

function cancelLongPressManage() {
  clearLongPressTimer();
}

function iconMarkup(icon: string) {
  return `<span class="slopshell-circle-icon" aria-hidden="true">${icon}</span>`;
}

function syncSegmentMarkup() {
  const menu = circleMenu();
  if (!(menu instanceof HTMLElement)) return;
  SLOPSHELL_CIRCLE_SEGMENTS.forEach((segmentContract) => {
    const node = menu.querySelector(`[data-segment="${segmentContract.id}"]`);
    if (!(node instanceof HTMLButtonElement)) return;
    node.innerHTML = iconMarkup(segmentContract.icon);
    node.dataset.icon = segmentContract.icon_id;
    node.setAttribute('aria-label', segmentContract.label);
    node.title = segmentContract.label;
  });
}

function ensureCornerControls() {
  const root = cornerControlsRoot();
  if (!(root instanceof HTMLElement)) return;
  if (root.childElementCount > 0) return;
  SLOPSHELL_CIRCLE_CORNERS.forEach((corner) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-btn edge-icon-btn slopshell-circle-corner-btn';
    button.dataset.corner = corner.id;
    button.setAttribute('aria-label', corner.label);
    button.title = corner.label;
    button.innerHTML = iconMarkup(corner.icon);
    button.addEventListener('click', () => {
      setSlopshellCircleCorner(corner.id);
    });
    root.appendChild(button);
  });
}

function syncCornerControls() {
  const root = cornerControlsRoot();
  if (!(root instanceof HTMLElement)) return;
  const activeCorner = currentCorner();
  root.querySelectorAll('button[data-corner]').forEach((node) => {
    if (!(node instanceof HTMLButtonElement)) return;
    const nextCorner = normalizeSlopshellCircleCorner(node.dataset.corner || '');
    node.setAttribute('aria-pressed', String(nextCorner === activeCorner));
  });
}

function shellSize() {
  return window.innerWidth <= 720
    ? SLOPSHELL_CIRCLE_LAYOUT.shell_size_mobile_px
    : SLOPSHELL_CIRCLE_LAYOUT.shell_size_px;
}

function dotSize() {
  return window.innerWidth <= 720
    ? SLOPSHELL_CIRCLE_LAYOUT.dot_size_mobile_px
    : SLOPSHELL_CIRCLE_LAYOUT.dot_size_px;
}

function segmentSize() {
  return window.innerWidth <= 720
    ? SLOPSHELL_CIRCLE_LAYOUT.segment_size_mobile_px
    : SLOPSHELL_CIRCLE_LAYOUT.segment_size_px;
}

function applyCornerGeometry() {
  const root = circleRoot();
  const dot = circleDot();
  const menu = circleMenu();
  if (!(root instanceof HTMLElement) || !(dot instanceof HTMLButtonElement) || !(menu instanceof HTMLElement)) return;
  const corner = currentCorner();
  const nextShellSize = shellSize();
  const nextDotSize = dotSize();
  const nextSegmentSize = segmentSize();
  const segmentHalf = nextSegmentSize / 2;
  const dotPosition = dotPositionForCorner(nextShellSize, nextDotSize, corner);
  const anchor = dotCenterForCorner(nextShellSize, nextDotSize, corner);
  const signs = cornerSigns(corner);

  root.dataset.corner = corner;
  root.style.setProperty('--slopshell-circle-shell-size', `${nextShellSize}px`);
  root.style.setProperty('--slopshell-circle-dot-size', `${nextDotSize}px`);
  root.style.setProperty('--slopshell-circle-segment-size', `${nextSegmentSize}px`);
  dot.style.left = `${dotPosition.left}px`;
  dot.style.top = `${dotPosition.top}px`;

  SLOPSHELL_CIRCLE_SEGMENTS.forEach((segmentContract) => {
    const node = menu.querySelector(`[data-segment="${segmentContract.id}"]`);
    if (!(node instanceof HTMLButtonElement)) return;
    const theta = (segmentContract.angle_deg * Math.PI) / 180;
    const dx = Math.cos(theta) * segmentContract.radius_px * signs.x;
    const dy = Math.sin(theta) * segmentContract.radius_px * signs.y;
    const left = anchor.x + dx - segmentHalf;
    const top = anchor.y + dy - segmentHalf;
    node.style.left = `${Math.round(left)}px`;
    node.style.top = `${Math.round(top)}px`;
  });
}

function loadStoredCorner() {
  state.toolPalettePosition = storageCorner();
}

function bindResizeLayout() {
  if (layoutBound) return;
  layoutBound = true;
  window.addEventListener('resize', () => applyCornerGeometry());
}

export function initSlopshellCircle() {
  const root = circleRoot();
  const dot = circleDot();
  const menu = circleMenu();
  if (!(root instanceof HTMLElement) || !(dot instanceof HTMLButtonElement) || !(menu instanceof HTMLElement)) return;
  if (circleBound) return;
  circleBound = true;

  loadStoredCorner();
  syncSegmentMarkup();
  ensureCornerControls();
  bindResizeLayout();

  dot.addEventListener('click', (event) => {
    if (longPressTriggered) {
      longPressTriggered = false;
      event.preventDefault();
      return;
    }
    event.preventDefault();
    toggleCircleExpanded();
  });
  dot.addEventListener('contextmenu', (event) => {
    event.preventDefault();
    openManagementPage();
  });
  dot.addEventListener('pointerdown', () => {
    scheduleLongPressManage();
  });
  dot.addEventListener('pointerup', cancelLongPressManage);
  dot.addEventListener('pointercancel', cancelLongPressManage);
  dot.addEventListener('pointerleave', cancelLongPressManage);

  menu.addEventListener('click', onSegmentClick);
  document.addEventListener('click', handleOutsideCircleClick, true);
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if (circleExpanded) {
      event.preventDefault();
      collapseSlopshellCircle();
      return;
    }
    if (state.liveSessionActive) {
      event.preventDefault();
      void handleStopAction();
    }
  });

  renderSlopshellCircle();
}

export function setSlopshellCircleCorner(next: string) {
  const normalized = normalizeSlopshellCircleCorner(next);
  if (normalized === currentCorner()) return;
  persistCorner(normalized);
  applyCornerGeometry();
  syncCornerControls();
  showStatus(`circle moved to ${normalized.replace('_', ' ')}`);
}

export function renderSlopshellCircle() {
  const root = circleRoot();
  const dot = circleDot();
  const menu = circleMenu();
  if (!(root instanceof HTMLElement) || !(dot instanceof HTMLButtonElement) || !(menu instanceof HTMLElement)) return;
  initSlopshellCircle();

  root.classList.toggle('is-expanded', circleExpanded);
  root.classList.toggle('is-collapsed', !circleExpanded);
  root.dataset.state = circleExpanded ? 'expanded' : 'collapsed';

  const tool = String(state.interaction.tool || 'pointer').trim().toLowerCase() || 'pointer';
  const session = currentSession();
  const sessionLabel = sessionDisplayName(session);
  const toolLabel = toolDisplayName(tool);
  dot.dataset.tool = tool;
  dot.dataset.icon = slopshellCircleToolIconID(tool);
  dot.dataset.session = session;
  dot.dataset.silent = String(Boolean(state.ttsSilent));
  dot.innerHTML = `${iconMarkup(slopshellCircleToolIcon(tool))}<span class="slopshell-circle-dot-badge" aria-hidden="true">${sessionLabel}</span>`;
  dot.title = circleExpanded ? 'Close Slopshell Circle' : 'Open Slopshell Circle';
  dot.setAttribute(
    'aria-label',
    `${circleExpanded ? 'Close' : 'Open'} Slopshell Circle. Live mode: ${sessionLabel}. Current tool: ${toolLabel}. Fast mode: ${state.fastMode ? 'on' : 'off'}.`,
  );
  dot.setAttribute('aria-expanded', circleExpanded ? 'true' : 'false');

  const segments = menu.querySelectorAll('.slopshell-circle-segment');
  const disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  segments.forEach((node) => {
    if (!(node instanceof HTMLButtonElement)) return;
    const kind = String(node.dataset.kind || '').trim().toLowerCase();
    const name = String(node.dataset.segment || '').trim().toLowerCase();
    if (kind === 'tool') {
      node.setAttribute('aria-pressed', String(name === tool));
      node.disabled = disabled;
      return;
    }
    if (kind === 'session') {
      node.setAttribute('aria-pressed', String(name === session));
      node.disabled = disabled || !state.activeWorkspaceId;
      return;
    }
    if (name === 'fast') {
      node.setAttribute('aria-pressed', String(Boolean(state.fastMode)));
      node.disabled = disabled;
      return;
    }
    node.setAttribute('aria-pressed', String(Boolean(state.ttsSilent)));
    node.disabled = !state.ttsEnabled || disabled;
  });

  applyCornerGeometry();
  syncCornerControls();
}

export function openManagementPage(path = 'manage') {
  const targetURL = appURL(String(path || 'manage'));
  if (state.liveSessionActive) {
    window.open(targetURL, '_blank', 'noopener');
    return;
  }
  window.location.assign(targetURL);
}
