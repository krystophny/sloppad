import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  action?: string;
  text?: string;
  [key: string]: unknown;
};

const TEST_IMAGE_DATA_URL = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFElEQVR42mP8z/D/PwMDAwMjI4MBAF0CBR8XTur2AAAAAElFTkSuQmCC';

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => {
    (window as any).__harnessLog.splice(0);
  });
}

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._slopshellApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function injectCanvasModuleRef(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/canvas.js');
    (window as any).__canvasModule = mod;
  });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._slopshellApp;
    const sessionId = String(app?.getState?.().chatSessionId || '');
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string'
      && ws.url.includes('/ws/chat/')
      && (!sessionId || ws.url.includes(`/ws/chat/${sessionId}`)));
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
}

async function renderTestArtifact(page: Page) {
  await page.evaluate(() => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'cursor-artifact',
      kind: 'text_artifact',
      title: 'test.txt',
      text: 'Line one\nLine two\nLine three\nLine four\nLine five',
    });
    const pane = document.getElementById('canvas-text');
    if (pane) {
      pane.style.display = '';
      pane.classList.add('is-active');
    }
    const app = (window as any)._slopshellApp;
    if (app?.getState) app.getState().hasArtifact = true;
  });
}

async function renderPdfArtifactMock(page: Page) {
  await page.evaluate(() => {
    const pane = document.getElementById('canvas-pdf');
    if (!(pane instanceof HTMLElement)) return;
    pane.style.display = '';
    pane.classList.add('is-active');
    pane.innerHTML = '';

    const surface = document.createElement('div');
    surface.className = 'canvas-pdf-surface';
    const pagesHost = document.createElement('div');
    pagesHost.className = 'canvas-pdf-pages';

    const pageNode = document.createElement('section');
    pageNode.className = 'canvas-pdf-page';
    pageNode.dataset.page = '1';

    const pageInner = document.createElement('div');
    pageInner.className = 'canvas-pdf-page-inner';
    pageInner.style.width = '640px';
    pageInner.style.height = '860px';

    const canvas = document.createElement('canvas');
    canvas.className = 'canvas-pdf-canvas';
    canvas.width = 640;
    canvas.height = 860;
    canvas.style.width = '640px';
    canvas.style.height = '860px';
    pageInner.appendChild(canvas);

    const textLayer = document.createElement('div');
    textLayer.className = 'textLayer canvas-pdf-text-layer';
    textLayer.style.setProperty('--scale-factor', '1');

    const line = document.createElement('span');
    line.textContent = 'Persistent PDF note';
    line.style.position = 'absolute';
    line.style.left = '72px';
    line.style.top = '132px';
    line.style.fontSize = '18px';
    line.style.lineHeight = '1';
    textLayer.appendChild(line);
    pageInner.appendChild(textLayer);

    pageNode.appendChild(pageInner);
    pagesHost.appendChild(pageNode);
    surface.appendChild(pagesHost);
    pane.appendChild(surface);

    const app = (window as any)._slopshellApp;
    const state = app?.getState?.();
    if (state) {
      state.currentCanvasArtifact = {
        kind: 'pdf_artifact',
        title: 'test.pdf',
        path: 'docs/test.pdf',
        event_id: 'art-pdf-1',
      };
      state.hasArtifact = true;
    }
    document.dispatchEvent(new CustomEvent('slopshell:canvas-rendered', {
      detail: {
        kind: 'pdf_artifact',
        title: 'test.pdf',
        path: 'docs/test.pdf',
        event_id: 'art-pdf-1',
      },
    }));
  });
}

