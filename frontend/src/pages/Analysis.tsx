import summary from '../../../analysis/out/summary.json'
import reqlog from '../../../analysis/out/requestlog.json'
import { Bars, ms } from '../components'
import { srcUrl } from '../source'

const S: any = summary
const R: any = reqlog

// Six months of synthetic alert history, ranked by impact, plus the
// request-log analysis of this repo's own load tests.
export function Analysis() {
  const t = S.totals
  return (
    <>
      <h1>告警历史分析</h1>
      <p className="lede">
        <span className="pill amber" data-testid="synthetic-badge">合成数据</span> 这半年的事故和告警由 <code>analysis/gen_alerts.py</code> 用固定种子 20261125 生成，形状模仿真实值班记录（恢复时间对数正态、告警反复触发、噪音规则）。不包含任何真实公司的数据。SQL 用 Spark SQL 写法、在 DuckDB 上运行；pandas 做后处理。
      </p>
      <div className="grid3" style={{ marginBottom: 16 }}>
        <div className="card stat"><span className="v">{t.incidents}</span><span className="k">事故 · {t.since} 至 {t.until}</span></div>
        <div className="card stat"><span className="v" data-testid="dedup">{t.fires} → {t.episodes}</span><span className="k">告警 fire → 合并后的 episode（平均 {t.dedup_ratio} 次 fire 一个 episode）</span></div>
        <div className="card stat hot"><span className="v" data-testid="noise-rate">{(100 * t.noise_rate_episodes).toFixed(1)}%</span><span className="k">episode 没有对应事故（噪音）</span></div>
      </div>

      <div className="grid2">
        <div className="card">
          <h3 style={{ marginTop: 0 }}>按总影响排名（事故小时数）</h3>
          <Bars testid="cause-bars" unit=" h" rows={S.causes.map((c: any) => ({ label: c.cause, v: c.impact_hours, hot: c.impact_rank === 1, note: `${c.incidents} 次` }))} />
          <p className="small muted">
            <a className="src-link" href={srcUrl('sql-cause-ranking')} target="_blank" rel="noreferrer">看 SQL</a> · 按次数排第一的是 <code>{S.naive.top_cause_by_count}</code>，按影响排第一的是 <code data-testid="top-cause">{S.causes[0].cause}</code>。
          </p>
        </div>
        <div className="card">
          <h3 style={{ marginTop: 0 }}>排名表</h3>
          <table data-testid="cause-table">
            <thead><tr><th>#</th><th>原因</th><th className="r">次数</th><th className="r">影响</th><th className="r">MTTR p50</th><th className="r">p90</th><th className="r">按次数</th></tr></thead>
            <tbody>
              {S.causes.map((c: any) => (
                <tr key={c.cause}><td className="num">{c.impact_rank}</td><td><code>{c.cause}</code></td><td className="r num">{c.incidents}</td><td className="r num">{c.impact_hours}h</td><td className="r num">{c.mttr_p50_min}m</td><td className="r num">{c.mttr_p90_min}m</td><td className="r num">{c.count_rank}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <h2>哪些告警规则该调</h2>
      <div className="card">
        <table data-testid="rule-table">
          <thead><tr><th>规则</th><th className="r">fire</th><th className="r">episode</th><th className="r">每个 episode 响几次</th><th>噪音率</th><th className="r">抓到事故</th><th className="r">检测延迟 p50</th><th>建议</th></tr></thead>
          <tbody>
            {S.rules.map((r: any) => (
              <tr key={r.rule} data-testid={'rule-' + r.rule}>
                <td><code>{r.rule}</code></td><td className="r num">{r.fires}</td><td className="r num">{r.episodes}</td><td className="r num">{r.fires_per_episode}</td>
                <td style={{ minWidth: 110 }}><div style={{ background: '#efe8da', borderRadius: 3 }}><div style={{ width: `${100 * r.noise_rate}%`, height: 10, borderRadius: 3, background: r.noise_rate > 0.8 ? 'var(--hot)' : '#c9c2b2' }} /></div><span className="small num">{Math.round(100 * r.noise_rate)}%</span></td>
                <td className="r num">{r.incidents_caught}</td><td className="r num">{r.detect_p50_min ?? '–'}m</td><td className="small">{r.action}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="small muted">
          <a className="src-link" href={srcUrl('sql-rule-quality')} target="_blank" rel="noreferrer">规则质量 SQL</a> · <a className="src-link" href={srcUrl('sql-episodes')} target="_blank" rel="noreferrer">合并重复告警</a> · <a className="src-link" href={srcUrl('sql-link')} target="_blank" rel="noreferrer">按时间关联事故</a> · <a className="src-link" href={srcUrl('rule-advice')} target="_blank" rel="noreferrer">建议规则</a>
        </p>
      </div>

      <h2>请求日志：尖峰里时间花在哪</h2>
      <p className="lede">这一部分不是合成的：它读的是本仓库自己压测时六个服务写下的 JSON Lines 请求日志（{R.lines} 行，{R.from} 至 {R.until} UTC），按 trace_id 把网关和 inventory 的同一个请求拼起来。</p>
      <div className="grid3" style={{ marginBottom: 12 }}>
        <div className="card stat hot"><span className="v" data-testid="spike-windows">{R.spike_windows}/{R.windows}</span><span className="k">十秒窗口里 p99 超过 300ms 的</span></div>
        <div className="card stat hot"><span className="v" data-testid="spike-share">{(100 * R.spike_db_wait_share_median).toFixed(1)}%</span><span className="k">尖峰窗口里网关耗时花在等连接上的比例（中位数）</span></div>
        <div className="card stat green"><span className="v">{(100 * R.calm_db_wait_share_median).toFixed(1)}%</span><span className="k">平静窗口里的同一比例</span></div>
      </div>
      <div className="grid2">
        <div className="card"><img src="/repo-file/analysis/report/requestlog.png" alt="search p99 vs db wait" style={{ width: '100%' }} /></div>
        <div className="card">
          <table>
            <thead><tr><th>服务 · 路由</th><th className="r">请求</th><th className="r">p50</th><th className="r">p99</th></tr></thead>
            <tbody>
              {R.endpoints.map((e: any) => (
                <tr key={e.svc + e.route}><td className="small"><b>{e.svc}</b> {e.route}</td><td className="r num">{e.requests}</td><td className="r num">{ms(e.p50_ms)}</td><td className="r num">{ms(e.p99_ms)}</td></tr>
              ))}
            </tbody>
          </table>
          <p className="small muted"><a className="src-link" href={srcUrl('sql-trace-join')} target="_blank" rel="noreferrer">trace_id join</a> · <a className="src-link" href={srcUrl('sql-spike-windows')} target="_blank" rel="noreferrer">尖峰窗口</a> · <a className="src-link" href={srcUrl('sql-endpoint-latency')} target="_blank" rel="noreferrer">端点分位数</a> · <a className="src-link" href={srcUrl('pandas-correlate')} target="_blank" rel="noreferrer">pandas 相关性</a></p>
        </div>
      </div>
    </>
  )
}
