import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { marked } from 'marked'
import { resolveDocLink } from './source'
import { setToken } from './api'

// --- tiny router: history API, no dependency ---
export function usePath(): string {
  const [p, setP] = useState(location.pathname)
  useEffect(() => {
    const on = () => setP(location.pathname)
    addEventListener('popstate', on)
    addEventListener('tailhunt:nav', on)
    return () => {
      removeEventListener('popstate', on)
      removeEventListener('tailhunt:nav', on)
    }
  }, [])
  return p
}

export function go(to: string) {
  history.pushState(null, '', to)
  dispatchEvent(new Event('tailhunt:nav'))
}

export function Link({ to, children, className, testid }: { to: string; children: ReactNode; className?: string; testid?: string }) {
  return (
    <a
      href={to}
      className={className}
      data-testid={testid}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey) return
        e.preventDefault()
        go(to)
      }}
    >
      {children}
    </a>
  )
}

export function Shell({ path, user, children }: { path: string; user: string; children: ReactNode }) {
  const nav: [string, string, boolean?][] = [
    ['/console', '值班台'],
    ['/analysis', '告警分析'],
    ['/runbook', 'Runbook'],
    ['/learn', '教学模式'],
    ['/learn/problems', '日常问题', true],
    ['/learn/interview', '面试题', true],
  ]
  return (
    <div className="shell">
      <nav className="rail">
        <div className="brand">tailhunt</div>
        <div className="tag">hunting a p99 spike to a full connection pool</div>
        {nav.map(([to, label, sub]) => (
          <Link key={to} to={to} testid={'nav-' + to.split('/').pop()} className={(path === to ? 'on ' : '') + (sub ? 'sub' : '')}>
            {label}
          </Link>
        ))}
        <div className="who">
          {user}
          <button
            onClick={() => {
              setToken('')
              go('/login')
            }}
          >
            退出
          </button>
        </div>
      </nav>
      <main className="main">{children}</main>
    </div>
  )
}

// --- markdown with repo-aware links ---
export function Markdown({ text, docPath }: { text: string; docPath: string }) {
  const html = useMemo(() => renderMarkdown(text, docPath), [text, docPath])
  return <div className="doc" dangerouslySetInnerHTML={{ __html: html }} />
}

export function renderMarkdown(text: string, docPath: string): string {
  const renderer = new marked.Renderer()
  const link = renderer.link.bind(renderer)
  renderer.link = (tok) => {
    const href = resolveDocLink(tok.href, docPath)
    const out = link({ ...tok, href })
    return href.startsWith('http') ? out.replace('<a ', '<a target="_blank" rel="noreferrer" class="src-link" ') : out
  }
  const image = renderer.image.bind(renderer)
  renderer.image = (tok) => image({ ...tok, href: resolveDocLink(tok.href, docPath) })
  return marked.parse(text, { renderer, async: false }) as string
}

// --- video slot: plays /videos/... if the file exists, else says so ---
export function Video({ src, testid }: { src: string; testid?: string }) {
  const [ok, setOk] = useState<boolean | null>(null)
  useEffect(() => {
    let live = true
    setOk(null)
    fetch(src, { method: 'HEAD' })
      .then((r) => live && setOk(r.ok && (r.headers.get('content-type') || '').startsWith('video/')))
      .catch(() => live && setOk(false))
    return () => {
      live = false
    }
  }, [src])
  return (
    <div className="vid" data-testid={testid} data-video={ok === null ? 'checking' : ok ? 'ready' : 'missing'}>
      {ok ? <video src={src} controls preload="metadata" /> : ok === false ? <span>视频待生成 · {src}</span> : <span>…</span>}
    </div>
  )
}

// --- charts: hand-drawn SVG, nothing to install ---
export type Pt = { t: number; v: number }