async function renderImageArtifactMock(page: Page) {
  await page.evaluate(async (dataURL) => {
    const imagePane = document.getElementById('canvas-image');
    const image = document.getElementById('canvas-img');
    if (!(imagePane instanceof HTMLElement) || !(image instanceof HTMLImageElement)) return;
    imagePane.style.display = '';
    imagePane.classList.add('is-active');
    image.src = String(dataURL || '');
    image.alt = 'test-image.png';
    image.style.width = '320px';
    image.style.height = '240px';
    const app = (window as any)._slopshellApp;
    const state = app?.getState?.();
    if (state) {
      state.currentCanvasArtifact = {
        kind: 'image_artifact',
        title: 'test-image.png',
        path: 'docs/test-image.png',
        event_id: 'art-image-1',
      };
      state.hasArtifact = true;
    }
    document.dispatchEvent(new CustomEvent('slopshell:canvas-rendered', {
      detail: {
        kind: 'image_artifact',
        title: 'test-image.png',
        path: 'docs/test-image.png',
        event_id: 'art-image-1',
      },
    }));
    if (!(image.complete && image.naturalWidth > 0)) {
      await new Promise<void>((resolve) => {
        const finish = () => resolve();
        image.addEventListener('load', finish, { once: true });
        image.addEventListener('error', finish, { once: true });
        window.setTimeout(finish, 300);
      });
      try {
        await image.decode();
      } catch (_) {}
    }
  }, TEST_IMAGE_DATA_URL);
}

async function renderMarkdownArtifactWithImage(page: Page) {
  await page.evaluate(() => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'markdown-artifact',
      kind: 'text_artifact',
      title: 'docs/readme.md',
      path: 'docs/readme.md',
      text: '![Diagram](images/diagram.png)',
    });
    const pane = document.getElementById('canvas-text');
    if (pane) {
      pane.style.display = '';
      pane.classList.add('is-active');
    }
    const app = (window as any)._slopshellApp;
    const state = app?.getState?.();
    if (state) {
      state.currentCanvasArtifact = {
        kind: 'text_artifact',
        title: 'docs/readme.md',
        path: 'docs/readme.md',
      };
      state.hasArtifact = true;
    }
  });
}

async function seedBrainWorkspace(page: Page) {
  await page.evaluate(() => {
    (window as any).__setProjects([
      {
        id: 'brain',
        name: 'Brain',
        kind: 'linked',
        sphere: 'work',
        workspace_path: '/tmp/vault/brain',
        root_path: '/tmp/vault/brain',
        chat_session_id: 'chat-brain',
        canvas_session_id: 'brain',
        chat_mode: 'chat',
        chat_model: 'local',
        chat_model_reasoning_effort: 'none',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
    ], 'brain');
  });
}

async function setInteractionTool(page: Page, tool: 'pointer' | 'highlight' | 'ink' | 'text_note' | 'prompt') {
  await page.evaluate((mode) => {
    (window as any).__setRuntimeState?.({ tool: mode });
    const app = (window as any)._slopshellApp;
    if (app?.getState) {
      const interaction = app.getState().interaction;
      interaction.tool = mode;
      interaction.toolPinned = true;
    }
  }, tool);
}

async function openCircle(page: Page) {
  await page.evaluate(() => {
    const button = document.getElementById('slopshell-circle-dot');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('slopshell circle dot not found');
    }
    button.click();
  });
  await expect(page.locator('#slopshell-circle')).toHaveAttribute('data-state', 'expanded');
}

async function switchToTestProject(page: Page) {
  await page.evaluate(() => {
    const buttons = Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'));
    const button = buttons.find((node) => node.textContent?.trim().toLowerCase() === 'test');
    if (button instanceof HTMLButtonElement) {
      button.click();
    }
  });
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._slopshellApp;
    const state = app?.getState?.();
    const wsOpen = (window as any).WebSocket.OPEN;
    if (String(state?.activeWorkspaceId || '') !== 'test') return '';
    return state?.chatWs?.readyState === wsOpen ? 'ready' : 'waiting';
  })).toBe('ready');
}

async function setLiveMode(page: Page, mode: 'dialogue' | 'meeting') {
  await switchToTestProject(page);
  await openCircle(page);
  const buttonId = mode === 'dialogue'
    ? 'slopshell-circle-segment-dialogue'
    : 'slopshell-circle-segment-meeting';
  await page.evaluate((id) => {
    const button = document.getElementById(id);
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error(`live mode button not found: ${id}`);
    }
    button.click();
  }, buttonId);
  await expect(page.locator(`#${buttonId}`)).toHaveAttribute('aria-pressed', 'true');
}

