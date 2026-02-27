const THREE_CDN = 'https://cdn.jsdelivr.net/npm/three@0.172.0/build/three.module.min.js';
let THREE = null;
async function loadThree() {
  if (!THREE) THREE = await import(THREE_CDN);
  return THREE;
}

const COLS = 30;
const ROWS = 40;
const CLOTH_W = 8.5;
const CLOTH_H = 11;
const REST_DX = CLOTH_W / (COLS - 1);
const REST_DY = CLOTH_H / (ROWS - 1);
const DAMPING = 0.97;
const GRAVITY_Y = -0.0008;
const CONSTRAINT_ITERS = 15;
const INTERACT_RADIUS = 1.5;
const INTERACT_STRENGTH = 0.04;
const WIND_STRENGTH = 0.00012;
const DRAG_STRENGTH = 0.15;
const SCROLL_FORCE = 0.0006;

let scene = null;
let camera = null;
let renderer = null;
let mesh = null;
let geo = null;
let particles = null;
let rafId = 0;
let container = null;
let resizeOb = null;
let pointer = { x: 0, y: 0, active: false };
let dragging = false;
let dragIdx = -1;
let scrollAcc = 0;
let frameTime = 0;

function idx(col, row) {
  return row * COLS + col;
}

function makeParticles() {
  const n = COLS * ROWS;
  const px = new Float64Array(n);
  const py = new Float64Array(n);
  const pz = new Float64Array(n);
  const ox = new Float64Array(n);
  const oy = new Float64Array(n);
  const oz = new Float64Array(n);
  const pinned = new Uint8Array(n);
  for (let r = 0; r < ROWS; r++) {
    for (let c = 0; c < COLS; c++) {
      const i = idx(c, r);
      const x = c * REST_DX - CLOTH_W / 2;
      const y = -r * REST_DY + CLOTH_H / 2;
      px[i] = x;
      py[i] = y;
      pz[i] = 0;
      ox[i] = x;
      oy[i] = y;
      oz[i] = 0;
      if (r === 0) pinned[i] = 1;
    }
  }
  return { px, py, pz, ox, oy, oz, pinned, n };
}

function simulate(dt) {
  const p = particles;
  const t = frameTime;
  const windX = Math.sin(t * 0.7) * WIND_STRENGTH;
  const windZ = (Math.sin(t * 1.1) * 0.5 + 0.5) * WIND_STRENGTH;

  for (let i = 0; i < p.n; i++) {
    if (p.pinned[i]) continue;
    let vx = (p.px[i] - p.ox[i]) * DAMPING;
    let vy = (p.py[i] - p.oy[i]) * DAMPING;
    let vz = (p.pz[i] - p.oz[i]) * DAMPING;
    p.ox[i] = p.px[i];
    p.oy[i] = p.py[i];
    p.oz[i] = p.pz[i];
    p.px[i] += vx + windX;
    p.py[i] += vy + GRAVITY_Y;
    p.pz[i] += vz + windZ;
  }

  if (scrollAcc !== 0) {
    const force = scrollAcc * SCROLL_FORCE;
    for (let i = 0; i < p.n; i++) {
      if (p.pinned[i]) continue;
      p.pz[i] += force;
    }
    scrollAcc *= 0.85;
    if (Math.abs(scrollAcc) < 0.01) scrollAcc = 0;
  }

  for (let iter = 0; iter < CONSTRAINT_ITERS; iter++) {
    for (let r = 0; r < ROWS; r++) {
      for (let c = 0; c < COLS; c++) {
        const i = idx(c, r);
        if (c < COLS - 1) solveConstraint(p, i, idx(c + 1, r), REST_DX);
        if (r < ROWS - 1) solveConstraint(p, i, idx(c, r + 1), REST_DY);
      }
    }
  }
}

function solveConstraint(p, a, b, rest) {
  const dx = p.px[b] - p.px[a];
  const dy = p.py[b] - p.py[a];
  const dz = p.pz[b] - p.pz[a];
  const dist = Math.sqrt(dx * dx + dy * dy + dz * dz);
  if (dist < 1e-8) return;
  const diff = (dist - rest) / dist * 0.5;
  const cx = dx * diff;
  const cy = dy * diff;
  const cz = dz * diff;
  if (!p.pinned[a]) {
    p.px[a] += cx;
    p.py[a] += cy;
    p.pz[a] += cz;
  }
  if (!p.pinned[b]) {
    p.px[b] -= cx;
    p.py[b] -= cy;
    p.pz[b] -= cz;
  }
}