export function LineChart({
  series,
  height = 180,
  threshold,
  marks = [],
  log = true,
  testid,
}: {
  series: { name: string; color: string; pts: Pt[] }[]
  height?: number
  threshold?: number
  marks?: { t: number; label: string; color: string }[]
  log?: boolean
  testid?: string
}) {
  const W = 720
  const H = height
  const pad = { l: 46, r: 10, t: 10, b: 22 }
  const all = series.flatMap((s) => s.pts)
  if (all.length < 2) return <div className="muted small">等待数据…</div>
  const t0 = Math.min(...all.map((p) => p.t))
  const t1 = Math.max(...all.map((p) => p.t))
  const vmax = Math.max(threshold ?? 0, ...all.map((p) => p.v), 1)
  const f = (v: number) => (log ? Math.log10(Math.max(v, 1)) : v)
  const ymax = f(vmax) * 1.05 || 1
  const x = (t: number) => pad.l + ((t - t0) / Math.max(t1 - t0, 1)) * (W - pad.l - pad.r)
  const y = (v: number) => pad.t + (1 - f(v) / ymax) * (H - pad.t - pad.b)
  const ticks = log ? [1, 10, 100, 1000, 10000].filter((v) => f(v) <= ymax) : [0, vmax / 2, vmax]
  return (
    <svg className="chart" viewBox={`0 0 ${W} ${H}`} width="100%" data-testid={testid}>
      {ticks.map((v) => (
        <g key={v}>
          <line x1={pad.l} x2={W - pad.r} y1={y(v)} y2={y(v)} stroke="#e1d7c3" />
          <text x={pad.l - 6} y={y(v) + 4} textAnchor="end">
            {v >= 1000 ? v / 1000 + 's' : Math.round(v) + 'ms'}
          </text>
        </g>
      ))}
      {threshold !== undefined && (
        <g>
          <line x1={pad.l} x2={W - pad.r} y1={y(threshold)} y2={y(threshold)} stroke="#b5502f" strokeDasharray="4 4" />
          <text x={W - pad.r} y={y(threshold) - 4} textAnchor="end" style={{ fill: '#b5502f' }}>
            告警线 {threshold}ms
          </text>
        </g>
      )}
      {marks.map((m) => (
        <g key={m.label}>
          <line x1={x(m.t)} x2={x(m.t)} y1={pad.t} y2={H - pad.b} stroke={m.color} strokeWidth={1.5} />
          <text x={x(m.t) + 4} y={pad.t + 10} style={{ fill: m.color }}>
            {m.label}
          </text>
        </g>
      ))}
      {series.map((s) => (
        <polyline key={s.name} fill="none" stroke={s.color} strokeWidth={2} points={s.pts.map((p) => `${x(p.t)},${y(p.v)}`).join(' ')} />
      ))}
      <text x={pad.l} y={H - 4}>
        {new Date(t0 * 1000).toLocaleTimeString()}
      </text>
      <text x={W - pad.r} y={H - 4} textAnchor="end">
        {new Date(t1 * 1000).toLocaleTimeString()}
      </text>
    </svg>
  )
}

export function Bars({ rows, unit, testid }: { rows: { label: string; v: number; hot?: boolean; note?: string }[]; unit: string; testid?: string }) {
  const max = Math.max(...rows.map((r) => r.v), 1)
  return (
    <div data-testid={testid}>
      {rows.map((r) => (
        <div key={r.label} className="row" style={{ gap: 8, margin: '3px 0', flexWrap: 'nowrap' }}>
          <div style={{ width: 190, fontSize: 13, fontFamily: 'var(--mono)' }}>{r.label}</div>
          <div style={{ flex: 1, background: '#efe8da', borderRadius: 3 }}>
            <div style={{ width: `${(100 * r.v) / max}%`, height: 14, borderRadius: 3, background: r.hot ? 'var(--hot)' : '#c9c2b2' }} />
          </div>
          <div style={{ width: 150, fontSize: 12.5 }} className="num">
            {r.v}
            {unit} {r.note && <span className="muted">· {r.note}</span>}
          </div>
        </div>
      ))}
    </div>
  )
}

export function Json({ data, label = '看原始数据', testid }: { data: unknown; label?: string; testid?: string }) {
  return (
    <details className="json" data-testid={testid}>
      <summary>{label}</summary>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </details>
  )
}

export function ms(v: number | undefined | null): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '–'
  if (v >= 1000) return (v / 1000).toFixed(2) + ' s'
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + ' ms'
}
