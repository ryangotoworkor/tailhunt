#!/usr/bin/env node
// After a recording, check every red frame in video/timeline.json against
// the element it was meant to frame: the frame must contain the target,
// must not be much larger than it (a frame around a whole panel when the
// narration talks about one number is the failure we care about), and the
// target must be on screen with a real size. One line per segment.
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const t = JSON.parse(readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'timeline.json'), 'utf8'))
let bad = 0
for (const s of t.segments) {
  if (!s.rects) {
    console.log(`  ${s.id}  (no frame)`)
    continue
  }
  const [tx, ty, tw, th] = s.rects.target
  const [fx, fy, fw, fh] = s.rects.frame
  const contains = fx <= tx + 1 && fy <= ty + 1 && fx + fw >= tx + tw - 1 && fy + fh >= ty + th - 1
  const slack = s.shape === 'circle' ? Math.max(30, 0.5 * Math.max(tw, th)) : 12
  const tight = fw - tw <= 2 * slack && fh - th <= 2 * slack
  const visible = th > t.size.height ? ty < t.size.height && ty + th > 0 : ty >= 0 && ty + th <= t.size.height && tx >= 0 && tx + tw <= t.size.width
  const ok = tw > 0 && th > 0 && contains && tight && visible
  if (!ok) bad++
  console.log(`${ok ? 'ok' : 'XX'} ${s.id} ${s.shape.padEnd(6)} target ${tw}x${th}@${tx},${ty}  frame +${fw - tw}x${fh - th}  ${s.highlight}`)
}
console.log(bad ? `\n${bad} frame(s) off target` : '\nall frames on target')
process.exit(bad ? 1 : 0)
