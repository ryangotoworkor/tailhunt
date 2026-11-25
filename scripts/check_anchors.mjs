#!/usr/bin/env node
// Gate for every "看源码" link in the repo (CI runs it):
//   1. anchors.json is exactly what a fresh scan produces (not stale);
//   2. every anchor id referenced by the frontend, the docs and the video
//      sources exists, and a doc link's file is the anchor's file;
//   3. every range is non-empty, inside its file, and its first line is
//      real code (not a marker or a blank line);
//   4. every anchor is referenced at least once (no dead anchors).
import { readFileSync, readdirSync, existsSync } from 'node:fs'
import { join, dirname, resolve, relative, sep } from 'node:path'
import { collectAnchors, render, ROOT, OUT } from './learn_anchors.mjs'

const { anchors, problems } = collectAnchors()
const errors = [...problems]
if (!existsSync(OUT) || readFileSync(OUT, 'utf8') !== render(anchors)) errors.push('anchors.json is stale; run node scripts/learn_anchors.mjs')

const refs = new Map() // id -> [where]
const ref = (id, where, path) => {
  if (!refs.has(id)) refs.set(id, [])
  refs.get(id).push(where)
  if (!anchors[id]) errors.push(`${where}: unknown anchor ${id}`)
  else if (path && path !== anchors[id].path) errors.push(`${where}: links ${path} but anchor ${id} is in ${anchors[id].path}`)
}

// @learn:anchor-refs
// frontend: srcUrl('id') calls and anchors: ['a', 'b'] lists
const walk = (d) => readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? (e.name === 'generated' ? [] : walk(join(d, e.name))) : [join(d, e.name)]))
for (const f of walk(join(ROOT, 'frontend', 'src')).filter((f) => /\.tsx?$/.test(f))) {
  const text = readFileSync(f, 'utf8')
  const where = relative(ROOT, f)
  for (const m of text.matchAll(/srcUrl\('([a-z0-9-]+)'\)/g)) ref(m[1], where)
  for (const m of text.matchAll(/anchors:\s*\[([^\]]*)\]/g)) for (const id of m[1].matchAll(/'([a-z0-9-]+)'/g)) ref(id[1], where)
}
// docs: markdown links of the form [text](relative/path.go#anchor-id)
const docs = ['README.md', 'analysis/report.md', ...['docs', 'docs/tutorial'].flatMap((d) => (existsSync(join(ROOT, d)) ? readdirSync(join(ROOT, d)).filter((f) => f.endsWith('.md')).map((f) => `${d}/${f}`) : []))]
for (const doc of docs) {
  const text = readFileSync(join(ROOT, doc), 'utf8')
  for (const m of text.matchAll(/\]\(([^)\s#]+\.(?:go|py|sql|sh|yml|mjs|ts|tsx))#([a-z0-9-]+)\)/g)) {
    const target = relative(ROOT, resolve(join(ROOT, dirname(doc)), m[1])).split(sep).join('/')
    ref(m[2], doc, target)
  }
}
// video sources: the problem walkthroughs cut code by anchor
const psrc = join(ROOT, 'video', 'problems.src.json')
if (existsSync(psrc))
  for (const p of JSON.parse(readFileSync(psrc, 'utf8')))
    for (const from of [p.code_from, p.before_from].filter(Boolean)) ref(from.anchor, `problems.src.json ${p.id}`, from.path)
// @learn:end anchor-refs

for (const [id, a] of Object.entries(anchors)) {
  const lines = readFileSync(join(ROOT, a.path), 'utf8').split('\n')
  if (a.start > a.end) errors.push(`${id}: empty range in ${a.path}`)
  else if (a.end > lines.length) errors.push(`${id}: L${a.end} is past the end of ${a.path}`)
  else if (!lines[a.start - 1].trim() || lines[a.start - 1].includes('@' + 'learn:')) errors.push(`${id}: first line L${a.start} of ${a.path} is not code`)
  if (!refs.has(id)) errors.push(`${id}: defined in ${a.path} but never referenced`)
}

if (errors.length) {
  console.error(errors.map((e) => 'FAIL ' + e).join('\n'))
  process.exit(1)
}
console.log(`ok: ${Object.keys(anchors).length} anchors, ${refs.size} referenced ids, ${[...refs.values()].flat().length} references`)
