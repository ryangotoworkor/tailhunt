# 第 8 章 · 写成 runbook，再做成能点的教学模式

> 讲解视频：[p6-runbook.mp4](../videos/p6-runbook.mp4)（另外每个阶段的视频在 learn 页面里点「看讲解视频」）

## 这一章解决什么问题？

前七章的东西散在命令行里：curl、psql、Grafana、Jaeger、SQL。
下一次半夜被叫醒的人，不会去翻七章教程。他需要两样东西：

1. 一份**照着做的清单**：[docs/runbook.md](../runbook.md)；
2. 一个能**亲手点一遍**的地方，看每一步在真实系统里长什么样：web UI 的 `/learn`。

## runbook 怎么写才有用？

假设：runbook 失败的原因通常是「步骤是对的，但不知道看到什么算对」。
所以每一步都写三件事：**看什么、看到什么说明什么、下一步去哪**，并且贴真实输出。比如第 4 步：

> InUse 顶满 **并且** WaitCount 持续增长 **并且** trace 里等待在 acquire。三条都满足才算。只有 InUse 高而 WaitCount 不涨，说明池子刚好够用，不是瓶颈。

最后附一棵判断树，把「不是连接池」的分支也画出来，避免 runbook 只会解一种题。

## 教学模式为什么要后端驱动？

前端做一个定时器播动画很容易，但那就是假的。
这里点「提交样例」，后端的状态机（[learn.go#stage-alert](../../internal/gateway/learn.go#stage-alert) 开始的九个阶段）真的去做每一件事：

| 阶段 | 后端在做什么 | 页面显示的真实数据 |
|---|---|---|
| 告警触发 | 先健康跑八秒，再注入 quote +300ms，轮询 Prometheus 直到 SearchP99High firing | 告警 JSON、从故障到告警的秒数 |
| p99 曲线 | 读网关每秒窗口（[learn.go#stage-p99](../../internal/gateway/learn.go#stage-p99)） | 曲线、故障前 p99、峰值 |
| 慢 trace | 取最慢请求的 trace_id，从 Jaeger API 读回，等根 span 到齐（[learn.go#stage-trace](../../internal/gateway/learn.go#stage-trace)） | 瀑布图、acquire 占比 |
| DBStats | 隔一秒读两次 `/debug/dbstats`（[learn.go#stage-dbstats](../../internal/gateway/learn.go#stage-dbstats)） | InUse/MaxOpen、每秒新增等待 |
| 占连接的事务 | 读 pg_stat_activity 和当前配置（[learn.go#stage-culprit](../../internal/gateway/learn.go#stage-culprit)） | idle in transaction 的会话表 |
| 打开修复 | 先记下修复前的窗口，再切 fixed（[learn.go#stage-fix](../../internal/gateway/learn.go#stage-fix)） | 四个开关的前后对比 |
| 前后对比 | 等积压排空八秒，再测十秒（[learn.go#stage-verify](../../internal/gateway/learn.go#stage-verify)） | 前后 p50/p95/p99/错误率、修复后等待次数 |
| 告警历史 | 读 analysis/out/summary.json（[learn.go#stage-history](../../internal/gateway/learn.go#stage-history)） | 排名表（合成数据）、规则建议 |
| runbook | 列出这次跑过的 runbook 步骤 | 清单 |

每个阶段做完就停下，等页面点「下一步」。页面每秒轮询 `GET /api/learn/runs/{id}`，亮哪一步、显示什么数据，全部来自这个接口。

一次真跑的阶段摘要（浏览器里每个阶段下面那行小字）：

```
alert    SearchP99High firing，故障后 10s
p99      平时 7.3 ms → 峰值 6.58 s
trace    等连接占 100%
dbstats  InUse 6/6，每秒等 56 次
culprit  6 条 idle in transaction
fix      legacy → fixed
verify   p99 7.62 s → 10.1 ms
history  连接池耗尽：影响排第 1
runbook  9 步
```

## 「看源码」的行号为什么不会过期？

源码里用两行注释圈出一段：`@learn:<id>` 和 `@learn:end <id>`，标记行本身不算在范围里。
`scripts/learn_anchors.mjs` 扫描出 `{id: {path, start, end}}`，写进 `frontend/src/generated/anchors.json`。
链接格式是 `<VITE_REPO_URL>/blob/<构建时的 commit>/<path>#L<start>-L<end>`，钉在 commit 上，不写 main。

[check_anchors.mjs#anchor-refs](../../scripts/check_anchors.mjs#anchor-refs) 在 CI 里检查：生成文件没过期；前端、文档、视频脚本引用的每个 id 都存在；文档链接写的文件就是锚点所在的文件；每个锚点都被引用过。
文档里的写法是普通相对链接加锚点 id 作 fragment，例如 `../../internal/inventory/hold.go#hold-fixed`：在 GitHub 上点开是那个文件，在 web UI 里会被换成带行号的链接。

## e2e 测了什么？

`e2e/tailhunt.spec.ts` 用 Playwright headless 从界面点一遍真实后端：

- 点「提交样例」，九个阶段**依次**亮起（后面的阶段在前面完成之前不能先完成）；
- 每一步断言真实数据：告警名是 SearchP99High、瀑布图第一行是 gateway 且有 db.pool.acquire、InUse 等于 MaxOpen、至少一条 idle in transaction、修复后 p99 低于修复前……
- 每个「看源码」链接的 href 符合格式，并且打开本地文件确认行号范围存在；
- 面试题、日常问题标签页：展开一题，能看到答案和「视频待生成」。

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-08
./scripts/demo.sh                 # 起全部服务和 web UI（:5188）
npx playwright test -c e2e/playwright.config.ts
./scripts/stop.sh
```
