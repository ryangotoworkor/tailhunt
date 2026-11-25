# 第 3 章 · 三种信号：trace、指标、请求日志

> 讲解视频：[p1-alert.mp4](../videos/p1-alert.mp4)（告警是怎么从这些信号里长出来的）

## 这一章解决什么问题？

线上出尖峰时，你手里只有三样东西：告警、dashboard、日志。
如果系统没埋点，就只能猜。这一章把三种信号都接上，并且**专门把「等连接」这件事暴露出来**。

| 信号 | 去哪 | 看什么 |
|---|---|---|
| trace（OpenTelemetry） | Jaeger :16698 | 一个请求在六个服务里各花了多久 |
| 指标（Prometheus） | Prometheus :9198 / Grafana :3098 | p99、连接池 InUse、WaitCount |
| 请求日志（JSON Lines） | `var/logs/<svc>.jsonl` | 每个请求一行，第 7 章用 SQL 分析 |

## 为什么默认的埋点看不到连接池等待？

假设：`database/sql` 的 `QueryContext` 里面会先「拿连接」，再「发查询」。
拿连接如果要排队，这段时间被**藏在 QueryContext 里**，trace 上只看到一个很长的查询 span。
你会误以为是 SQL 慢，去优化 SQL，白忙一场。

所以 [pool.go#pool-acquire](../../internal/inventory/pool.go) 显式调用 `db.Conn(ctx)` 拿连接，并且：

- 包一个 span `db.pool.acquire`，属性里带 `db.pool.wait_ms`、`in_use_at_start`、`max_open`；
- 记一个直方图 `tailhunt_db_acquire_seconds{path=availability|hold}`；
- 把等待时间写进这次请求的日志字段 `db_wait_ms`。

这样「等连接」和「跑查询」在 trace 里是两段，一眼分得开。

## sql.DBStats 是什么？

`db.Stats()` 是 Go 自带的连接池快照。最关键的四个字段：

- `MaxOpenConnections`：上限（我们设的六）；
- `InUse`：正在被借走的连接数；
- `WaitCount`：**累计**有多少次「来了没连接，只能等」；
- `WaitDuration`：累计等了多久。

`InUse == MaxOpen` 并且 `WaitCount` 在涨，就是连接池耗尽的直接证据。
我们用 client_golang 的 `collectors.NewDBStatsCollector` 把它导出成 `go_sql_*` 指标，
另外 inventory 的 `/debug/dbstats` 直接返回 JSON（[pool.go#dbstats](../../internal/inventory/pool.go)）。

## 真跑出来是什么样？

一次锁房的 trace（从 Jaeger API 取回，按开始时间排）：

```
gateway    POST /api/holds              4260 us
gateway    HTTP POST                    4127 us
inventory  POST /holds                  3419 us
inventory  db.pool.acquire              11 us       <- 现在几乎不用等
inventory  db.tx hold                   3243 us
inventory  HTTP GET                     1098 us     <- 事务里调 quote
quote      GET /quote                   360 us
```

注意 `HTTP GET` 是 `db.tx hold` 的**子 span**：trace 已经把「事务里调下游」画出来了。现在它只有一毫秒。

同一个请求在两个服务的日志，用 `trace_id` 对得上：

```
{"svc":"gateway","route":"POST /api/holds","status":201,"dur_ms":4.221,"db_wait_ms":0,"trace_id":"21e49d8b..."}
{"svc":"inventory","route":"POST /holds","status":201,"dur_ms":3.393,"db_wait_ms":0.003,"trace_id":"21e49d8b..."}
```

连接池快照：

```
$ curl -s localhost:8882/debug/dbstats
{"max_open":6,"open":1,"in_use":0,"idle":1,"wait_count":0,"wait_duration_ms":0,...}
```

## 诊断查询为什么要单独一个池子？

[pool.go#pg-activity](../../internal/inventory/pool.go) 查 `pg_stat_activity`，看每条连接在 Postgres 那边是什么状态。
它跑在一个**只有一条连接的独立池子**上（application_name 是 `tailhunt-diag`）。
如果它和业务共用池子，池子满的时候它自己也要排队，你最需要它的时候它最慢。

测试 `TestSlowQuoteShowsAsIdleInTransaction` 让 quote 慢六百毫秒，然后断言：
pg_stat_activity 里能看到一条 `idle in transaction` 的会话，查询是 `SELECT ... FOR UPDATE`，事务已经开了两百毫秒以上。
这就是第 4 章 runbook 里要找的「特征」。

## 告警规则

[rules.yml#alert-rules](../../deploy/prometheus/rules.yml) 里两条：

- `SearchP99High`：网关的搜索 p99 超过三百毫秒，持续六秒；
- `InventoryDBPoolSaturated`：二十秒内 InUse 顶到上限，同时每秒有超过一次等待。

窗口故意很短，演示时十几秒就能触发。生产上会用分钟级。

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-03
docker-compose -p tailhunt up -d --build
open http://localhost:3098      # Grafana，默认首页就是 tailhunt 的 dashboard
open http://localhost:16698     # Jaeger
go test ./...
```