function applyInteraction(ray) {
  if (!particles || !ray) return;
  const p = particles;
  const plane = { nx: 0, ny: 0, nz: 1, d: 0 };
  const denom = ray.dx * plane.nz;
  if (Math.abs(denom) < 1e-8) return;
  const t = -ray.oz / denom;
  if (t < 0) return;
  const hx = ray.ox + ray.dx * t;
  const hy = ray.oy + ray.dy * t;
  const r2 = INTERACT_RADIUS * INTERACT_RADIUS;

  for (let i = 0; i < p.n; i++) {
    if (p.pinned[i]) continue;
    const ex = p.px[i] - hx;
    const ey = p.py[i] - hy;
    const d2 = ex * ex + ey * ey;
    if (d2 > r2) continue;
    const falloff = 1 - d2 / r2;
    if (dragging && dragIdx >= 0) {
      p.px[i] += (hx - p.px[i]) * DRAG_STRENGTH * falloff;
      p.py[i] += (hy - p.py[i]) * DRAG_STRENGTH * falloff;
    } else {
      p.pz[i] += INTERACT_STRENGTH * falloff;
    }
  }
}

function updateGeometry() {
  if (!geo || !particles) return;
  const pos = geo.attributes.position;
  const p = particles;
  for (let i = 0; i < p.n; i++) {
    pos.setXYZ(i, p.px[i], p.py[i], p.pz[i]);
  }
  pos.needsUpdate = true;
  geo.computeVertexNormals();
}

function buildScene(T) {
  scene = new T.Scene();
  camera = new T.PerspectiveCamera(30, 1, 0.1, 100);
  camera.position.set(0, 0, 35);
  camera.lookAt(0, 0, 0);

  const ambient = new T.AmbientLight(0xffffff, 0.9);
  scene.add(ambient);
  const dir = new T.DirectionalLight(0xffffff, 0.3);
  dir.position.set(5, 10, 7);
  scene.add(dir);

  particles = makeParticles();
  geo = new T.PlaneGeometry(CLOTH_W, CLOTH_H, COLS - 1, ROWS - 1);
  const mat = new T.MeshStandardMaterial({
    color: 0xffffff,
    side: T.DoubleSide,
    roughness: 0.85,
    metalness: 0.0,
  });
  mesh = new T.Mesh(geo, mat);
  scene.add(mesh);
}

function makeRay(cx, cy, w, h) {
  if (!camera) return null;
  const ndcX = (cx / w) * 2 - 1;
  const ndcY = -(cy / h) * 2 + 1;
  const near = unproject(ndcX, ndcY, -1);
  const far = unproject(ndcX, ndcY, 1);
  const dx = far[0] - near[0];
  const dy = far[1] - near[1];
  const dz = far[2] - near[2];
  const len = Math.sqrt(dx * dx + dy * dy + dz * dz);
  return {
    ox: near[0], oy: near[1], oz: near[2],
    dx: dx / len, dy: dy / len, dz: dz / len,
  };
}

function unproject(ndcX, ndcY, ndcZ) {
  if (!camera) return [0, 0, 0];
  const m = camera.projectionMatrixInverse.elements;
  const v = camera.matrixWorld.elements;
  const w = m[3] * ndcX + m[7] * ndcY + m[11] * ndcZ + m[15];
  let ex = (m[0] * ndcX + m[4] * ndcY + m[8] * ndcZ + m[12]) / w;
  let ey = (m[1] * ndcX + m[5] * ndcY + m[9] * ndcZ + m[13]) / w;
  let ez = (m[2] * ndcX + m[6] * ndcY + m[10] * ndcZ + m[14]) / w;
  const wx = v[0] * ex + v[4] * ey + v[8] * ez + v[12];
  const wy = v[1] * ex + v[5] * ey + v[9] * ez + v[13];
  const wz = v[2] * ex + v[6] * ey + v[10] * ez + v[14];
  return [wx, wy, wz];
}

function onPointerMove(e) {
  const rect = container.getBoundingClientRect();
  pointer.x = e.clientX - rect.left;
  pointer.y = e.clientY - rect.top;
  pointer.active = true;
}

