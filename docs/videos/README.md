# 讲解视频

每个教学阶段一段短片，`/learn` 页面里每个阶段旁边的「看讲解视频」播放的就是这里的文件。清单见 [`video/parts.json`](../../video/parts.json)：

| 文件 | 内容 | 对应教程 |
|---|---|---|
| [`p0-overview.mp4`](p0-overview.mp4) | 这次排查讲什么 | [01](../tutorial/01-six-services.md) |
| [`p1-alert.mp4`](p1-alert.mp4) | 告警触发 | [03](../tutorial/03-observability.md) |
| [`p2-trace.mp4`](p2-trace.mp4) | p99 曲线和一条慢 trace | [04](../tutorial/04-reproduce-the-incident.md) |
| [`p3-pool.mp4`](p3-pool.mp4) | 连接池和占着连接的事务 | [04](../tutorial/04-reproduce-the-incident.md) |
| [`p4-fix.mp4`](p4-fix.mp4) | 打开修复，故障不关再测一次 | [05](../tutorial/05-fix-in-layers.md) |
| [`p5-analysis.mp4`](p5-analysis.mp4) | 半年告警历史里的排名（合成数据） | [07](../tutorial/07-log-and-alert-analysis.md) |
| [`p6-runbook.mp4`](p6-runbook.mp4) | 沉淀成 runbook | [08](../tutorial/08-runbook-and-teaching-mode.md) |

画面由 `video/record.mjs` 在真实页面上录制（会话里的数据都是这次真实请求产生的），红框标出每一步该看的地方；旁白来自 `video/narration.json`。旁白是本地 TTS 合成的（CosyVoice 2 自带的示例音色，不调任何云端 API），成片用本地 whisper 逐段回验过念的是稿子上的话。

## 日常问题（`problems/`）

每个问题一段代码讲解：先看修复前的写法和现象，再看修复后的写法，讲到哪几行就用红框框出哪几行。内容来自 [`video/problems.walk.json`](../../video/problems.walk.json)（由 `scripts/build_problem_videos.mjs` 按锚点从源码生成），问题的来龙去脉见 [`docs/problems.md`](../problems.md)。

- [`q01-pool-exhaustion.mp4`](problems/q01-pool-exhaustion.mp4) — 锁房事务攥着连接去调报价
- [`q02-bulkhead-scope.mp4`](problems/q02-bulkhead-scope.mp4) — 舱壁包得太宽，锁房大面积失败
- [`q03-bash32-empty-array.mp4`](problems/q03-bash32-empty-array.mp4) — 失败对照脚本的对照组没跑
- [`q04-rank-prefix-quirk.mp4`](problems/q04-rank-prefix-quirk.mp4) — 等价性测试抓到旧排序的怪癖
- [`q05-jaeger-oom.mp4`](problems/q05-jaeger-oom.mp4) — 演练到一半，Jaeger 被 OOM 杀掉
- [`q06-awk-label-space.mp4`](problems/q06-awk-label-space.mp4) — 性能脚本说每个请求分配七十一兆
- [`q07-window-by-finish.mp4`](problems/q07-window-by-finish.mp4) — 修好了，页面却说九十九分位还是九秒
- [`q08-trace-root-late.mp4`](problems/q08-trace-root-late.mp4) — trace 瀑布图偶尔少了第一跳
- [`q09-naive-alert-ranking.mp4`](problems/q09-naive-alert-ranking.mp4) — 按告警数量排，会把精力花错地方