async function currentDotPosition(page: Page) {
  return page.evaluate(() => {
    const dot = document.querySelector('#indicator .record-dot');
    if (!(dot instanceof HTMLElement)) return null;
    return {
      left: dot.style.left,
      top: dot.style.top,
      indicatorClass: document.getElementById('indicator')?.className || '',
    };
  });
}

async function triggerVoiceAssistantTTS(page: Page, turnID: string, text = 'Hello there.') {
  await page.evaluate(() => {
    const app = (window as any)._slopshellApp;
    const s = app.getState();
    s.lastInputOrigin = 'voice';
    s.voiceAwaitingTurn = true;
  });
  await injectChatEvent(page, { type: 'turn_started', turn_id: turnID });
  await injectChatEvent(page, { type: 'assistant_message', turn_id: turnID, message: text });
  await injectChatEvent(page, { type: 'assistant_output', role: 'assistant', turn_id: turnID, message: text });
}

test.beforeEach(async ({ page }) => {
  await waitReady(page);
  await injectCanvasModuleRef(page);
});

test('dialogue tap starts local capture with the tapped cursor context', async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__slopshellConversationListenMs = 1_200;
  });
  await setLiveMode(page, 'dialogue');
  await renderTestArtifact(page);
  await clearLog(page);
  await triggerVoiceAssistantTTS(page, 'cursor-dialogue-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  const x = 420;
  const y = 360;
  await page.mouse.click(x, y);
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }).toBe(true);

  let log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);

  await page.mouse.click(x, y);
  await expect.poll(async () => {
    const nextLog = await getLog(page);
    return nextLog.find((entry) => entry.type === 'message_sent') || null;
  }).not.toBeNull();

  log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  const sentEntry = log.find((entry) => entry.type === 'message_sent');
  expect(String(sentEntry?.text || '')).toContain('hello world');
  expect(sentEntry?.cursor).toMatchObject({
    title: 'test.txt',
  });
});

test('meeting taps start direct capture without dispatching canvas cursor prompts', async ({ page }) => {
  await setLiveMode(page, 'meeting');
  await renderTestArtifact(page);
  await clearLog(page);

  const firstX = 420;
  const firstY = 360;
  const secondX = 520;
  const secondY = 430;

  await page.mouse.click(firstX, firstY);
  await page.waitForTimeout(120);
  await page.mouse.click(secondX, secondY);
  await page.waitForTimeout(120);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(true);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
});

test('request_position stays local in annotation tools instead of dispatching a reply', async ({ page }) => {
  await renderPdfArtifactMock(page);
  await setInteractionTool(page, 'text_note');
  await clearLog(page);
  await injectChatEvent(page, {
    type: 'request_position',
    prompt: 'Tap where the comment should go.',
  });

  await page.mouse.click(420, 360);
  await page.waitForTimeout(150);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(false);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  await expect(page.locator('#annotation-bubble')).toBeVisible();
  await expect(page.locator('#canvas-pdf .canvas-sticky-note')).toHaveCount(1);
  expect(await page.evaluate(() => (window as any)._slopshellApp.getState().requestedPositionPrompt)).toBe('Tap where the comment should go.');
});

test('request_position in prompt tool starts a local capture instead of streaming a reply', async ({ page }) => {
  await renderTestArtifact(page);
  await setInteractionTool(page, 'prompt');
  await clearLog(page);
  await injectChatEvent(page, {
    type: 'request_position',
    prompt: 'Tap where the comment should go.',
  });

  await page.mouse.click(420, 360);
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }).toBe(true);

  let log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  expect(await page.evaluate(() => (window as any)._slopshellApp.getState().requestedPositionPrompt)).toBe('');

  await page.mouse.click(420, 360);
  await expect.poll(async () => {
    const nextLog = await getLog(page);
    return nextLog.find((entry) => entry.type === 'message_sent') || null;
  }).not.toBeNull();

  log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  const sentEntry = log.find((entry) => entry.type === 'message_sent');
  expect(String(sentEntry?.text || '')).toContain('hello world');
  expect(sentEntry?.cursor).toMatchObject({
    title: 'test.txt',
  });
});

