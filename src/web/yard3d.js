// The yard in 3D: the ground (heights exaggerated so slopes show), what covers it, the buildings
// and downpipes, the plan's rain gardens, tanks, pipes and regrading, and the storm's water
// rising, running and pooling minute by minute, with rain falling while it lasts.
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { decodeFrame } from './engine.js';

const VERT = 4; // vertical exaggeration
const COVER = [0x8fbf6e, 0xb7bcc0, 0x7a5c3e, 0xe8dccb, 0x4a3a28, 0xa3c784]; // lawn, paving, garden, building, rain garden, berm
const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export class YardScene {
  constructor(canvas, { interactive = true, spin = false } = {}) {
    this.canvas = canvas;
    this.renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true, preserveDrawingBuffer: true });
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    this.scene = new THREE.Scene();
    this.camera = new THREE.PerspectiveCamera(38, 1, 0.1, 400);
    this.scene.add(new THREE.HemisphereLight(0xeaf4ff, 0x5a6b4a, 1.1));
    const sun = new THREE.DirectionalLight(0xffffff, 1.4);
    sun.position.set(-20, 40, -10);
    this.scene.add(sun);
    this.pivot = new THREE.Group();
    this.scene.add(this.pivot);
    this.root = new THREE.Group();
    this.pivot.add(this.root);
    this.spin = spin && !reduced();
    if (interactive) {
      this.controls = new OrbitControls(this.camera, canvas);
      this.controls.enableDamping = true;
      this.controls.maxPolarAngle = Math.PI * 0.47;
      this.controls.minDistance = 8;
      this.controls.maxDistance = 120;
      this.controls.addEventListener('change', () => this.kick());
    }
    this.frames = [];
    this.time = 0;
    new ResizeObserver(() => this.resize()).observe(canvas);
    this.resize();
  }

  resize() {
    const w = this.canvas.clientWidth || 600;
    const h = this.canvas.clientHeight || 400;
    this.renderer.setSize(w, h, false);
    this.camera.aspect = w / h;
    this.camera.updateProjectionMatrix();
    this.kick();
  }

  /** Shows a planned lot: `which` is 'today' or 'plan'. */
  show(res, which) {
    const pic = which === 'plan' ? res.after : res.before;
    const watch = which === 'plan' ? res.watchPlan : res.watchToday;
    this.root.clear();
    const W = res.lot.width;
    const D = res.lot.depth;
    this.size = [W, D];
    this.root.position.set(-W / 2, 0, -D / 2);
    this.groundAt = (x, y) => {
      const cx = Math.max(0, Math.min(pic.nx - 1, Math.floor(x / pic.cell)));
      const cy = Math.max(0, Math.min(pic.ny - 1, Math.floor(y / pic.cell)));
      return (pic.z[cy * pic.nx + cx] / 1000) * VERT;
    };
    this.root.add(this.ground(pic));
    for (const b of res.lot.buildings) this.root.add(this.building(b, pic));
    for (const p of res.pipes) this.root.add(this.downpipe(p, res.lot.buildings[p.building]));
    if (which === 'plan') for (const a of res.actions) this.root.add(this.change(a));
    this.water = this.waterMesh(watch);
    this.root.add(this.water);
    this.rain = this.rainDrops(W, D);
    this.root.add(this.rain);
    this.frames = watch.frames;
    this.every = watch.every;
    this.decoded = new Map();
    this.watch = watch;
    this.rainMinutes = res.storm.minutes;
    if (!this.placed) this.place(W, D);
    this.setTime(this.time);
  }

  place(W, D) {
    const r = Math.max(W, D);
    this.camera.position.set(r * 0.42, r * 0.5, -r * 0.78);
    this.camera.lookAt(0, 0, 0);
    if (this.controls) {
      this.controls.target.set(0, 0, 0);
      this.controls.update();
    }
    this.placed = true;
  }

  ground(pic) {
    const { nx, ny, cell } = pic;
    const geo = new THREE.BufferGeometry();
    const pos = new Float32Array(nx * ny * 3);
    const col = new Float32Array(nx * ny * 3);
    const c = new THREE.Color();
    let lo = Infinity; let hi = -Infinity;
    for (const z of pic.z) { lo = Math.min(lo, z); hi = Math.max(hi, z); }
    for (let i = 0; i < nx * ny; i++) {
      const x = (i % nx + 0.5) * cell;
      const y = (Math.floor(i / nx) + 0.5) * cell;
      pos.set([x, (pic.z[i] / 1000) * VERT, y], i * 3);
      c.setHex(COVER[pic.kind[i]] ?? COVER[0]);
      const shade = 0.88 + 0.2 * ((pic.z[i] - lo) / Math.max(1, hi - lo));
      col.set([c.r * shade, c.g * shade, c.b * shade], i * 3);
    }
    const idx = [];
    for (let y = 0; y < ny - 1; y++) {
      for (let x = 0; x < nx - 1; x++) {
        const a = y * nx + x;
        idx.push(a, a + nx, a + 1, a + 1, a + nx, a + nx + 1);
      }
    }
    geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
    geo.setAttribute('color', new THREE.BufferAttribute(col, 3));
    geo.setIndex(idx);
    geo.computeVertexNormals();
    const mesh = new THREE.Mesh(geo, new THREE.MeshStandardMaterial({ vertexColors: true, roughness: 0.95, side: THREE.DoubleSide }));
    // A skirt of soil around the edge, so the lot reads as a block of ground.
    const base = new THREE.Mesh(new THREE.BoxGeometry(nx * cell, 0.6, ny * cell), new THREE.MeshStandardMaterial({ color: 0x6b5236, roughness: 1 }));
    base.position.set((nx * cell) / 2, (lo / 1000) * VERT - 0.31, (ny * cell) / 2);
    const g = new THREE.Group();
    g.add(mesh, base);
    return g;
  }

  building(b, pic) {
    const g = new THREE.Group();
    let floor = -Infinity;
    for (const [x, y] of [[b.x, b.y], [b.x + b.w, b.y], [b.x, b.y + b.d], [b.x + b.w, b.y + b.d]]) floor = Math.max(floor, this.groundAt(Math.min(x, pic.nx * pic.cell - 0.01), Math.min(y, pic.ny * pic.cell - 0.01)));
    const walls = new THREE.Mesh(new THREE.BoxGeometry(b.w, b.height, b.d), new THREE.MeshStandardMaterial({ color: b.home ? 0xe8dccb : 0xcfc6b8, roughness: 0.8 }));
    walls.position.set(b.x + b.w / 2, floor + b.height / 2 - 0.3, b.y + b.d / 2);
    const roof = new THREE.Mesh(new THREE.BoxGeometry(b.w + 0.5, 0.25, b.d + 0.5), new THREE.MeshStandardMaterial({ color: b.home ? 0x8a3f2c : 0x6f7378, roughness: 0.7 }));
    roof.position.set(b.x + b.w / 2, floor + b.height - 0.15, b.y + b.d / 2);
    g.add(walls, roof);
    return g;
  }

  downpipe(p, b) {
    const h = (b?.height ?? 3) + 0.2;
    const y0 = this.groundAt(p.x + p.nx * 0.3, p.y + p.ny * 0.3);
    const m = new THREE.Mesh(new THREE.CylinderGeometry(0.09, 0.09, h, 10), new THREE.MeshStandardMaterial({ color: 0x5f6b73, roughness: 0.5 }));
    m.position.set(p.x + p.nx * 0.12, y0 + h / 2 - 0.3, p.y + p.ny * 0.12);
    return m;
  }

  change(a) {
    const g = new THREE.Group();
    const gold = new THREE.MeshStandardMaterial({ color: 0xe2a72e, roughness: 0.5 });
    if (a.kind === 'tank') {
      const t = new THREE.Mesh(new THREE.CylinderGeometry(0.7, 0.7, 1.9, 24), new THREE.MeshStandardMaterial({ color: 0x3f6f8a, roughness: 0.6 }));
      t.position.set(a.x, this.groundAt(a.x, a.y) + 0.95, a.y);
      g.add(t);
    } else if (a.kind === 'extend' || a.kind === 'pipe') {
      const from = new THREE.Vector3(a.x, this.groundAt(a.x, a.y) + 0.12, a.y);
      const to = new THREE.Vector3(a.x2, this.groundAt(a.x2, a.y2) + 0.12, a.y2);
      const tube = new THREE.Mesh(new THREE.TubeGeometry(new THREE.LineCurve3(from, to), 8, 0.1, 8, false), gold);
      g.add(tube);
    } else if (a.kind === 'garden' || a.kind === 'grow') {
      // A few plants in the bowl.
      const leaf = new THREE.MeshStandardMaterial({ color: 0x3f7d34, roughness: 0.9 });
      const n = Math.round(a.size * a.size * 1.2);
      for (let k = 0; k < n; k++) {
        const u = ((k * 0.618) % 1) - 0.5;
        const v = ((k * 0.381 + 0.17) % 1) - 0.5;
        const x = a.x + u * a.size * 0.85;
        const y = a.y + v * a.size * 0.85;
        const s = new THREE.Mesh(new THREE.ConeGeometry(0.16, 0.55, 6), leaf);
        s.position.set(x, this.groundAt(x, y) + 0.27, y);
        g.add(s);
      }
    } else if (a.kind === 'regrade') {
      const from = new THREE.Vector3(a.x, this.groundAt(a.x, a.y) + 0.06, a.y);
      const to = new THREE.Vector3(a.x2, this.groundAt(a.x2, a.y2) + 0.06, a.y2);
      g.add(new THREE.Mesh(new THREE.TubeGeometry(new THREE.LineCurve3(from, to), 4, 0.07, 6, false), gold));
    }
    return g;
  }

  waterMesh(w) {
    const { nx, ny, cell } = w;
    const geo = new THREE.BufferGeometry();
    this.wPos = new Float32Array(nx * ny * 3);
    this.wCol = new Float32Array(nx * ny * 3);
    for (let i = 0; i < nx * ny; i++) {
      this.wPos.set([(i % nx + 0.5) * cell, -1, (Math.floor(i / nx) + 0.5) * cell], i * 3);
    }
    const idx = [];
    for (let y = 0; y < ny - 1; y++) {
      for (let x = 0; x < nx - 1; x++) {
        const a = y * nx + x;
        idx.push(a, a + nx, a + 1, a + 1, a + nx, a + nx + 1);
      }
    }
    geo.setAttribute('position', new THREE.BufferAttribute(this.wPos, 3));
    geo.setAttribute('color', new THREE.BufferAttribute(this.wCol, 3));
    geo.setIndex(idx);
    const mat = new THREE.MeshStandardMaterial({ vertexColors: true, transparent: true, opacity: 0.82, roughness: 0.15, metalness: 0.05, depthWrite: false });
    const mesh = new THREE.Mesh(geo, mat);
    mesh.renderOrder = 2;
    return mesh;
  }

  rainDrops(W, D) {
    const n = 900;
    const pos = new Float32Array(n * 3);
    for (let k = 0; k < n; k++) pos.set([((k * 0.7548) % 1) * W, ((k * 0.5698) % 1) * 14, ((k * 0.3141 + 0.2) % 1) * D], k * 3);
    const geo = new THREE.BufferGeometry();
    geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
    const pts = new THREE.Points(geo, new THREE.PointsMaterial({ color: 0x5c9fd0, size: 0.09, transparent: true, opacity: 0.75 }));
    pts.visible = false;
    return pts;
  }

  frame(k) {
    if (!this.decoded.has(k)) this.decoded.set(k, decodeFrame(this.frames[k]));
    return this.decoded.get(k);
  }

  /** Minutes since the rain started (0 to the end of the simulation). */
  minutes() { return this.time * this.every / 60; }
  get lastFrame() { return this.frames.length - 1; }
  /** The frame when the rain stops: the most water on the ground. */
  get rainEnd() { return Math.min(this.lastFrame, Math.round((this.rainMinutes * 60) / this.every)); }

  /** Shows the water at frame position t (fractional frames interpolate). */
  setTime(t) {
    if (!this.frames.length) return;
    this.time = Math.max(0, Math.min(this.lastFrame, t));
    const k = Math.floor(this.time);
    const f = this.time - k;
    const a = this.frame(k);
    const b = this.frame(Math.min(this.lastFrame, k + 1));
    const { z } = this.watch;
    for (let i = 0; i < a.length; i++) {
      const d = a[i] * (1 - f) + b[i] * f;
      const ground = (z[i] / 1000) * VERT;
      const wet = d > 0.002;
      this.wPos[i * 3 + 1] = wet ? ground + Math.max(d * VERT, 0.03) + 0.02 : ground - 0.08;
      const deep = Math.min(1, d / 0.12);
      this.wCol.set([0.36 - 0.28 * deep, 0.74 - 0.36 * deep, 0.9 - 0.25 * deep], i * 3);
    }
    this.water.geometry.attributes.position.needsUpdate = true;
    this.water.geometry.attributes.color.needsUpdate = true;
    this.water.geometry.computeVertexNormals();
    this.rain.visible = this.minutes() < this.rainMinutes && !reduced();
    this.kick();
  }

  kick() {
    if (this.pending) return;
    this.pending = true;
    requestAnimationFrame(() => this.draw());
  }

  draw() {
    this.pending = false;
    if (this.spin) this.pivot.rotation.y += 0.0012;
    if (this.rain?.visible) {
      const p = this.rain.geometry.attributes.position;
      for (let k = 0; k < p.count; k++) {
        let y = p.getY(k) - 0.35;
        if (y < 0) y += 14;
        p.setY(k, y);
      }
      p.needsUpdate = true;
    }
    const moving = this.controls ? this.controls.update() : false;
    this.renderer.render(this.scene, this.camera);
    if (this.spin || moving || this.rain?.visible || this.playing) this.kick();
  }
}
