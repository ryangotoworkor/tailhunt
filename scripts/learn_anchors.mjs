#!/usr/bin/env node
// Collect source anchors for the learn page's "看源码" links.
//
// A range is marked by two comment lines, and the markers sit OUTSIDE it:
//     // @learn:hold-fixed          <- marker line (not part of the range)
//     func (s *Server) ...          <- start
//     ...                           <- end
//     // @learn:end hold-fixed      <- marker line
// Output: frontend/src/generated/anchors.json = {id: {path, start, end}}
// with 1-based line numbers. Nobody types a line number by hand.
//
//   node scripts/learn_anchors.mjs           write the file
//   node scripts/learn_anchors.mjs --check   exit 1 if the file is stale
import { readFileSync, writeFileSync, readdirSync, mkdirSync, existsSync } from 'node:fs'
import { join, relative, extname, dirname, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

export const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
export const OUT = join(ROOT, 'frontend', 'src', 'generated', 'anchors.json')
const CODE = ['.go', '.py', '.sql', '.sh', '.yml', '.mjs', '.ts', '.tsx']
const SKIP = ['node_modules', '.git', '.venv', 'dist', 'bin', 'var', '.runtime', 'generated', 'test-results', 'playwright-report', '__pycache__']
const TAG = '@' + 'learn:' // split so this file's own source never looks like a marker
const OPEN = new RegExp(TAG + '([a-z0-9][a-z0-9-]*)\\s*$')
const CLOSE = new RegExp(TAG + 'end ([a-z0-9][a-z0-9-]*)\\s*$')

function files(dir, acc = []) {
  for (const ent of readdirSync(dir, { withFileTypes: true })) {
    if (SKIP.includes(ent.name)) continue
    const p = join(dir, ent.name)
    if (ent.isDirectory()) files(p, acc)
    else if (CODE.includes(extname(ent.name))) acc.push(p)
  }
  return acc
}

export function collectAnchors(root = ROOT) {
  const anchors = {}
  const problems = []
  for (const file of files(root)) {
    const path = relative(root, file).split(sep).join('/')
    const open = new Map()
    readFileSync(file, 'utf8').split('\n').forEach((text, i) => {
      const line = i + 1
      const c = text.match(CLOSE)
      const o = c ? null : text.match(OPEN)
      if (o) {
        if (open.has(o[1])) problems.push(`${path}:${line} ${o[1]} opened twice`)
        open.set(o[1], line)
      } else if (c) {
        const id = c[1]
        if (!open.has(id)) return problems.push(`${path}:${line} end of ${id} without a start`)
        if (anchors[id]) problems.push(`${path}:${line} ${id} already defined in ${anchors[id].path}`)
        anchors[id] = { path, start: open.get(id) + 1, end: line - 1 }
        open.delete(id)
      }
    })
    for (const [id, line] of open) problems.push(`${path}:${line} ${id} never closed`)
  }
  const sorted = {}
  for (const id of Object.keys(anchors).sort()) sorted[id] = anchors[id]
  return { anchors: sorted, problems }
}

export const render = (anchors) => JSON.stringify(anchors, null, 2) + '\n'

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { anchors, problems } = collectAnchors()
  if (problems.length) {
    console.error(problems.join('\n'))
    process.exit(1)
  }
  const text = render(anchors)
  if (process.argv.includes('--check')) {
    const now = existsSync(OUT) ? readFileSync(OUT, 'utf8') : ''
    if (now !== text) {
      console.error('anchors.json is stale; run node scripts/learn_anchors.mjs')
      process.exit(1)
    }
  } else {
    mkdirSync(dirname(OUT), { recursive: true })
    writeFileSync(OUT, text)
  }
  console.log(`anchors: ${Object.keys(anchors).length}`)
}
