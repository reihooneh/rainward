// Small helpers for building the page with DOM calls only (never HTML from strings).

export function el(tag, attrs = {}, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') e.className = v;
    else if (k === 'text') e.textContent = v;
    else e.setAttribute(k, v === true ? '' : String(v));
  }
  for (const k of kids) if (k !== null && k !== undefined) e.append(k);
  return e;
}

const SVGNS = 'http://www.w3.org/2000/svg';
export function svg(tag, attrs = {}, text) {
  const e = document.createElementNS(SVGNS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  if (text !== undefined) e.textContent = text;
  return e;
}

export const litres = (m3) => `${Math.round(m3 * 1000).toLocaleString('en-AU')} L`;
export const dollars = (v) => `$${Math.round(v).toLocaleString('en-AU')}`;
export const whole = (v) => Math.round(v).toLocaleString('en-AU');

export function pressed(group, value) {
  for (const b of group.querySelectorAll('button')) b.setAttribute('aria-pressed', String(b.dataset.value === value));
}