test('meeting image taps start direct capture without dispatching canvas cursor prompts', async ({ page }) => {
  await setLiveMode(page, 'meeting');
  await renderImageArtifactMock(page);
  await clearLog(page);

  const imageBox = await page.locator('#canvas-img').boundingBox();
  expect(imageBox).not.toBeNull();
  await expect.poll(async () => page.evaluate(() => {
    const image = document.getElementById('canvas-img');
    return image instanceof HTMLImageElement ? image.naturalWidth : 0;
  })).toBeGreaterThan(0);
  await page.mouse.click((imageBox?.x || 0) + (imageBox?.width || 0) * 0.5, (imageBox?.y || 0) + (imageBox?.height || 0) * 0.5);
  await page.waitForTimeout(200);
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(true);
});

test('pdf meeting taps start direct capture without dispatching canvas cursor prompts', async ({ page }) => {
  await setLiveMode(page, 'meeting');
  await renderPdfArtifactMock(page);
  await clearLog(page);

  const pageBox = await page.locator('#canvas-pdf .canvas-pdf-page-inner').boundingBox();
  expect(pageBox).not.toBeNull();
  await page.mouse.click((pageBox?.x || 0) + (pageBox?.width || 0) * 0.35, (pageBox?.y || 0) + (pageBox?.height || 0) * 0.25);
  await page.waitForTimeout(200);
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(true);
});

test('markdown image paths are rewritten through the canvas file proxy', async ({ page }) => {
  await renderMarkdownArtifactWithImage(page);
  await expect.poll(async () => page.evaluate(() => {
    const img = document.querySelector('#canvas-text img');
    return img instanceof HTMLImageElement ? img.src : '';
  })).toContain('/api/files/');
  await expect(page.locator('#canvas-text img')).toHaveAttribute('src', /docs%2Fimages%2Fdiagram\.png/);
});

test('folder markdown links start an agent in the linked workspace rooted at the vault target', async ({ page }) => {
  await seedBrainWorkspace(page);
  await clearLog(page);
  await page.evaluate(() => {
    const app = (window as any)._slopshellApp;
    if (app?.getState) app.getState().activeWorkspaceId = 'brain';
    (window as any).__mockMarkdownLinkResolution = {
      ok: true,
      kind: 'folder',
      resolved_path: 'project/path',
      vault_relative_path: 'project/path',
      source_path: 'topics/active.md',
    };
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'folder-markdown-link',
      kind: 'text_artifact',
      title: 'topics/active.md',
      path: 'topics/active.md',
      text: '[Project folder](../../project/path/)',
    });
  });

  await page.locator('#canvas-text a', { hasText: 'Project folder' }).evaluate((node) => {
    if (node instanceof HTMLAnchorElement) node.click();
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.kind || '') === 'linked'
        && String(entry.payload?.path || '') === '/tmp/vault/project/path',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.source_workspace_id || '') === 'brain',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.source_path || '') === 'topics/active.md',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'message_sent'
        && String(entry.text || '') === 'Start agent here.',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).not.toBe('brain');
});

test('linked workspace welcome action returns to the source note in one step', async ({ page }) => {
  await seedBrainWorkspace(page);
  await page.evaluate(() => {
    const mod = (window as any)._slopshellApp;
    if (mod?.getState) mod.getState().activeWorkspaceId = 'linked-1';
  });
  await page.evaluate(async () => {
    const mod = await import(`../../internal/web/static/app-chat-ui.js?ts=${Date.now()}`);
    mod.renderWelcomeSurface({
      workspace_id: 'linked-1',
      title: 'Linked source',
      sections: [{
        id: 'origin',
        title: 'Origin',
        cards: [{
          id: 'return-to-source-note',
          title: 'Return to source note',
          subtitle: 'topics/active.md',
          description: 'Go back to the brain note that opened this workspace.',
          action: {
            type: 'switch_workspace_and_open_file',
            workspace_id: 'brain',
            path: 'topics/active.md',
          },
        }],
      }],
    });
  });

  await page.locator('#canvas-text .welcome-card', { hasText: 'Return to source note' }).click();

  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).toBe('brain');
  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().workspaceOpenFilePath || '')), { timeout: 5_000 }).toBe('topics/active.md');
});

