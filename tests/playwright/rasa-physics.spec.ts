import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = { type: string; action?: string; text?: string; [key: string]: unknown };

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function injectRasaModuleRef(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/rasa.js');
    (window as any).__rasaModule = mod;
    const canvasMod = await import('../../internal/web/static/canvas.js');
    (window as any).__canvasModule = canvasMod;
  });
}

async function showArtifact(page: Page) {
  await page.evaluate(() => {
    const canvasMod = (window as any).__canvasModule;
    canvasMod.renderCanvas({
      event_id: 'art-1',
      kind: 'text_artifact',
      title: 'test.txt',
      text: 'hello world',
    });
    const rasaMod = (window as any).__rasaModule;
    if (rasaMod?.hideRasa) rasaMod.hideRasa();
    const ct = document.getElementById('canvas-text');
    if (ct) {
      ct.style.display = '';
      ct.classList.add('is-active');
    }
    const app = (window as any)._taburaApp;
    if (app?.getState) {
      app.getState().hasArtifact = true;
    }
  });
}

async function clearArtifact(page: Page) {
  await page.evaluate(() => {
    const viewport = document.getElementById('canvas-viewport');
    if (viewport) {
      viewport.querySelectorAll('.canvas-pane').forEach((p: any) => {
        p.style.display = 'none';
        p.classList.remove('is-active');
      });
    }
    const app = (window as any)._taburaApp;
    if (app?.getState) {
      app.getState().hasArtifact = false;
    }
    const rasaMod = (window as any).__rasaModule;
    if (rasaMod?.showRasa) {
      rasaMod.showRasa(document.getElementById('canvas-viewport'));
    }
  });
}

// Minimal mock Three.js module providing enough API surface for rasa.js.
const MOCK_THREE_JS = `
class Vector3 {
  constructor(x, y, z) { this.x = x||0; this.y = y||0; this.z = z||0; }
  set(x, y, z) { this.x = x; this.y = y; this.z = z; return this; }
  copy(v) { this.x = v.x; this.y = v.y; this.z = v.z; return this; }
}

class Matrix4 {
  constructor() { this.elements = new Float32Array(16); this.identity(); }
  identity() {
    const e = this.elements;
    e.fill(0);
    e[0] = e[5] = e[10] = e[15] = 1;
    return this;
  }
  multiplyMatrices() { return this; }
  makePerspective() {
    const e = this.elements;
    e.fill(0);
    e[0] = 1; e[5] = 1; e[10] = -1; e[11] = -1; e[14] = -2;
    return this;
  }
}

class BufferAttribute {
  constructor(array, itemSize) {
    this.array = array;
    this.itemSize = itemSize;
    this.needsUpdate = false;
    this.count = array.length / itemSize;
  }
  setXYZ(i, x, y, z) {
    const o = i * this.itemSize;
    this.array[o] = x;
    this.array[o+1] = y;
    this.array[o+2] = z;
  }
}

class BufferGeometry {
  constructor() { this.attributes = {}; this.index = null; }
  setAttribute(name, attr) { this.attributes[name] = attr; return this; }
  computeVertexNormals() {}
  dispose() {}
}

class PlaneGeometry extends BufferGeometry {
  constructor(w, h, ws, hs) {
    super();
    const nx = (ws||1) + 1;
    const ny = (hs||1) + 1;
    const verts = new Float32Array(nx * ny * 3);
    for (let iy = 0; iy < ny; iy++) {
      for (let ix = 0; ix < nx; ix++) {
        const i = iy * nx + ix;
        verts[i*3] = (ix/(nx-1) - 0.5) * w;
        verts[i*3+1] = (0.5 - iy/(ny-1)) * h;
        verts[i*3+2] = 0;
      }
    }
    this.attributes.position = new BufferAttribute(verts, 3);
    this.attributes.normal = new BufferAttribute(new Float32Array(nx*ny*3), 3);
  }
}

class Object3D {
  constructor() {
    this.children = [];
    this.position = new Vector3();
    this.matrixWorld = new Matrix4();
  }
  add(child) { this.children.push(child); }
  lookAt() {}
}

class Scene extends Object3D {}

class Camera extends Object3D {
  constructor() {
    super();
    this.projectionMatrix = new Matrix4();
    this.projectionMatrixInverse = new Matrix4();
  }
  updateProjectionMatrix() {}
}

class PerspectiveCamera extends Camera {
  constructor(fov, aspect, near, far) {
    super();
    this.fov = fov;
    this.aspect = aspect || 1;
    this.near = near || 0.1;
    this.far = far || 100;
  }
  updateProjectionMatrix() {
    this.projectionMatrix.makePerspective();
    this.projectionMatrixInverse.identity();
  }
}

class Light extends Object3D {
  constructor(color, intensity) {
    super();
    this.color = color;
    this.intensity = intensity;
  }
}
class AmbientLight extends Light {}
class DirectionalLight extends Light {}

class Material { dispose() {} }
class MeshStandardMaterial extends Material {
  constructor(params) { super(); Object.assign(this, params || {}); }
}

class Mesh extends Object3D {
  constructor(geometry, material) {
    super();
    this.geometry = geometry;
    this.material = material;
  }
}

class WebGLRenderer {
  constructor(params) {
    this.domElement = document.createElement('canvas');
    this.domElement.width = 800;
    this.domElement.height = 600;
  }
  setSize(w, h) {
    this.domElement.width = w;
    this.domElement.height = h;
    this.domElement.style.width = w + 'px';
    this.domElement.style.height = h + 'px';
  }
  setPixelRatio() {}
  setClearColor() {}
  render() {}
  dispose() {}
}

export const DoubleSide = 2;
export {
  Scene, PerspectiveCamera, AmbientLight, DirectionalLight,
  PlaneGeometry, MeshStandardMaterial, Mesh, WebGLRenderer,
};
`;

