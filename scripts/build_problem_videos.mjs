#!/usr/bin/env node
// Build video/problems.walk.json (input of the line-by-line walkthrough
// renderer) from video/problems.src.json. Code is never pasted into the
// JSON: each problem names @learn anchors, code_from (after the fix) and
// before_from (before it), and the code is cut out of the source here.
// Checks, and fails on any violation:
//   - every highlighted line number is inside the cut (1-based);
//   - narration has no code symbols and no Arabic digits;
//   - 6..14 segments per problem (after + before together);
// and prints each segment's narration above the lines it highlights so a
// person can check the words match the code. Also checks
// video/narration.json against the same narration rules.
//
//   node scripts/build_problem_videos.mjs              build + check
//   node scripts/build_problem_videos.mjs --show <id>  print an anchor with line numbers
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { collectAnchors, ROOT } from './learn_anchors.mjs'

const { anchors, problems: scanErrors } = collectAnchors()
if (scanErrors.length) {
  console.error(scanErrors.join('\n'))
  process.exit(1)
}

// @learn:cut-anchor
function cut(from) {
  const a = anchors[from.anchor]
  if (!a) throw new Error(`unknown anchor ${from.anchor}`)
  if (from.path && from.path !== a.path) throw new Error(`anchor ${from.anchor} is in ${a.path}, not ${from.path}`)
  const lines = readFileSync(join(ROOT, a.path), 'utf8').split('\n').slice(a.start - 1, a.end)
  while (lines.length && !lines[lines.length - 1].trim()) lines.pop()
  const indent = Math.min(...lines.filter((l) => l.trim()).map((l) => l.match(/^\s*/)[0].length))
  return { code: lines.map((l) => l.slice(indent)).join('\n'), anchor: a }
}
// @learn:end cut-anchor

if (process.argv[2] === '--show') {
  const { code, anchor } = cut({ anchor: process.argv[3] })
  console.log(`# ${anchor.path} L${anchor.start}-L${anchor.end}`)
  code.split('\n').forEach((l, i) => console.log(String(i + 1).padStart(3) + ' | ' + l))
  process.exit(0)
}

// Spoken text: numbers in Chinese, no code punctuation. English words are fine.
const FORBIDDEN = /[0-9_=<>{}()[\]\\/*#`|;$&^~@%+]|->|\.[A-Za-z]/
let errors = 0
const fail = (m) => {
  console.error('FAIL ' + m)
  errors++
}

for (const s of JSON.parse(readFileSync(join(ROOT, 'video', 'narration.json'), 'utf8')))
  if (FORBIDDEN.test(s.text)) fail(`narration ${s.id}: "${s.text.match(FORBIDDEN)[0]}" in: ${s.text}`)

const src = JSON.parse(readFileSync(join(ROOT, 'video', 'problems.src.json'), 'utf8'))
const out = []
for (const p of src) {
  const after = cut(p.code_from)
  const before = p.before_from ? cut(p.before_from) : null
  const n = p.segments.length + (p.before_segments?.length || 0)
  if (n < 6 || n > 14) fail(`${p.id}: ${n} segments, want 6..14`)
  console.log(`\n=== ${p.id}  ${p.title}`)
  const check = (segs, code, label) => {
    const lines = code.split('\n')
    segs.forEach((s, i) => {
      if (FORBIDDEN.test(s.text)) fail(`${p.id} ${label}[${i}]: "${s.text.match(FORBIDDEN)[0]}" in: ${s.text}`)
      console.log(`  [${label} ${i}] ${s.text}`)
      for (const ln of s.lines) {
        if (ln < 1 || ln > lines.length) fail(`${p.id} ${label}[${i}]: line ${ln} outside 1..${lines.length}`)
        console.log(`        ${String(ln).padStart(3)} | ${lines[ln - 1] ?? '<out of range>'}`)
      }
    })
  }
  if (before) check(p.before_segments || [], before.code, 'before')
  check(p.segments, after.code, 'after')
  out.push({
    id: p.id,
    title: p.title,
    subtitle: p.subtitle,
    lang: p.lang,
    source: `${after.anchor.path}#L${after.anchor.start}-L${after.anchor.end}`,
    code: after.code,
    segments: p.segments,
    example_code: before ? before.code : '',
    example_segments: p.before_segments || [],
    example_source: before ? `${before.anchor.path}#L${before.anchor.start}-L${before.anchor.end}` : '',
  })
}
if (errors) {
  console.error(`\n${errors} problem(s); video/problems.walk.json not written`)
  process.exit(1)
}
writeFileSync(join(ROOT, 'video', 'problems.walk.json'), JSON.stringify(out, null, 2) + '\n')
console.log(`\nwrote video/problems.walk.json (${out.length} problems)`)