test('start agent here welcome action opens the linked source folder and sends a starter turn', async ({ page }) => {
  await seedBrainWorkspace(page);
  await page.evaluate(async () => {
    const mod = await import(`../../internal/web/static/app-chat-ui.js?ts=${Date.now()}`);
    mod.renderWelcomeSurface({
      workspace_id: 'brain',
      title: 'Linked source',
      sections: [{
        id: 'agent',
        title: 'Agent',
        cards: [{
          id: 'start-agent-here',
          title: 'Start agent here',
          subtitle: 'project/path',
          description: 'Use nearest instructions',
          action: {
            type: 'start_agent_here',
            path: '/tmp/vault/project/path',
          },
        }],
      }],
    });
  });

  await page.locator('#canvas-text .welcome-card', { hasText: 'Start agent here' }).click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.kind || '') === 'linked'
        && String(entry.payload?.path || '') === '/tmp/vault/project/path',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'message_sent'
        && String(entry.text || '') === 'Start agent here.',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).not.toBe('brain');
});

test('start agent here welcome action preserves the active linked workspace origin', async ({ page }) => {
  await page.evaluate(() => {
    const projects = [
      {
        id: 'brain',
        name: 'Brain',
        kind: 'linked',
        sphere: 'work',
        workspace_path: '/tmp/vault/brain',
        root_path: '/tmp/vault/brain',
        chat_session_id: 'chat-brain',
        canvas_session_id: 'brain',
        chat_mode: 'chat',
        chat_model: 'local',
        chat_model_reasoning_effort: 'none',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
      {
        id: 'linked-1',
        name: 'Project path',
        kind: 'linked',
        sphere: 'work',
        workspace_path: '/tmp/vault/project/path',
        root_path: '/tmp/vault/project/path',
        source_workspace_id: 'brain',
        source_path: 'topics/active.md',
        chat_session_id: 'chat-linked',
        canvas_session_id: 'linked-1',
        chat_mode: 'chat',
        chat_model: 'local',
        chat_model_reasoning_effort: 'none',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
    ];
    (window as any).__setProjects(projects, 'linked-1');
    const state = (window as any)._slopshellApp?.getState?.();
    if (state) {
      state.projects = projects;
      state.activeWorkspaceId = 'linked-1';
      state.chatSessionId = 'chat-linked';
      state.sessionId = 'linked-1';
    }
  });
  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).toBe('linked-1');
  await clearLog(page);

  await page.evaluate(async () => {
    const mod = await import(`../../internal/web/static/app-chat-ui.js?ts=${Date.now()}`);
    mod.renderWelcomeSurface({
      workspace_id: 'linked-1',
      title: 'Linked source',
      sections: [{
        id: 'agent',
        title: 'Agent',
        cards: [{
          id: 'start-agent-here',
          title: 'Start agent here',
          subtitle: 'project/path',
          description: 'Use nearest instructions',
          action: {
            type: 'start_agent_here',
            path: '/tmp/vault/project/path',
          },
        }],
      }],
    });
  });

  await page.locator('#canvas-text .welcome-card', { hasText: 'Start agent here' }).click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'message_sent'
        && String(entry.text || '') === 'Start agent here.',
    );
  }, { timeout: 5_000 }).toBe(true);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'project_create')).toBe(false);
  const startEntry = log.find(
    (entry) => entry.type === 'message_sent'
      && String(entry.text || '') === 'Start agent here.',
  ) as HarnessLogEntry | undefined;
  expect((startEntry?.cursor as any)?.path).toBe('topics/active.md');
  expect((startEntry?.cursor as any)?.view).toBe('source_context');
  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).toBe('linked-1');
});