function mockThreeRoute(page: Page) {
  return page.route(/cdn\.jsdelivr\.net.*three/, (route) => {
    route.fulfill({
      status: 200,
      contentType: 'text/javascript; charset=utf-8',
      body: MOCK_THREE_JS,
      headers: {
        'Access-Control-Allow-Origin': '*',
        'Content-Type': 'text/javascript; charset=utf-8',
      },
    });
  });
}

function blockThreeRoute(page: Page) {
  return page.route(/cdn\.jsdelivr\.net.*three/, (route) => {
    route.abort('connectionrefused');
  });
}

test.describe('rasa physics - paper canvas', () => {
  test('rasa mode injects a canvas.rasa-canvas into #canvas-viewport', async ({ page }) => {
    await mockThreeRoute(page);
    await waitReady(page);

    await page.waitForFunction(() => {
      const vp = document.getElementById('canvas-viewport');
      return vp && vp.querySelector('canvas.rasa-canvas') !== null;
    }, null, { timeout: 5_000 });

    const rasaCanvas = page.locator('#canvas-viewport canvas.rasa-canvas');
    await expect(rasaCanvas).toHaveCount(1);
  });

  test('loading an artifact removes the rasa canvas', async ({ page }) => {
    await mockThreeRoute(page);
    await waitReady(page);
    await injectRasaModuleRef(page);

    await page.waitForFunction(() => {
      const vp = document.getElementById('canvas-viewport');
      return vp && vp.querySelector('canvas.rasa-canvas') !== null;
    }, null, { timeout: 5_000 });

    await showArtifact(page);

    await page.waitForFunction(() => {
      const vp = document.getElementById('canvas-viewport');
      return !vp || !vp.querySelector('canvas.rasa-canvas');
    }, null, { timeout: 3_000 });

    const rasaCanvas = page.locator('#canvas-viewport canvas.rasa-canvas');
    await expect(rasaCanvas).toHaveCount(0);
  });

  test('clearing artifact restores the rasa canvas', async ({ page }) => {
    await mockThreeRoute(page);
    await waitReady(page);
    await injectRasaModuleRef(page);

    await page.waitForFunction(() => {
      const vp = document.getElementById('canvas-viewport');
      return vp && vp.querySelector('canvas.rasa-canvas') !== null;
    }, null, { timeout: 5_000 });

    await showArtifact(page);

    await page.waitForFunction(() => {
      const vp = document.getElementById('canvas-viewport');
      return !vp || !vp.querySelector('canvas.rasa-canvas');
    }, null, { timeout: 3_000 });

    await clearArtifact(page);

    await page.waitForFunction(() => {
      const vp = document.getElementById('canvas-viewport');
      return vp && vp.querySelector('canvas.rasa-canvas') !== null;
    }, null, { timeout: 5_000 });

    const rasaCanvas = page.locator('#canvas-viewport canvas.rasa-canvas');
    await expect(rasaCanvas).toHaveCount(1);
  });

  test('graceful degradation: no Three.js means no canvas, no errors', async ({ page }) => {
    await blockThreeRoute(page);
    await waitReady(page);

    await page.waitForTimeout(500);

    const rasaCanvas = page.locator('#canvas-viewport canvas.rasa-canvas');
    await expect(rasaCanvas).toHaveCount(0);

    const log = await getLog(page);
    const rejections = log.filter((e) => e.type === 'unhandled_rejection');
    expect(rejections).toHaveLength(0);
  });
});
