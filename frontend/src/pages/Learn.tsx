import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { Json, LineChart, Video, ms, Link } from '../components'
import { srcUrl, anchorInfo } from '../source'
import { OVERVIEW_PART, STAGES } from '../learnStages'

type Stage = { id: string; title: string; status: 'pending' | 'active' | 'done' | 'failed'; data?: any; error?: string; started_at?: string; finished_at?: string }
type Run = { id: string; stages: Stage[]; waiting: boolean; finished: boolean }

// The page never animates on its own: every lit stage and every number
// comes from GET /api/learn/runs/{id}, polled once a second.
export function Learn() {
  const [run, setRun] = useState<Run | null>(null)
  const [sel, setSel] = useState<string>('alert')
  const [video, setVideo] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const follow = useRef(true)

  useEffect(() => {
    api<Run>('/learn/runs/latest').then(setRun).catch(() => {})
  }, [])

  useEffect(() => {
    if (!run || run.finished) return
    const t = setInterval(async () => {
      try {
        const r = await api<Run>('/learn/runs/' + run.id)
        setRun(r)
        if (follow.current) {
          const cur = [...r.stages].reverse().find((s) => s.status !== 'pending')
          if (cur) setSel(cur.id)
        }
      } catch (e: any) {
        setErr(e.message)
      }
    }, 1000)
    return () => clearInterval(t)
  }, [run?.id, run?.finished])

  async function start() {
    setErr('')
    follow.current = true
    try {
      setRun(await api<Run>('/learn/runs', {}))
      setSel('alert')
    } catch (e: any) {
      setErr(e.message)
    }
  }
  async function cancel() {
    if (run) await api(`/learn/runs/${run.id}/cancel`, {})
    setTimeout(async () => setRun(await api<Run>('/learn/runs/latest')), 300)
  }
  async function next() {
    follow.current = true
    if (run) setRun(await api<Run>(`/learn/runs/${run.id}/next`, {}))
  }

  const stage = run?.stages.find((s) => s.id === sel)
  const meta = STAGES.find((s) => s.id === sel)!
  const busy = run && !run.finished
  return (
    <>
      <div className="tabs">
        <Link to="/learn" className="on">排查流程</Link>
        <Link to="/learn/problems" testid="tab-problems">日常问题</Link>
        <Link to="/learn/interview" testid="tab-interview">面试题</Link>
      </div>
      <h1>教学模式 · 一次真实的排查</h1>
      <p className="lede">
        按下按钮，系统会在真的六个服务上制造一次连接池耗尽：quote 变慢、锁房事务攥着连接、搜索排队。然后一步一步取证、修复、验证。每一步的数据都是后端此刻真实返回的。
      </p>
      <div className="card row" style={{ justifyContent: 'space-between', marginBottom: 16 }}>
        <div className="row">
          <button className="btn hot" data-testid="learn-start" onClick={start} disabled={!!busy}>
            提交样例
          </button>
          <button className="btn ghost" data-testid="learn-next" onClick={next} disabled={!run?.waiting}>
            下一步 →
          </button>
          {busy && run?.waiting && (
            <button className="btn ghost small" data-testid="learn-cancel" onClick={cancel}>
              放弃这次
            </button>
          )}
          <span className="small muted" data-testid="learn-state">
            {!run ? '还没有运行' : run.finished ? `${run.id} 已完成` : run.waiting ? `${run.id} 停在这一步，读完点「下一步」` : `${run.id} 正在执行…`}
          </span>
          {err && <span className="pill hot">{err}</span>}
        </div>
        <button className="btn ghost small" data-testid="video-overview" onClick={() => setVideo(OVERVIEW_PART)}>
          看总览视频
        </button>
      </div>
      <div className="learn">
        <ol className="trail" data-testid="trail">
          {STAGES.map((m, i) => {
            const s = run?.stages.find((x) => x.id === m.id)
            return (
              <li
                key={m.id}
                className={'stage' + (sel === m.id ? ' sel' : '')}
                data-testid={'stage-' + m.id}
                data-status={s?.status || 'pending'}
                onClick={() => {
                  follow.current = false
                  setSel(m.id)
                }}
              >
                <span className="dot" />
                <div className="t">
                  {i + 1}. {m.title}
                </div>
                <div className="s">{s?.status === 'done' ? summaryLine(m.id, s.data) : s?.status === 'active' ? '进行中…' : s?.status === 'failed' ? s.error : '等待'}</div>
                <div className="acts">
                  <button data-testid={'video-btn-' + m.id} onClick={(e) => (e.stopPropagation(), setSel(m.id), setVideo(m.part))}>
                    看讲解视频
                  </button>
                  <a className="src-link" data-testid={'src-' + m.id} href={srcUrl(m.anchors[0])} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>
                    看源码
                  </a>
                </div>
              </li>
            )
          })}
        </ol>
        <section className="card detail" data-testid="stage-detail" data-stage={sel}>
          {video && (
            <div style={{ marginBottom: 14 }}>
              <div className="row small muted" style={{ justifyContent: 'space-between' }}>
                <span>讲解视频 · {video}</span>
                <button className="btn ghost small" onClick={() => setVideo(null)}>
                  关闭
                </button>
              </div>
              <Video src={`/videos/${video}.mp4`} testid="stage-video" />
            </div>
          )}
          <h2>{meta.title}</h2>
          <p className="muted" style={{ marginTop: 0 }}>{meta.why}</p>
          <div className="evidence" data-testid="evidence">
            {!stage || stage.status === 'pending' ? (
              <p className="muted">这一步还没跑到。</p>
            ) : stage.status === 'active' ? (
              <p className="muted" data-testid="evidence-wait">正在向后端取数据…</p>
            ) : stage.status === 'failed' ? (
              <p className="pill hot">{stage.error}</p>
            ) : (
              <Evidence id={stage.id} d={stage.data} />
            )}
          </div>
          {stage?.data && <Json data={stage.data} testid="raw-json" />}
          <h3>看源码</h3>
          <ul className="small" data-testid="src-list">
            {meta.anchors.map((a) => (
              <li key={a}>
                <a className="src-link" href={srcUrl(a)} target="_blank" rel="noreferrer" data-anchor={a}>
                  {anchorInfo(a).path} L{anchorInfo(a).start}–{anchorInfo(a).end}
                </a>{' '}
                <span className="muted">({a})</span>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </>
  )
}

function summaryLine(id: string, d: any): string {
  if (!d) return ''
  switch (id) {
    case 'alert':
      return `${d.alert?.alertname} firing，故障后 ${d.seconds_to_fire}s`
    case 'p99':
      return `平时 ${ms(d.calm_p99_ms)} → 峰值 ${ms(d.peak_p99_ms)}`
    case 'trace':
      return `等连接占 ${Math.round(100 * d.acquire_share)}%`
    case 'dbstats':
      return `InUse ${d.in_use}/${d.max_open}，每秒等 ${d.wait_count_per_s} 次`
    case 'culprit':
      return `${d.idle_in_transaction} 条 idle in transaction`
    case 'fix':
      return `${d.before?.mode} → ${d.after?.mode}`
    case 'verify':
      return `p99 ${ms(d.before?.p99_ms)} → ${ms(d.after?.p99_ms)}`
    case 'history':
      return `连接池耗尽：影响排第 ${d.cause?.impact_rank}`
    case 'runbook':
      return `${d.steps?.length} 步`
  }
  return ''
}

function Evidence({ id, d }: { id: string; d: any }) {
  switch (id) {
    case 'alert':
      return (
        <div>
          <div className="card" style={{ borderColor: 'var(--hot)', background: 'var(--hot-soft)' }} data-testid="alert-card">
            <div className="row">
              <span className="pill hot" data-testid="alert-state">{d.alert.state}</span>
              <b data-testid="alert-name">{d.alert.alertname}</b>
              <span className="pill grey">{d.alert.severity}</span>
            </div>
            <div style={{ marginTop: 6 }} data-testid="alert-summary">{d.alert.summary}</div>
            <div className="small muted">activeAt {d.alert.active_at} · runbook: {d.alert.runbook}</div>
          </div>
          <div className="kv" style={{ marginTop: 12 }}>
            <span className="k">同时在响</span>
            <span data-testid="alert-firing">{d.firing.join('、')}</span>
            <span className="k">注入的故障</span>
            <span className="num">quote +{d.fault.latency_ms}ms（比例 {d.fault.ratio}）</span>
            <span className="k">流量</span>
            <span className="num">搜索 {d.traffic.search_rps}/s，锁房 {d.traffic.hold_rps}/s</span>
            <span className="k">从故障到告警</span>
            <span className="num" data-testid="alert-seconds">{d.seconds_to_fire} 秒</span>
          </div>
        </div>
      )
    case 'p99': {
      const pts = (d.series as any[]).filter((p) => p.n > 0)
      return (
        <div>
          <div className="grid3" style={{ marginBottom: 10 }}>
            <div className="stat green"><span className="v" data-testid="p99-calm">{ms(d.calm_p99_ms)}</span><span className="k">故障前每秒 p99（中位数）</span></div>
            <div className="stat hot"><span className="v" data-testid="p99-peak">{ms(d.peak_p99_ms)}</span><span className="k">峰值 p99</span></div>
            <div className="stat"><span className="v">{d.spike_at - d.fault_at}s</span><span className="k">故障后多久越过 300ms</span></div>
          </div>
          <LineChart
            testid="p99-chart"
            threshold={300}
            series={[
              { name: 'p99', color: '#b5502f', pts: pts.map((p) => ({ t: p.t, v: p.p99_ms })) },
              { name: 'p50', color: '#1f6f5c', pts: pts.map((p) => ({ t: p.t, v: p.p50_ms })) },
            ]}
            marks={[{ t: d.fault_at, label: '注入故障', color: '#a77b12' }]}
          />
          <div className="small muted">红线 p99，绿线 p50，纵轴对数。数据来自网关每秒的延迟窗口。</div>
        </div>
      )
    }
    case 'trace': {
      const total = d.total_ms
      return (
        <div>
          <div className="row" style={{ marginBottom: 8 }}>
            <span className="small">trace</span>
            <code data-testid="trace-id">{d.trace_id}</code>
            <a className="small" href={d.jaeger_url} target="_blank" rel="noreferrer" data-testid="jaeger-link">在 Jaeger 打开</a>
          </div>
          <div className="wf" data-testid="waterfall">
            {d.spans.map((s: any, i: number) => (
              <div key={i} className={'r' + (s.name === 'db.pool.acquire' ? ' hotspan' : '')} data-testid={s.name === 'db.pool.acquire' ? 'span-acquire' : undefined}>
                <div style={{ paddingLeft: s.depth * 6 }}>
                  <b>{s.service}</b> {s.name}
                </div>
                <div className="lane">
                  <div className="bar" style={{ position: 'absolute', left: `${(100 * s.start_ms) / total}%`, width: `${Math.max(0.4, (100 * s.dur_ms) / total)}%` }} />
                </div>
                <div className="num" style={{ textAlign: 'right' }}>{ms(s.dur_ms)}</div>
              </div>
            ))}
          </div>
          <p>
            这条请求一共 <b className="num" data-testid="trace-total">{ms(total)}</b>，其中 <b className="num" style={{ color: 'var(--hot)' }} data-testid="trace-acquire">{ms(d.acquire_ms)}</b> 在 <code>db.pool.acquire</code>：等连接。真正的查询只有零点几毫秒。
          </p>
        </div>
      )
    }
    case 'dbstats':
      return (
        <div>
          <div className="grid3">
            <div className="stat hot"><span className="v" data-testid="db-inuse">{d.in_use} / {d.max_open}</span><span className="k">InUse / MaxOpen</span></div>
            <div className="stat hot"><span className="v" data-testid="db-waits">+{d.wait_count_per_s}</span><span className="k">一秒内新增 WaitCount</span></div>
            <div className="stat"><span className="v">{ms(d.avg_wait_per_wait)}</span><span className="k">这一秒里平均每次等多久</span></div>
          </div>
          <table style={{ marginTop: 12 }} data-testid="db-table">
            <thead><tr><th>采样</th><th className="r">in_use</th><th className="r">idle</th><th className="r">wait_count</th><th className="r">wait_duration</th></tr></thead>
            <tbody>
              {[d.first, d.second].map((x: any, i: number) => (
                <tr key={i}><td>{i ? '一秒后' : '第一次'}</td><td className="r num">{x.in_use}</td><td className="r num">{x.idle}</td><td className="r num">{x.wait_count}</td><td className="r num">{ms(x.wait_duration_ms)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    case 'culprit':
      return (
        <div>
          <p>
            <b className="num" data-testid="idle-count">{d.idle_in_transaction}</b> 条连接是 <code>idle in transaction</code>，等待事件 <code>Client:ClientRead</code>：数据库在等应用发下一条语句。
          </p>
          <table data-testid="pg-table">
            <thead><tr><th>pid</th><th>state</th><th className="r">事务已开</th><th>wait</th><th>最后一条语句</th></tr></thead>
            <tbody>
              {d.sessions.map((x: any) => (
                <tr key={x.pid} data-testid="pg-row"><td className="num">{x.pid}</td><td><span className={'pill ' + (x.state === 'idle in transaction' ? 'hot' : 'grey')}>{x.state}</span></td><td className="r num">{ms(x.xact_age_ms)}</td><td className="small">{x.wait_event}</td><td><code className="small">{x.query}</code></td></tr>
              ))}
            </tbody>
          </table>
          <p className="small muted">
            当前配置：quote_outside_tx = <b data-testid="cfg-txscope">{String(d.config?.config?.quote_outside_tx)}</b>。也就是说，锁房在事务里调 quote，quote 慢三百毫秒，这条连接就被攥三百毫秒。
          </p>
        </div>
      )
    case 'fix':
      return (
        <table data-testid="fix-table">
          <thead><tr><th>层</th><th>修复前</th><th>修复后</th></tr></thead>
          <tbody>
            {Object.keys(d.after.config).map((k) => (
              <tr key={k} data-testid={'fix-' + k}><td><code>{k}</code></td><td><span className="pill grey">{String(d.before.config[k])}</span></td><td><span className="pill green">{String(d.after.config[k])}</span></td></tr>
            ))}
          </tbody>
        </table>
      )
    case 'verify':
      return (
        <div>
          <table data-testid="verify-table">
            <thead><tr><th></th><th className="r">请求数</th><th className="r">p50</th><th className="r">p95</th><th className="r">p99</th><th className="r">错误率</th></tr></thead>
            <tbody>
              {[['修复前（尖峰里）', d.before, 'hot'], ['修复后（故障仍开着）', d.after, 'green']].map(([label, x, c]: any) => (
                <tr key={label} data-testid={c === 'hot' ? 'verify-before' : 'verify-after'}><td>{label}</td><td className="r num">{x.requests}</td><td className="r num">{ms(x.p50_ms)}</td><td className="r num">{ms(x.p95_ms)}</td><td className={'r num'} style={{ color: `var(--${c})`, fontWeight: 600 }} data-testid={c === 'hot' ? 'p99-before' : 'p99-after'}>{ms(x.p99_ms)}</td><td className="r num">{(100 * x.error_rate).toFixed(2)}%</td></tr>
              ))}
            </tbody>
          </table>
          <p>修复后这 {d.after_window_s} 秒里，连接池新增等待 <b className="num" data-testid="verify-waits">{d.pool_waits_during_after}</b> 次。</p>
        </div>
      )
    case 'history':
      return (
        <div>
          <p className="small"><span className="pill amber">合成数据</span> 半年告警历史由 analysis/gen_alerts.py 用固定种子生成。</p>
          <table data-testid="history-table">
            <thead><tr><th>按影响排名</th><th>原因</th><th className="r">次数</th><th className="r">总影响</th><th className="r">MTTR p50</th><th className="r">按次数排名</th></tr></thead>
            <tbody>
              {d.top.map((c: any) => (
                <tr key={c.cause} data-testid={'cause-' + c.cause} style={c.cause === 'db_pool_exhaustion' ? { background: 'var(--hot-soft)' } : undefined}><td className="num">#{c.impact_rank}</td><td><code>{c.cause}</code></td><td className="r num">{c.incidents}</td><td className="r num">{c.impact_hours} h</td><td className="r num">{c.mttr_p50_min} min</td><td className="r num">#{c.count_rank}</td></tr>
              ))}
            </tbody>
          </table>
          <p>
            告警规则 <code>InventoryDBPoolSaturated</code>：噪音率 {Math.round(100 * d.rule.noise_rate)}%，但每个 episode 平均响 <b className="num" data-testid="rule-refires">{d.rule.fires_per_episode}</b> 次 → <span data-testid="rule-action">{d.rule.action}</span>
          </p>
        </div>
      )
    case 'runbook':
      return (
        <div>
          <ol data-testid="runbook-steps">
            {d.steps.map((s: any) => (
              <li key={s.step}>{s.title} <span className="pill green small">本次第 {STAGES.findIndex((m) => m.id === s.stage) + 1} 步已做</span></li>
            ))}
          </ol>
          <Link to="/runbook" testid="runbook-link">打开完整 runbook →</Link>
        </div>
      )
  }
  return null
}
