import * as context from './app-context.js';
import { apiURL } from './app-env.js';

const { refs, state } = context;

const renderSidebarRow = (...args) => refs.renderSidebarRow(...args);
const formatSidebarAge = (...args) => refs.formatSidebarAge(...args);
const showStatus = (...args) => refs.showStatus(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);

export function isDedupCandidateGroup(item) {
  return Array.isArray(item?.items)
    && ['open', 'review_later'].includes(String(item?.state || '').trim().toLowerCase())
    && ['action', 'project'].includes(String(item?.kind || '').trim().toLowerCase());
}

export function renderDedupCandidateRow(group) {
  const row = renderSidebarRow({
    icon: 'symbol',
    iconText: '=',
    label: dedupCandidateLabel(group),
    meta: formatSidebarAge(group?.detected_at),
    active: Number(group?.id || 0) === Number(state.itemSidebarActiveItemID || 0),
    item: group,
    onClick: () => {},
  });
  row.classList.add('dedup-candidate-row');
  appendDedupCandidateMembers(row, group);
  appendDedupActions(row, group);
  return row;
}

function dedupCandidateLabel(group) {
  const kind = String(group?.kind || '').trim().toLowerCase() === 'project' ? 'Project duplicate' : 'Action duplicate';
  const items = Array.isArray(group?.items) ? group.items : [];
  const outcome = String(group?.outcome || '').trim();
  return outcome ? `${kind}: ${outcome}` : `${kind} (${items.length} items)`;
}

function appendDedupCandidateMembers(row, group) {
  const target = row.querySelector('.sidebar-row-secondary');
  if (!(target instanceof HTMLElement)) return;
  const members = document.createElement('span');
  members.className = 'sidebar-dedup-members';
  dedupMemberLines(group).forEach((line) => {
    const entry = document.createElement('span');
    entry.className = 'sidebar-row-subtitle';
    entry.textContent = line;
    members.appendChild(entry);
  });
  target.appendChild(members);
}

function dedupMemberLines(group) {
  const items = Array.isArray(group?.items) ? group.items : [];
  return items.map((member) => {
    const item = member?.item || {};
    const bindings = dedupBindingLabels(member);
    const containers = dedupContainerLabels(member);
    const dates = Array.isArray(member?.dates) ? member.dates : [];
    return [
      `#${Number(item?.id || 0)} ${String(item?.title || 'Untitled item').trim()}`,
      String(member?.outcome || '').trim(),
      bindings.join(', '),
      containers.length > 0 ? `containers ${containers.join(', ')}` : '',
      dates.join(', '),
    ].filter(Boolean).join(' · ');
  });
}

function dedupBindingLabels(member) {
  const bindings = Array.isArray(member?.source_bindings) ? member.source_bindings : [];
  return bindings.map((binding) => {
    const provider = String(binding?.provider || '').trim();
    const objectType = String(binding?.object_type || '').trim();
    const remoteID = String(binding?.remote_id || '').trim();
    return [provider, objectType, remoteID].filter(Boolean).join(':');
  }).filter(Boolean);
}

function dedupContainerLabels(member) {
  const containers = Array.isArray(member?.source_containers) ? member.source_containers : [];
  return containers.map((value) => String(value || '').trim()).filter(Boolean);
}

function appendDedupActions(row, group) {
  const target = row.querySelector('.sidebar-row-secondary');
  if (!(target instanceof HTMLElement)) return;
  const actions = document.createElement('span');
  actions.className = 'sidebar-row-badges';
  appendDedupMergeButtons(actions, group);
  appendDedupActionButton(actions, group, 'keep_separate', 'Keep separate', null);
  appendDedupActionButton(actions, group, 'review_later', 'Review later', null);
  target.appendChild(actions);
}

function appendDedupMergeButtons(actions, group) {
  const items = Array.isArray(group?.items) ? group.items : [];
  items.forEach((member) => {
    const itemID = Number(member?.item?.id || 0);
    if (itemID > 0) appendDedupActionButton(actions, group, 'merge', `Merge into #${itemID}`, itemID);
  });
}

function appendDedupActionButton(actions, group, action, label, canonicalItemID) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'sidebar-badge';
  button.textContent = label;
  button.addEventListener('click', (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    void performDedupAction(group, action, canonicalItemID);
  });
  actions.appendChild(button);
}

async function performDedupAction(group, action, canonicalItemID) {
  const candidateID = Number(group?.id || 0);
  if (candidateID <= 0) return false;
  const body = canonicalItemID ? { canonical_item_id: canonicalItemID } : {};
  const resp = await fetch(apiURL(`items/dedup/${candidateID}/${encodeURIComponent(action)}`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    showStatus(`dedup action failed: ${detail}`);
    return false;
  }
  showStatus(`dedup ${String(action).replace(/_/g, ' ')}`);
  await loadItemSidebarView(state.itemSidebarView, state.itemSidebarFilters);
  return true;
}
