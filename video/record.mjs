#!/usr/bin/env node
// Screen-record the learn page walking one live investigation, paced by
// the narration. Per segment of video/narration.json:
//   1. run its action (a click frames the target first, pauses, then clicks);
//   2. draw a red frame (or ellipse) on exactly the element in "highlight";
//   3. hold for the segment's spoken length: video/durations.json {id: s},
//      or 4 s per segment when that file does not exist yet.
// Output: video/out/learn.webm (1600x900) and video/timeline.json with the
// real start of every segment relative to the recording, its part, and the
// target/frame rectangles that video/check_frames.mjs verifies.
//
//   ./scripts/demo.sh && node video/record.mjs
// Stop this project's tests first: they share Postgres and would add load.
import { chromium } from '@playwright/test'
import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync, renameSync, rmSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const BASE = process.env.TAILHUNT_UI || 'http://localhost:5188'
const SIZE = { width: 1600, height: 900 }
const segments = JSON.parse(readFileSync(join(HERE, 'narration.json'), 'utf8'))
const durFile = join(HERE, 'durations.json')
const durations = existsSync(durFile) ? JSON.parse(readFileSync(durFile, 'utf8')) : {}
const OUT = join(HERE, 'out')
rmSync(OUT, { recursive: true, force: true })
mkdirSync(OUT, { recursive: true })

const browser = await chromium.launch()
const context = await browser.newContext({ viewport: SIZE, deviceScaleFactor: 1, recordVideo: { dir: OUT, size: SIZE } })
const page = await context.newPage()
const t0 = Date.now()

// @learn:record-frame
// The frame is an overlay, not a style on the element, so it can hug a
// single number, follow the element while the page scrolls, and pulse in.
// Returns {target, frame} rectangles; a missing or zero-size target throws.
async function frame(selector, shape = 'box') {
  await page.evaluate(() => document.getElementById('th-frame')?.remove())
  if (!selector) return null
  const loc = page.locator(selector).first()
  await loc.waitFor({ state: 'visible', timeout: 20_000 })
  await loc.scrollIntoViewIfNeeded()
  await loc.evaluate((el, shape) => {
    if (!document.getElementById('th-frame-css')) {
      const css = document.createElement('style')
      css.id = 'th-frame-css'
      css.textContent = `
        @keyframes th-in { 0% { transform: scale(1.3); opacity: 0 } 60% { transform: scale(.97); opacity: 1 } 100% { transform: scale(1) } }
        #th-frame { position: fixed; z-index: 2147483000; pointer-events: none; border: 3px solid #e5383b;
                    box-shadow: 0 0 0 6px rgba(229,56,59,.25); animation: th-in .5s ease-out both; }`
      document.head.appendChild(css)
    }
    const r0 = el.getBoundingClientRect()
    if (r0.width === 0 || r0.height === 0) throw new Error('frame target has no size')
    const f = document.createElement('div')
    f.id = 'th-frame'
    document.body.appendChild(f)
    const place = () => {
      const r = el.getBoundingClientRect()
      const ell = shape === 'circle'
      const px = ell ? Math.max(12, r.width * 0.2) : 5
      const py = ell ? Math.max(9, r.height * 0.4) : 4
      f.style.left = r.left - px + 'px'
      f.style.top = r.top - py + 'px'
      f.style.width = r.width + 2 * px + 'px'
      f.style.height = r.height + 2 * py + 'px'
      f.style.borderRadius = ell ? '50%' : '7px'
      if (f.isConnected) requestAnimationFrame(place)
    }
    place()
  }, shape)
  await page.waitForTimeout(600) // let the pulse settle before measuring
  return loc.evaluate((el) => {
    const r = el.getBoundingClientRect()
    const f = document.getElementById('th-frame').getBoundingClientRect()
    const box = (x) => [x.left, x.top, x.width, x.height].map(Math.round)
    return { target: box(r), frame: box(f) }
  })
}
// @learn:end record-frame

