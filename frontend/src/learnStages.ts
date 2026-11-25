// The investigation the learn page walks, stage by stage. Ids match the
// backend (internal/gateway/learn.go StageIDs) and the recorder.
export type StageMeta = {
  id: string
  title: string
  part: string // teaching video part, /videos/<part>.mp4
  why: string // what this step is for
  anchors: string[]
}

export const STAGES: StageMeta[] = [
  { id: 'alert', title: '告警触发', part: 'p1-alert', why: '先让事故真的发生：quote 慢三百毫秒，锁房每秒二十五个，等 Prometheus 的 SearchP99High 响。', anchors: ['stage-alert', 'alert-rules', 'quote-fault'] },
  { id: 'p99', title: '看 p99 曲线', part: 'p2-trace', why: '告警只说「超了」。曲线告诉你从哪一秒开始、峰值多高、平时多少。', anchors: ['stage-p99', 'obs-wrap'] },
  { id: 'trace', title: '打开一条慢 trace', part: 'p2-trace', why: '拿尖峰里最慢的一条请求，看六个服务里时间花在哪一段。', anchors: ['stage-trace', 'pool-acquire', 'search-fanout'] },
  { id: 'dbstats', title: '看 sql.DBStats', part: 'p3-pool', why: 'trace 说在等连接。连接池自己的计数器能证实：InUse 顶满，WaitCount 每秒在涨。', anchors: ['stage-dbstats', 'dbstats', 'pool-open'] },
  { id: 'culprit', title: '找到占着连接的事务', part: 'p3-pool', why: '池子满了，是谁占着？去 Postgres 那一侧看每条连接在干什么。', anchors: ['stage-culprit', 'pg-activity', 'hold-legacy'] },
  { id: 'fix', title: '打开修复', part: 'p4-fix', why: '运行时切到修复版：拿连接超时、舱壁、报价挪出事务、报价超时加熔断。', anchors: ['stage-fix', 'fix-config', 'hold-fixed', 'acquire-timeout', 'bulkhead'] },
  { id: 'verify', title: '修复前后对比', part: 'p4-fix', why: '故障还开着，quote 依然慢。只比较 p99 和连接池等待有没有回到正常。', anchors: ['stage-verify', 'test-fixed', 'sabotage'] },
  { id: 'history', title: '告警历史里的排名', part: 'p5-analysis', why: '这一次修好了。半年里这一类故障排第几？告警规则该怎么调？（合成数据）', anchors: ['stage-history', 'sql-cause-ranking', 'sql-link', 'rule-advice'] },
  { id: 'runbook', title: '沉淀成 runbook', part: 'p6-runbook', why: '把刚才每一步变成下次值班的人照着做的清单。', anchors: ['failing-controls', 'test-top-cause'] },
]

export const OVERVIEW_PART = 'p0-overview'
