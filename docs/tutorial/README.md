# tailhunt 教程

按「先做出最小能跑的版本，再一步步加功能」的顺序写。每章对应一个 git tag，`git checkout step-0N` 就能回到那一章结束时的代码。

| 章 | tag | 解决什么问题 | 视频 |
|---|---|---|---|
| [1. 六个服务先跑起来](01-six-services.md) | step-01 | 一套真的多服务系统，连接池上限从第一行就钉死 | p0-overview |
| [2. 登录、锁房事务和过期清理](02-auth-and-holds.md) | step-02 | 有写、有事务、有后台任务；「事务里调下游」在这里埋下 | p0-overview |
| [3. 三种信号](03-observability.md) | step-03 | trace、指标、请求日志，专门把「等连接」暴露出来 | p1-alert |
| [4. 把事故复现出来](04-reproduce-the-incident.md) | step-04 | 故障注入、开放模型压测、一个必须复现事故的测试 | p2-trace, p3-pool |
| [5. 分层修复](05-fix-in-layers.md) | step-05 | 超时、舱壁、挪出事务、报价超时加熔断；每层一个能失败的测试 | p4-fix |
| [6. pprof 找过度分配](06-pprof-hot-path.md) | step-06 | search 排序比较函数里的字符串分配 | p4-fix |
| [7. 日志和告警数据分析](07-log-and-alert-analysis.md) | step-07 | SQL + pandas：尖峰里时间花在哪；半年里什么最伤（合成数据） | p5-analysis |
| [8. runbook 和教学模式](08-runbook-and-teaching-mode.md) | step-08 | 照着做的清单，和能亲手点一遍的 /learn | p6-runbook |

视频文件（`docs/videos/<part>.mp4`）由配音流水线之后生成；在那之前链接是空的，web UI 里显示「视频待生成」。
