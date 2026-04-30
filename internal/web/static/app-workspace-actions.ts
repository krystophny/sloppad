type WorkspaceActionState = {
  projectSwitchInFlight: boolean;
  projectModelSwitchInFlight: boolean;
  defaultWorkspaceId: string;
  projects: Array<{ id?: string }>;
};

type WorkspaceActionDeps = {
  apiURL: (path: string) => string;
  state: WorkspaceActionState;
  showStatus: (text: string) => void;
  appendPlainMessage: (role: string, text: string) => void;
  fetchProjects: () => Promise<void>;
  switchProject: (workspaceID: string) => Promise<void>;
  upsertProject?: (project: any) => void;
  renderEdgeTopProjects?: () => void;
  renderEdgeTopModelButtons?: () => void;
  submitMessage?: (text: string, options?: Record<string, any>) => Promise<void>;
};

function startAgentHereCursor(sourceWorkspaceID: string, sourceNotePath: string): Record<string, any> | null {
  const sourcePath = String(sourceNotePath || '').trim();
  if (!sourcePath) return null;
  return {
    view: 'source_context',
    element: 'agent_handoff',
    title: sourcePath,
    path: sourcePath,
    is_dir: false,
    workspace_name: String(sourceWorkspaceID || '').trim(),
  };
}

async function postWorkspaceAction(
  deps: WorkspaceActionDeps,
  path: string,
  body: Record<string, any>,
): Promise<any> {
  const resp = await fetch(deps.apiURL(path), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  return resp.json();
}

async function openLinkedWorkspaceAtPath(
  deps: WorkspaceActionDeps,
  workspacePath: string,
  statusText: string,
  failurePrefix: string,
  readyText: string,
  sourceWorkspaceID = '',
  sourceNotePath = '',
): Promise<string> {
  const path = String(workspacePath || '').trim();
  if (!path) return '';
  if (deps.state.projectSwitchInFlight || deps.state.projectModelSwitchInFlight) return '';
  deps.showStatus(statusText);
  try {
    const sourceID = String(sourceWorkspaceID || '').trim();
    const responsePayload = await postWorkspaceAction(deps, 'runtime/workspaces', {
      kind: 'linked',
      path,
      activate: true,
      ...(sourceID ? { source_workspace_id: sourceID } : {}),
      ...(String(sourceNotePath || '').trim() ? { source_path: String(sourceNotePath || '').trim() } : {}),
    });
    const project = responsePayload?.workspace || {};
    const workspaceID = String(project?.id || '').trim();
    await deps.fetchProjects();
    if (workspaceID) {
      await deps.switchProject(workspaceID);
      return workspaceID;
    }
    deps.showStatus(readyText);
    return '';
  } catch (err) {
    const message = String(err?.message || err || 'workspace open failed');
    deps.appendPlainMessage('system', `${failurePrefix}: ${message}`);
    deps.showStatus(`${failurePrefix}: ${message}`);
    return '';
  }
}

export async function createLinkedWorkspaceAtPath(
  deps: WorkspaceActionDeps,
  workspacePath: string,
  sourceWorkspaceID = '',
  sourceNotePath = '',
): Promise<void> {
  await openLinkedWorkspaceAtPath(
    deps,
    workspacePath,
    'opening linked workspace...',
    'Linked workspace open failed',
    'linked workspace ready',
    sourceWorkspaceID,
    sourceNotePath,
  );
}

export async function startAgentHereAtPath(
  deps: WorkspaceActionDeps,
  workspacePath: string,
  sourceWorkspaceID = '',
  sourceNotePath = '',
): Promise<void> {
  const workspaceID = await openLinkedWorkspaceAtPath(
    deps,
    workspacePath,
    'starting agent here...',
    'Start agent here failed',
    'agent ready',
    sourceWorkspaceID,
    sourceNotePath,
  );
  if (!workspaceID || typeof deps.submitMessage !== 'function') return;
  const cursor = startAgentHereCursor(sourceWorkspaceID, sourceNotePath);
  await deps.submitMessage('Start agent here.', {
    kind: 'start_agent_here',
    ...(cursor ? { cursor } : {}),
  });
}

export async function persistTemporaryProject(
  deps: WorkspaceActionDeps,
  workspaceID: string,
): Promise<void> {
  const id = String(workspaceID || '').trim();
  if (!id) return;
  if (deps.state.projectSwitchInFlight || deps.state.projectModelSwitchInFlight) return;
  deps.showStatus('saving session...');
  try {
    const payload = await postWorkspaceAction(
      deps,
      `runtime/workspaces/${encodeURIComponent(id)}/persist`,
      {},
    );
    if (payload?.workspace && typeof deps.upsertProject === 'function') {
      deps.upsertProject(payload.workspace);
    }
    await deps.fetchProjects();
    deps.renderEdgeTopProjects?.();
    deps.renderEdgeTopModelButtons?.();
    deps.showStatus('session saved');
  } catch (err) {
    const message = String(err?.message || err || 'session save failed');
    deps.appendPlainMessage('system', `Session save failed: ${message}`);
    deps.showStatus(`session save failed: ${message}`);
  }
}

export async function discardTemporaryProject(
  deps: WorkspaceActionDeps,
  workspaceID: string,
): Promise<void> {
  const id = String(workspaceID || '').trim();
  if (!id) return;
  if (deps.state.projectSwitchInFlight || deps.state.projectModelSwitchInFlight) return;
  deps.showStatus('discarding session...');
  try {
    const payload = await postWorkspaceAction(
      deps,
      `runtime/workspaces/${encodeURIComponent(id)}/discard`,
      {},
    );
    const nextWorkspaceID = String(payload?.active_workspace_id || '').trim()
      || deps.state.defaultWorkspaceId
      || deps.state.projects[0]?.id
      || '';
    await deps.fetchProjects();
    if (nextWorkspaceID) {
      await deps.switchProject(nextWorkspaceID);
      return;
    }
    deps.renderEdgeTopProjects?.();
    deps.renderEdgeTopModelButtons?.();
    deps.showStatus('session discarded');
  } catch (err) {
    const message = String(err?.message || err || 'session discard failed');
    deps.appendPlainMessage('system', `Session discard failed: ${message}`);
    deps.showStatus(`session discard failed: ${message}`);
  }
}
