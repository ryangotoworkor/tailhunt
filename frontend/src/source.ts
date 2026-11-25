import anchors from './generated/anchors.json'

type Anchor = { path: string; start: number; end: number }
const A = anchors as Record<string, Anchor>

export const REPO = import.meta.env.VITE_REPO_URL.replace(/\/$/, '')
export const SHA = import.meta.env.VITE_GIT_SHA

// A GitHub link to the anchored lines, pinned to the build's commit (never
// "main", so the line numbers cannot drift).
export function srcUrl(id: string): string {
  const a = A[id]
  if (!a) throw new Error('unknown anchor ' + id)
  return `${REPO}/blob/${SHA}/${a.path}#L${a.start}-L${a.end}`
}

export function anchorInfo(id: string): Anchor {
  return A[id]
}

// Markdown docs link source as relative paths, optionally with an anchor
// id as the fragment: (../../internal/inventory/hold.go#hold-fixed). On
// GitHub that opens the file; here it becomes a pinned line-range link.
export function resolveDocLink(href: string, docPath: string): string {
  if (/^(https?:|mailto:|#)/.test(href)) return href
  const [rel, frag] = href.split('#')
  const parts = docPath.split('/').slice(0, -1)
  for (const seg of rel.split('/')) {
    if (seg === '..') parts.pop()
    else if (seg && seg !== '.') parts.push(seg)
  }
  const path = parts.join('/')
  const inApp: Record<string, string> = { 'docs/problems.md': '/learn/problems', 'docs/interview.md': '/learn/interview', 'docs/runbook.md': '/runbook' }
  if (inApp[path]) return inApp[path] + (frag ? '#' + frag : '')
  if (frag && A[frag] && A[frag].path === path) return srcUrl(frag)
  if (/\.png$/.test(path)) return `/repo-file/${path}`
  return `${REPO}/blob/${SHA}/${path}${frag ? '#' + frag : ''}`
}
