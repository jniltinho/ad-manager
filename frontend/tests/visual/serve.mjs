// Static server for the visual tests: serves the built SPA (../web/dist) with the SPA fallback and, when
// API_TARGET is set (recording), proxies /api to the running panel.
import { createReadStream, existsSync, statSync } from 'node:fs'
import { createServer, request } from 'node:http'
import { extname, join, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'

// DIST overrides the build to serve (e.g. the base commit's, to record the baseline)
const root = process.env.DIST ? process.env.DIST.replace(/\/?$/, '/') : fileURLToPath(new URL('../../../web/dist/', import.meta.url))
const port = Number(process.env.PORT || 4810)
const target = process.env.API_TARGET ? new URL(process.env.API_TARGET) : null
const types = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
  '.png': 'image/png',
  '.json': 'application/json',
}

createServer((req, res) => {
  if (req.url.startsWith('/api/')) {
    if (!target) return res.writeHead(404).end('{}')
    const up = request(
      { host: target.hostname, port: target.port, path: req.url, method: req.method, headers: { ...req.headers, host: target.host } },
      (r) => {
        res.writeHead(r.statusCode, r.headers)
        r.pipe(res)
      },
    )
    req.pipe(up)
    return
  }
  let file = normalize(join(root, decodeURIComponent(req.url.split('?')[0])))
  if (!file.startsWith(root) || !existsSync(file) || statSync(file).isDirectory()) file = join(root, 'index.html')
  res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream', 'Cache-Control': 'no-store' })
  createReadStream(file).pipe(res)
}).listen(port, '127.0.0.1')
