import { useEffect, useState } from 'react'
import { api } from '../api'
import { LineChart, ms } from '../components'
import { srcUrl } from '../source'

function usePoll<T>(path: string, every: number): [T | null, () => void] {
  const [v, setV] = useState<T | null>(null)
  const [n, setN] = useState(0)
  useEffect(() => {
    let live = true
    const tick = () => api<T>(path).then((x) => live && setV(x)).catch(() => {})
    tick()
    const t = setInterval(tick, every)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [path, every, n])
  return [v, () => setN((x) => x + 1)]
}

// The on-call console: switch the fault, the load and the fix, and watch
// the same signals the runbook reads.
export function Console() {
  const [win] = usePoll<any>('/admin/window?seconds=90', 1000)
  const [db] = usePoll<any>('/admin/dbstats', 1000)
  const [mode, refreshMode] = usePoll<any>('/admin/mode', 3000)
  const [fault, refreshFault] = usePoll<any>('/admin/fault', 3000)
  const [traffic, refreshTraffic] = usePoll<any>('/admin/traffic', 3000)
  const [pg] = usePoll<any>('/admin/pg-activity', 2000)
  const [alerts] = usePoll<any>('/admin/alerts', 3000)
  const pts = (win?.series || []).filter((p: any) => p.n > 0)
  const last = pts.slice(-5)
  const p99 = last.length ? Math.max(...last.map((p: any) => p.p99_ms)) : 0
  const firing = (alerts?.data?.alerts || []).filter((a: any) => a.state === 'firing')
  const faultOn = fault?.latency_ms > 0
  const trafficOn = traffic?.search_rps > 0

  return (
    <>
      <h1>值班台</h1>
      <p className="lede">故障注入、流量和修复开关都在这里。下面的曲线、连接池和 Postgres 会话每秒刷新，和 runbook 读的是同一批信号。</p>
      <div className="grid3" style={{ marginBottom: 16 }}>
        <div className="card">
          <h3 style={{ marginTop: 0 }}>故障注入：quote 变慢</h3>
          <div className="row">
            <button className={'btn ' + (faultOn ? 'ghost' : 'hot')} data-testid="fault-on" onClick={() => api('/admin/fault', { latency_ms: 300, ratio: 1 }).then(refreshFault)}>+300ms</button>
            <button className="btn ghost" data-testid="fault-off" onClick={() => api('/admin/fault', { latency_ms: 0, ratio: 0 }).then(refreshFault)}>恢复</button>
            <span className={'pill ' + (faultOn ? 'hot' : 'green')} data-testid="fault-state">{faultOn ? `+${fault.latency_ms}ms` : '正常'}</span>
          </div>
        </div>
        <div className="card">
          <h3 style={{ marginTop: 0 }}>流量</h3>
          <div className="row">
            <button className={'btn ' + (trafficOn ? 'ghost' : 'green')} data-testid="traffic-on" onClick={() => api('/admin/traffic', { search_rps: 30, hold_rps: 25 }).then(refreshTraffic)}>搜索 30/s + 锁房 25/s</button>
            <button className="btn ghost" data-testid="traffic-off" onClick={() => api('/admin/traffic', { search_rps: 0, hold_rps: 0 }).then(refreshTraffic)}>停</button>
          </div>
        </div>
        <div className="card">
          <h3 style={{ marginTop: 0 }}>修复开关</h3>
          <div className="row">
            <button className="btn ghost" data-testid="mode-legacy" onClick={() => api('/admin/mode', { mode: 'legacy' }).then(refreshMode)}>legacy</button>
            <button className="btn green" data-testid="mode-fixed" onClick={() => api('/admin/mode', { mode: 'fixed' }).then(refreshMode)}>fixed</button>
            <span className={'pill ' + (mode?.mode === 'fixed' ? 'green' : 'hot')} data-testid="mode-state">{mode?.mode || '…'}</span>
            <a className="small src-link" href={srcUrl('fix-config')} target="_blank" rel="noreferrer">看源码</a>
          </div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 16 }}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h3 style={{ margin: 0 }}>搜索延迟（网关，每秒）</h3>
          <div className="row small">
            <span className={'stat ' + (p99 > 300 ? 'hot' : 'green')} style={{ flexDirection: 'row', gap: 6, alignItems: 'baseline' }}>
              <span className="k">最近五秒 p99</span>
              <span className="v" style={{ fontSize: 20 }} data-testid="live-p99">{ms(p99)}</span>
            </span>
            {win?.slowest_url && (
              <a href={win.slowest_url} target="_blank" rel="noreferrer" data-testid="slowest-trace">最慢一条 trace（{ms(win.slowest.ms)}）</a>
            )}
          </div>
        </div>
        <LineChart
          testid="live-chart"
          threshold={300}
          series={[
            { name: 'p99', color: '#b5502f', pts: pts.map((p: any) => ({ t: p.t, v: p.p99_ms })) },
            { name: 'p50', color: '#1f6f5c', pts: pts.map((p: any) => ({ t: p.t, v: p.p50_ms })) },
          ]}
        />
      </div>

      <div className="grid2">
        <div className="card">
          <h3 style={{ marginTop: 0 }}>inventory 连接池（sql.DBStats）</h3>
          {db && (
            <div className="grid3">
              <div className={'stat ' + (db.in_use >= db.max_open ? 'hot' : 'green')}><span className="v" data-testid="pool-inuse">{db.in_use}/{db.max_open}</span><span className="k">InUse / MaxOpen</span></div>
              <div className="stat"><span className="v">{db.wait_count}</span><span className="k">WaitCount（累计）</span></div>
              <div className="stat"><span className="v">{ms(db.wait_duration_ms)}</span><span className="k">WaitDuration（累计）</span></div>
            </div>
          )}
          <h3>正在响的告警</h3>
          {firing.length === 0 ? <p className="muted small" data-testid="alerts-none">没有</p> : firing.map((a: any) => (
            <div key={a.labels.alertname} className="row small" data-testid="alert-row"><span className="pill hot">firing</span><b>{a.labels.alertname}</b><span className="muted">{a.annotations.summary}</span></div>
          ))}
        </div>
        <div className="card">
          <h3 style={{ marginTop: 0 }}>Postgres 会话（pg_stat_activity）</h3>
          <table>
            <thead><tr><th>pid</th><th>state</th><th className="r">事务已开</th><th>语句</th></tr></thead>
            <tbody>
              {(pg?.sessions || []).slice(0, 8).map((x: any) => (
                <tr key={x.pid}><td className="num">{x.pid}</td><td><span className={'pill ' + (x.state === 'idle in transaction' ? 'hot' : 'grey')}>{x.state || '–'}</span></td><td className="r num">{ms(x.xact_age_ms)}</td><td><code className="small">{x.query.slice(0, 60)}</code></td></tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </>
  )
}
