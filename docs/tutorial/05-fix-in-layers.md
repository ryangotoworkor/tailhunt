# 第 5 章 · 分层修复，每一层都要能被证明

> 讲解视频：[p4-fix.mp4](../videos/p4-fix.mp4)

## 这一章解决什么问题？

第 4 章找到了元凶：锁房事务攥着连接去等 quote。
修它不是改一行。线上的思路是**先止血，再根治，再兜底**，每一层防的东西不一样：

| 层 | 开关 | 防什么 | 代码 |
|---|---|---|---|
| 拿连接超时 | `acquire_timeout` | 池子满时，搜索八十毫秒内失败，不排几秒的队 | [inventory.go#acquire-timeout](../../internal/inventory/inventory.go) |
| 舱壁（bulkhead） | `bulkhead` | 锁房最多占六条里的两条，搜索永远有连接 | [guard.go#bulkhead](../../internal/guard/guard.go) |
| 报价挪出事务 | `quote_outside_tx` | **根治**：事务里不再有网络调用 | [hold.go#hold-fixed](../../internal/inventory/hold.go) |
| 报价超时 + 熔断 | `quote_guard` | quote 彻底卡死时，锁房八百毫秒失败，连错五次后直接拒绝 | [hold.go](../../internal/inventory/hold.go) 的 `price` |

四个开关在 [config.go#fix-config](../../internal/inventory/config.go)，运行时可切：

```
$ curl -s localhost:8880/api/admin/mode -H "$A" -d '{"mode":"legacy"}'
$ curl -s localhost:8880/api/admin/mode -H "$A" -d '{"mode":"fixed"}'
```

## 根治那一层为什么这样改？

修复前（[hold.go#hold-legacy](../../internal/inventory/hold.go)）：
`BEGIN → SELECT ... FOR UPDATE → 调 quote → UPDATE → INSERT → COMMIT`。

修复后（[hold.go#hold-fixed](../../internal/inventory/hold.go)）：
`调 quote → 拿连接 → BEGIN → UPDATE ... WHERE rooms_left > 0 RETURNING → INSERT → COMMIT`。

两点变化：

1. quote 在拿连接**之前**。网络调用期间不占任何数据库资源。
2. `SELECT ... FOR UPDATE` 加单独的 `UPDATE`，合成一条带条件的 `UPDATE ... RETURNING`。检查和扣减是同一条语句，行锁只活几微秒。

那「价格和库存要一致」的理由还成立吗？成立。价格只取决于酒店、日期、会员等级，跟还剩几间房无关。
原来的理由其实是「把两件不相关的事绑在一个事务里」。

## 舱壁为什么只包数据库那一段？

第一版我把舱壁包在整个 `placeHold` 外面，测试立刻抓到了问题：

```
incident_test.go:99: availability: n=200 p50=1.2ms p99=4.1ms err=0.00% | holds: p99=598ms err=72.00%
--- FAIL: TestFix_AllLayersKeepAvailabilityFast
```

搜索是快了，但锁房错了百分之七十二。
原因：修复后的路径先调 quote 再拿连接，舱壁却从调 quote 之前就占着槽位。两个槽位、每个占三百毫秒，每秒只能放七个锁房。
舱壁要保护的是**连接**，所以它应该只包「拿着连接」的那一段。改完以后锁房错误率回到零。

## 每一层都要有自己的测试，而且测试要能失败

[incident_test.go](../../internal/inventory/incident_test.go) 里，每个测试**只开一层**，在和第 4 章完全相同的事故负载下断言这一层的作用：

| 测试 | 只开 | 断言 |
|---|---|---|
| `TestFix_AcquireTimeoutFailsFastInsteadOfQueueing` | 超时 | 最慢的房态请求不超过三百毫秒，且出现了快速 503 |
| `TestFix_BulkheadReservesConnectionsForAvailability` | 舱壁 | 房态 p99 不超过一百毫秒 |
| `TestFix_QuoteOutsideTxReleasesTheConnection` | 挪出事务 | 房态 p99 不超过一百毫秒 |
| `TestFix_QuoteGuardBoundsHoldLatencyAndOpensTheBreaker` | 全开，quote 卡两秒 | 每次锁房不超过一点二秒，第六次被熔断器立刻拒绝 |
| `TestFix_AllLayersKeepAvailabilityFast` | 全开 | 房态 p99 不超过一百毫秒、错误率不超过百分之一；锁房错误率不超过百分之五 |

光有绿灯不够。`TAILHUNT_SABOTAGE=<层>` 会在测试背后把那一层强行关掉（[config.go#sabotage](../../internal/inventory/config.go)），
[failing-controls.sh#failing-controls](../../scripts/failing-controls.sh) 要求「关掉就红、打开就绿」。真跑的结果：

```
timeout    sabotaged TestFix_AcquireTimeoutFailsFastInsteadOfQueueing        FAIL (ok)
           incident_test.go:132: slowest availability 4237 ms with an 80 ms acquire timeout; want <= 300
timeout    clean     TestFix_AcquireTimeoutFailsFastInsteadOfQueueing        PASS (ok)
bulkhead   sabotaged TestFix_BulkheadReservesConnectionsForAvailability      FAIL (ok)
           incident_test.go:122: availability p99 4441.2 ms, errors 0.00% with the bulkhead
txscope    sabotaged TestFix_QuoteOutsideTxReleasesTheConnection             FAIL (ok)
           incident_test.go:113: availability p99 3284.7 ms with quote outside the tx; want <= 100
quoteguard sabotaged TestFix_QuoteGuardBoundsHoldLatencyAndOpensTheBreaker   FAIL (ok)
           incident_test.go:151: hold 0 took 2.015951958s against a 2 s quote; the 800 ms quote timeout is not there
all        sabotaged TestFix_AllLayersKeepAvailabilityFast                   FAIL (ok)
           incident_test.go:101: availability p99 3588.9 ms (want <= 100), errors 0.00% (want <= 1%)
all        clean     TestFix_AllLayersKeepAvailabilityFast                   PASS (ok)
all sabotaged runs failed and all clean runs passed
```

完整输出在 `docs/results/failing-controls.txt`。

这个脚本自己也踩过一个坑：第一版在 macOS 自带的 bash 3.2 上，「干净」的那五次**根本没跑**，打印的是 `none (UNEXPECTED)`。
空数组 `"${env[@]}"` 在 `set -u` 下被当成未定义变量。好在判据写的是「必须看到 PASS」而不是「没看到 FAIL」，所以它红了，而不是悄悄变绿。

## 只开超时那一层，为什么错误率高达八成？

看上面那行：`p99=82.1ms err=85.50%`。这不是 bug，是这一层的本分。
池子一直是满的，超时没法让搜索成功，它只能让搜索**快速失败**，不把网关的连接和 goroutine 一起拖进几秒的队里。
所以超时是止血，舱壁和挪出事务才是治病。面试时这一点要讲清楚。

## 在整套 compose 上压一遍

[bench.sh](../../scripts/bench.sh)：三种条件，三轮交替跑（避免同一台 colima 上别的项目的负载只打到一边），每次锁房每秒二十五、搜索每秒三十、测二十秒，取中位数。

| condition | searches/run | p50 | p95 | p99 (median) | p99 each round | search errors | hold errors |
|---|---:|---:|---:|---:|---|---:|---:|
| baseline | 600 | 2.5 ms | 4.2 ms | **6.9 ms** | 8 / 7 / 7 ms | 0.00% | 0.00% |
| before | 600 | 2077.7 ms | 9270.6 ms | **10003.5 ms** | 10003 / 10003 / 10004 ms | 3.83% | 4.00% |
| after | 600 | 2.6 ms | 4.6 ms | **5.9 ms** | 9 / 6 / 6 ms | 0.00% | 0.00% |

（数据：`docs/results/loadtest.json`，每一次运行在 `loadtest-runs.jsonl`。）

怎么读这张表：

- 修复前的 p99 是一万毫秒，三轮都是。这不是巧合：网关调下游的 HTTP 客户端超时是十秒，排得最久的那批请求被这个超时截断，变成了那百分之三点八的错误。真实的排队时间只会更长。
- 修复后的 p99 和健康基线是同一个量级（个位数毫秒），锁房也没有出错。
- quote 依旧慢三百毫秒。我们没有修 quote，我们修的是「慢的 quote 不再拖住连接」。

测量环境：本机 colima 虚拟机，四核、和别的项目共享 CPU。所以看中位数和每一轮的分布，不要拿单次的好数字当结论。

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-05
docker-compose -p tailhunt up -d --build
./scripts/failing-controls.sh     # 约两分钟
./scripts/bench.sh                # 约五分钟，需要 .venv（见第 7 章）
```