function onPointerDown(e) {
  dragging = true;
  onPointerMove(e);
  if (container && particles) {
    const rect = container.getBoundingClientRect();
    const ray = makeRay(pointer.x, pointer.y, rect.width, rect.height);
    if (ray) {
      const denom = ray.dz;
      if (Math.abs(denom) > 1e-8) {
        const t = -ray.oz / denom;
        const hx = ray.ox + ray.dx * t;
        const hy = ray.oy + ray.dy * t;
        let best = -1;
        let bestD = Infinity;
        for (let i = 0; i < particles.n; i++) {
          if (particles.pinned[i]) continue;
          const ex = particles.px[i] - hx;
          const ey = particles.py[i] - hy;
          const d2 = ex * ex + ey * ey;
          if (d2 < bestD) { bestD = d2; best = i; }
        }
        dragIdx = best;
      }
    }
  }
}

function onPointerUp() {
  dragging = false;
  dragIdx = -1;
}

function onPointerLeave() {
  pointer.active = false;
  dragging = false;
  dragIdx = -1;
}

function onWheel(e) {
  e.preventDefault();
  scrollAcc += e.deltaY;
}

function onTouchMove(e) {
  if (e.touches.length === 1) {
    const rect = container.getBoundingClientRect();
    const touch = e.touches[0];
    const dy = touch.clientY - pointer.y;
    scrollAcc += dy * 2;
    pointer.x = touch.clientX - rect.left;
    pointer.y = touch.clientY - rect.top;
    pointer.active = true;
  }
}

function bindEvents(el) {
  el.addEventListener('pointermove', onPointerMove);
  el.addEventListener('pointerdown', onPointerDown);
  el.addEventListener('pointerup', onPointerUp);
  el.addEventListener('pointerleave', onPointerLeave);
  el.addEventListener('wheel', onWheel, { passive: false });
  el.addEventListener('touchmove', onTouchMove, { passive: true });
}

function unbindEvents(el) {
  el.removeEventListener('pointermove', onPointerMove);
  el.removeEventListener('pointerdown', onPointerDown);
  el.removeEventListener('pointerup', onPointerUp);
  el.removeEventListener('pointerleave', onPointerLeave);
  el.removeEventListener('wheel', onWheel);
  el.removeEventListener('touchmove', onTouchMove);
}

function resize() {
  if (!container || !renderer || !camera) return;
  const w = container.clientWidth;
  const h = container.clientHeight;
  if (w === 0 || h === 0) return;
  renderer.setSize(w, h);
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
}

function loop() {
  rafId = requestAnimationFrame(loop);
  frameTime += 1 / 60;
  simulate(1);
  if (pointer.active && container) {
    const rect = container.getBoundingClientRect();
    const ray = makeRay(pointer.x, pointer.y, rect.width, rect.height);
    applyInteraction(ray);
  }
  updateGeometry();
  renderer.render(scene, camera);
}

let pendingTarget = null;

export async function initRasa() {
  try {
    await loadThree();
  } catch (_) {
    THREE = null;
  }
  if (THREE && pendingTarget) {
    attachRasa(pendingTarget);
    pendingTarget = null;
  }
}

function attachRasa(target) {
  container = target;
  if (!scene) buildScene(THREE);

  if (!renderer) {
    renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setClearColor(0x000000, 0);
  }

  const canvas = renderer.domElement;
  canvas.classList.add('rasa-canvas');
  if (!canvas.parentNode) container.appendChild(canvas);
  canvas.style.opacity = '0';
  resize();

  resizeOb = new ResizeObserver(resize);
  resizeOb.observe(container);

  bindEvents(canvas);

  requestAnimationFrame(() => {
    canvas.style.transition = 'opacity 400ms ease';
    canvas.style.opacity = '1';
  });

  if (!rafId) loop();
}

export function showRasa(target) {
  if (!target) return;
  if (THREE) {
    pendingTarget = null;
    attachRasa(target);
  } else {
    pendingTarget = target;
  }
}

export function hideRasa() {
  pendingTarget = null;
  if (!renderer) return;
  const canvas = renderer.domElement;
  if (!canvas.parentNode) return;

  if (rafId) {
    cancelAnimationFrame(rafId);
    rafId = 0;
  }

  canvas.style.transition = 'opacity 300ms ease';
  canvas.style.opacity = '0';
  const cleanup = () => {
    canvas.removeEventListener('transitionend', cleanup);
    if (canvas.parentNode) canvas.parentNode.removeChild(canvas);
    unbindEvents(canvas);
    if (resizeOb) {
      resizeOb.disconnect();
      resizeOb = null;
    }
  };
  canvas.addEventListener('transitionend', cleanup, { once: true });
  setTimeout(cleanup, 350);
}
