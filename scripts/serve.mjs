// A tiny static server for the built site (used by the browser tests and for local preview).
// Serves only files inside site/, with a fixed list of content types.
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { extname, join, normalize, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('../site/', import.meta.url));
const TYPES = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.txt': 'text/plain; charset=utf-8', '.png': 'image/png',
  '.wasm': 'application/wasm', '.json': 'application/json; charset=utf-8',
};

export function serve(port = 0) {
  const server = createServer(async (req, res) => {
    try {
      let path = decodeURIComponent(new URL(req.url, 'http://x').pathname);
      if (path.endsWith('/')) path += 'index.html';
      const file = normalize(join(ROOT, path));
      if (!file.startsWith(ROOT.endsWith(sep) ? ROOT : ROOT + sep)) throw new Error('outside');
      if (!(await stat(file)).isFile()) throw new Error('not a file');
      const type = TYPES[extname(file)];
      if (!type) throw new Error('type');
      res.writeHead(200, { 'content-type': type, 'x-content-type-options': 'nosniff' });
      res.end(await readFile(file));
    } catch {
      res.writeHead(404, { 'content-type': 'text/plain' });
      res.end('Not found');
    }
  });
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const s = await serve(Number(process.env.PORT) || 4173);
  console.log(`http://127.0.0.1:${s.address().port}/`);
}
