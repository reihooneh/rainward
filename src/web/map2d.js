// The yard from above: covers shaded by height, where the water ran (blue, darker for more) and
// where it pooled, the buildings and downpipes, and the plan's changes labelled.

const COVER = ['#9cc47f', '#b7bcc0', '#7a5c3e', '#e8dccb', '#5b4630', '#a9cc8c'];

const rgb = (hex) => [1, 3, 5].map((k) => parseInt(hex.slice(k, k + 2), 16));
const COVER_RGB = COVER.map(rgb);
const WHITE = rgb('#ffffff');
const SHADOW = rgb('#203020');
const FLOW = rgb('#1d84b5');
const POND_LO = rgb('#5cc8e8');
const POND_HI = rgb('#0f4f78');
const mix = (a, b, t) => a.map((v, k) => v * (1 - t) + b[k] * t);
const css = (c) => `rgb(${c.map((v) => Math.round(v)).join(' ')})`;

export class YardMap {
  constructor(canvas) {
    this.canvas = canvas;
    new ResizeObserver(() => this.draw()).observe(canvas);
  }

  show(res, which) {
    this.res = res;
    this.which = which;
    this.draw();
  }

  draw() {
    const res = this.res;
    if (!res || !this.canvas.clientWidth) return;
    const pic = this.which === 'plan' ? res.after : res.before;
    const W = res.lot.width;
    const D = res.lot.depth;
    const cssW = this.canvas.clientWidth;
    const scale = Math.min((cssW - 24) / W, 620 / D);
    const cssH = Math.round(D * scale + 24);
    this.canvas.style.height = `${cssH}px`;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    this.canvas.width = Math.round(cssW * dpr);
    this.canvas.height = Math.round(cssH * dpr);
    const ctx = this.canvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, cssW, cssH);
    // The street is at the bottom: y grows upwards on the page.
    const ox = (cssW - W * scale) / 2;
    const oy = 12 + D * scale;
    const X = (x) => ox + x * scale;
    const Y = (y) => oy - y * scale;
    const s = pic.cell * scale;

    let maxFlow = 1;
    for (const f of pic.flow) maxFlow = Math.max(maxFlow, f);
    let lo = Infinity; let hi = -Infinity;
    for (const z of pic.z) { lo = Math.min(lo, z); hi = Math.max(hi, z); }
    for (let i = 0; i < pic.nx * pic.ny; i++) {
      const cx = i % pic.nx;
      const cy = Math.floor(i / pic.nx);
      const k = pic.kind[i];
      let colour = COVER_RGB[k];
      if (k !== 3) {
        // Hillshade: compare with the cell up-left.
        const j = Math.max(0, i - 1 - pic.nx);
        const shade = Math.max(-1, Math.min(1, (pic.z[i] - pic.z[j]) / 40));
        colour = shade > 0 ? mix(colour, WHITE, shade * 0.25) : mix(colour, SHADOW, -shade * 0.25);
        const flow = pic.flow[i];
        if (flow > 30) colour = mix(colour, FLOW, Math.min(0.85, 0.25 + 0.6 * Math.log(flow / 30) / Math.log(maxFlow / 30 + 1)));
        if (pic.depth[i] > 3) colour = mix(POND_LO, POND_HI, Math.min(1, pic.depth[i] / 150));
      }
      ctx.fillStyle = css(colour);
      ctx.fillRect(X(cx * pic.cell), Y((cy + 1) * pic.cell), s + 0.5, s + 0.5);
    }

    // Buildings and downpipes.
    ctx.lineWidth = 1.5;
    for (const b of res.lot.buildings) {
      ctx.fillStyle = b.home ? '#e8dccb' : '#d8d0c3';
      ctx.strokeStyle = '#8a7a66';
      ctx.fillRect(X(b.x), Y(b.y + b.d), b.w * scale, b.d * scale);
      ctx.strokeRect(X(b.x), Y(b.y + b.d), b.w * scale, b.d * scale);
    }
    ctx.font = `600 ${Math.max(10, Math.min(13, scale * 0.9))}px 'Atkinson Hyperlegible Next', sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    for (const p of res.pipes) {
      ctx.fillStyle = '#4a5560';
      ctx.beginPath();
      ctx.arc(X(p.x), Y(p.y), Math.max(3, scale * 0.22), 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = '#12263a';
      ctx.fillText(p.id, X(p.x - p.nx * 0.9), Y(p.y - p.ny * 0.9));
    }

    // The street edge.
    ctx.fillStyle = '#4a5d6e';
    ctx.fillRect(X(0), oy + 2, W * scale, 4);

    if (this.which !== 'plan') return;
    for (const a of res.actions) {
      ctx.strokeStyle = '#e2a72e';
      ctx.fillStyle = '#e2a72e';
      ctx.lineWidth = Math.max(2, scale * 0.12);
      if (a.kind === 'garden' || a.kind === 'grow') {
        const h = a.size / 2;
        ctx.strokeRect(X(a.x - h), Y(a.y + h), a.size * scale, a.size * scale);
        this.tag(ctx, a.label, X(a.x), Y(a.y), scale);
      } else if (a.kind === 'extend' || a.kind === 'pipe') {
        ctx.setLineDash(a.kind === 'pipe' ? [6, 4] : []);
        ctx.beginPath();
        ctx.moveTo(X(a.x), Y(a.y));
        ctx.lineTo(X(a.x2), Y(a.y2));
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.beginPath();
        ctx.arc(X(a.x2), Y(a.y2), Math.max(3, scale * 0.18), 0, Math.PI * 2);
        ctx.fill();
      } else if (a.kind === 'tank') {
        ctx.fillStyle = '#3f6f8a';
        ctx.beginPath();
        ctx.arc(X(a.x), Y(a.y), Math.max(6, scale * 0.6), 0, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = '#fff';
        ctx.fillText('T', X(a.x), Y(a.y));
      } else if (a.kind === 'regrade') {
        ctx.lineWidth = Math.max(4, scale * 0.3);
        ctx.beginPath();
        ctx.moveTo(X(a.x), Y(a.y));
        ctx.lineTo(X(a.x2), Y(a.y2));
        ctx.stroke();
      }
    }
  }

  tag(ctx, text, x, y, scale) {
    const r = Math.max(9, scale * 0.55);
    ctx.fillStyle = '#e2a72e';
    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = '#12263a';
    ctx.fillText(text, x, y);
  }
}