// A bar at the bottom shows a link's URL, instead of leaving for GitHub.
async function caption(text) {
  await page.evaluate((text) => {
    let c = document.getElementById('th-caption')
    if (!c) {
      c = document.createElement('div')
      c.id = 'th-caption'
      c.style.cssText = 'position:fixed;left:24px;right:24px;bottom:18px;z-index:2147483001;background:#2b2a26;color:#f4e8c8;font:13px ui-monospace,Menlo,monospace;padding:9px 14px;border-radius:7px;word-break:break-all'
      document.body.appendChild(c)
    }
    c.textContent = text
    c.style.display = text ? 'block' : 'none'
  }, text)
}

async function act(action) {
  const [name, ...rest] = action.split(':')
  const arg = rest.join(':')
  switch (name) {
    case 'open': {
      await page.goto(BASE + '/login')
      await page.locator('input[name=password]').fill('oncall-demo')
      await page.getByRole('button', { name: '登录' }).click()
      await page.waitForURL('**/learn')
      // start from a quiet stack: no load, no fault, fixed code
      await page.evaluate(async () => {
        const h = { Authorization: 'Bearer ' + localStorage.getItem('tailhunt.token'), 'Content-Type': 'application/json' }
        const post = (p, b) => fetch('/api/admin/' + p, { method: 'POST', headers: h, body: JSON.stringify(b) })
        const last = await fetch('/api/learn/runs/latest', { headers: h }).then((r) => (r.ok ? r.json() : null))
        if (last && !last.finished) await fetch(`/api/learn/runs/${last.id}/cancel`, { method: 'POST', headers: h })
        await new Promise((r) => setTimeout(r, 500))
        await post('traffic', { search_rps: 0, hold_rps: 0 })
        await post('fault', { latency_ms: 0, ratio: 0 })
        await post('mode', { mode: 'fixed' })
      })
      await page.reload()
      await page.locator('[data-testid=learn-start]:not([disabled])').waitFor({ timeout: 20_000 })
      return
    }
    case 'click':
      await frame(arg)
      await page.waitForTimeout(900)
      await page.locator(arg).first().click()
      await page.waitForTimeout(300)
      return
    case 'wait':
      await page.locator(`[data-testid=stage-${arg}][data-status=done]`).waitFor({ timeout: 150_000 })
      await page.locator(`[data-testid=stage-detail][data-stage=${arg}]`).waitFor({ timeout: 10_000 })
      await page.waitForTimeout(400)
      return
    case 'source': {
      const a = page.locator(arg).first()
      await a.scrollIntoViewIfNeeded()
      await a.hover()
      await caption('看源码 → ' + (await a.getAttribute('href')))
      return
    }
    case 'goto':
      await page.goto(BASE + arg)
      await page.waitForLoadState('networkidle')
      return
    case 'show':
      return
    default:
      throw new Error('unknown action ' + action)
  }
}

// @learn:record-pacing
const timeline = []
for (const s of segments) {
  const startMs = Date.now() - t0
  await caption('')
  await act(s.action)
  const rects = s.highlight ? await frame(s.highlight, s.shape || 'box') : null
  const plannedMs = Math.round(1000 * (durations[s.id] ?? 4))
  const used = Date.now() - t0 - startMs
  // the narration starts at startMs; hold until it has been spoken, and at
  // least one second with the frame on screen
  await page.waitForTimeout(Math.max(1000, plannedMs - used))
  timeline.push({ id: s.id, part: s.part, startMs, durMs: Date.now() - t0 - startMs, plannedMs, highlight: s.highlight || null, shape: s.shape || 'box', rects })
  console.log(`${s.id} ${s.part.padEnd(12)} start ${(startMs / 1000).toFixed(1)}s  ${s.action}`)
}
// @learn:end record-pacing
await caption('')
await page.close()
await context.close()
await browser.close()
const webm = readdirSync(OUT).find((f) => f.endsWith('.webm'))
renameSync(join(OUT, webm), join(OUT, 'learn.webm'))
writeFileSync(join(HERE, 'timeline.json'), JSON.stringify({ video: 'video/out/learn.webm', size: SIZE, segments: timeline }, null, 2) + '\n')
console.log(`wrote video/out/learn.webm and video/timeline.json (${timeline.length} segments, ${((Date.now() - t0) / 1000).toFixed(0)} s)`)
