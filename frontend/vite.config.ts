import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { execSync } from 'node:child_process'
import { cpSync, createReadStream, existsSync, statSync } from 'node:fs'
import { join, normalize } from 'node:path'

// The one place the repository URL is defined. Everything else (source
// links, docs links, video URLs) derives from it.
const REPO_URL = process.env.VITE_REPO_URL || 'https://github.com/ryangotoworkor/tailhunt'
const GIT_SHA =
  process.env.VITE_GIT_SHA ||
  (() => {
    try {
      return execSync('git rev-parse HEAD', { cwd: __dirname }).toString().trim()
    } catch {
      return 'HEAD'
    }
  })()

// Teaching videos live in ../docs/videos (added later by the narration
// pipeline). In dev they are served from there; in a build they are
// copied into dist/videos. A missing file is a plain 404, never the SPA
// index page, so the UI can show "视频待生成".
const ROOT = join(__dirname, '..')
const VIDEOS = join(ROOT, 'docs', 'videos')
// Charts that the docs embed (analysis/report/*.png) are served at
// /repo-file/<path> so a rendered doc shows them before anything is pushed.
const IMAGE_DIRS = ['analysis/report', 'docs/img']
function videos(): Plugin {
  return {
    name: 'tailhunt-videos',
    configureServer(server) {
      server.middlewares.use('/videos', (req, res) => {
        const rel = normalize(decodeURIComponent((req.url || '/').split('?')[0])).replace(/^(\.\.[/\\])+/, '')
        const file = join(VIDEOS, rel)
        if (!file.startsWith(VIDEOS) || !existsSync(file) || !statSync(file).isFile()) {
          res.statusCode = 404
          res.end('video not generated yet')
          return
        }
        res.setHeader('Content-Type', file.endsWith('.mp4') ? 'video/mp4' : 'application/octet-stream')
        res.setHeader('Content-Length', String(statSync(file).size))
        if (req.method === 'HEAD') return void res.end()
        createReadStream(file).pipe(res)
      })
      server.middlewares.use('/repo-file', (req, res) => {
        const rel = normalize(decodeURIComponent((req.url || '/').split('?')[0])).replace(/^[/\\]+/, '')
        const file = join(ROOT, rel)
        if (!IMAGE_DIRS.some((d) => file.startsWith(join(ROOT, d))) || !/\.png$/.test(file) || !existsSync(file)) {
          res.statusCode = 404
          return void res.end()
        }
        res.setHeader('Content-Type', 'image/png')
        createReadStream(file).pipe(res)
      })
    },
    closeBundle() {
      if (existsSync(VIDEOS)) cpSync(VIDEOS, join(__dirname, 'dist', 'videos'), { recursive: true })
      for (const d of IMAGE_DIRS) if (existsSync(join(ROOT, d))) cpSync(join(ROOT, d), join(__dirname, 'dist', 'repo-file', d), { recursive: true })
    },
  }
}

export default defineConfig({
  plugins: [react(), videos()],
  define: {
    'import.meta.env.VITE_REPO_URL': JSON.stringify(REPO_URL),
    'import.meta.env.VITE_GIT_SHA': JSON.stringify(GIT_SHA),
  },
  server: {
    port: 5188,
    strictPort: true,
    fs: { allow: ['..'] },
    proxy: { '/api': process.env.VITE_API || 'http://localhost:8880' },
  },
  preview: { port: 5188, strictPort: true, proxy: { '/api': process.env.VITE_API || 'http://localhost:8880' } },
})