test('file markdown links open the parent folder as source context', async ({ page }) => {
  await seedBrainWorkspace(page);
  await clearLog(page);
  await page.evaluate(() => {
    const app = (window as any)._slopshellApp;
    if (app?.getState) {
      app.getState().activeWorkspaceId = 'brain';
    }
    (window as any).__mockWorkspaceFiles = {
      'active|': [
        { name: 'AGENTS.md', path: 'AGENTS.md', is_dir: false },
        { name: 'notes.md', path: 'notes.md', is_dir: false },
      ],
    };
    (window as any).__mockMarkdownLinkResolution = {
      ok: true,
      kind: 'text',
      resolved_path: 'project/path/file.md',
      vault_relative_path: 'project/path/file.md',
      source_path: 'topics/active.md',
      file_url: '/api/workspaces/brain/markdown-link/file?path=project%2Fpath%2Ffile.md',
    };
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'file-markdown-link',
      kind: 'text_artifact',
      title: 'topics/active.md',
      path: 'topics/active.md',
      text: '[File](../../project/path/file.md)',
    });
  });

  await page.locator('#canvas-text a', { hasText: 'File' }).evaluate((node) => {
    if (node instanceof HTMLAnchorElement) node.click();
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.kind || '') === 'linked'
        && String(entry.payload?.path || '') === '/tmp/vault/project/path',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.source_workspace_id || '') === 'brain',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.source_path || '') === 'topics/active.md',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'message_sent'
      && String(entry.text || '') === 'Start agent here.',
    );
  }, { timeout: 5_000 }).toBe(true);

  const startEntry = (await getLog(page)).find(
    (entry) => entry.type === 'message_sent'
      && String(entry.text || '') === 'Start agent here.',
  ) as HarnessLogEntry | undefined;
  expect((startEntry?.cursor as any)?.path).toBe('topics/active.md');
  expect((startEntry?.cursor as any)?.view).toBe('source_context');

  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).not.toBe('brain');

  await page.locator('#edge-left-tap').click();
  await page.getByRole('button', { name: 'Files' }).click();
  await expect(page.locator('#pr-file-list')).toContainText('AGENTS.md');
  await expect(page.locator('#pr-file-list')).toContainText('notes.md');

  await page.evaluate(async () => {
    const mod = await import(`../../internal/web/static/app-chat-transport.js?ts=${Date.now()}`);
    await mod.switchProject('brain');
  });
  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).toBe('brain');
});

test('in-brain markdown note links stay in the current workspace and render the linked note', async ({ page }) => {
  await seedBrainWorkspace(page);
  await clearLog(page);
  await page.evaluate(() => {
    const app = (window as any)._slopshellApp;
    if (app?.getState) {
      app.getState().activeWorkspaceId = 'brain';
    }
    (window as any).__mockMarkdownLinkFileText = '# Related note\n\nBrain note body';
    (window as any).__mockMarkdownLinkResolution = {
      ok: true,
      kind: 'text',
      resolved_path: 'brain/topics/related.md',
      vault_relative_path: 'brain/topics/related.md',
      source_path: 'topics/active.md',
      file_url: '/api/workspaces/brain/markdown-link/file?path=brain%2Ftopics%2Frelated.md',
    };
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'in-brain-markdown-link',
      kind: 'text_artifact',
      title: 'topics/active.md',
      path: 'topics/active.md',
      text: '[Related](related.md)',
    });
  });

  await page.locator('#canvas-text a', { hasText: 'Related' }).evaluate((node) => {
    if (node instanceof HTMLAnchorElement) node.click();
  });

  await expect(page.locator('#canvas-text')).toContainText('Brain note body');
  await expect.poll(async () => page.evaluate(() => String((window as any)._slopshellApp?.getState?.().activeWorkspaceId || '')), { timeout: 5_000 }).toBe('brain');
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'project_create')).toBe(false);
});

test('blocked markdown note links surface resolver reasons on canvas', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    (window as any).__mockMarkdownLinkResolution = {
      ok: false,
      blocked: true,
      reason: 'link target leaves the vault',
    };
    const app = (window as any)._slopshellApp;
    if (app?.getState) app.getState().activeWorkspaceId = 'active';
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'blocked-markdown-link',
      kind: 'text_artifact',
      title: 'topics/active.md',
      path: 'topics/active.md',
      text: '[Outside](../../../outside.md)\n\n[[Private Note]]',
    });
  });

  await page.locator('#canvas-text a', { hasText: 'Outside' }).click();
  await expect(page.locator('#canvas-text .markdown-link-blocked-reason')).toHaveText('link target leaves the vault');
  await expect(page.locator('#canvas-text a', { hasText: 'Private Note' })).toHaveAttribute('href', /slopshell-wiki:/);
  await page.waitForTimeout(200);
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'project_create')).toBe(false);
});
