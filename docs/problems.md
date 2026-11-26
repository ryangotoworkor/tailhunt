# 日常问题与解决

这里记的是做这个项目时**真的遇到**的问题（标「真实踩坑」），以及这个系统在生产上的**典型事故**（标「生产典型」，就是失败对照演示的那一类）。
每个问题都按同一个结构写：现象 → 在哪里 → 怎么定位 → 怎么解决 → 怎么证明修好了 → 面试怎么讲。
每个问题都有一段逐行高亮的代码讲解视频（`docs/videos/problems/<id>.mp4`，由配音流水线生成）；源码引用都是锚点链接，代码不手抄。

旧代码已经不在主干里的，保留在 [docs/problems/snippets/](problems/snippets/README.md)。

<a id="q01-pool-exhaustion"></a>
## 1. 搜索 p99 飙到秒级：锁房事务攥着连接去调报价（生产典型）

**现象**

```
$ curl -s "localhost:8880/api/admin/window?seconds=4" -H "$A"
{"t":1795630856,"n":24,"p50_ms":1594.66,"p99_ms":6319.64,"errors":0}
$ curl -s localhost:8880/api/admin/dbstats -H "$A"
{"max_open":6,"open":6,"in_use":6,"idle":0,"wait_count":651,"wait_duration_ms":549574.987}
```

**在哪里**：[hold.go#hold-legacy](../internal/inventory/hold.go#hold-legacy)（修复前），[hold.go#hold-fixed](../internal/inventory/hold.go#hold-fixed)（修复后）。

**怎么定位**

- 假设一：搜索的 SQL 慢。trace 里 `db.query availability` 只有零点二毫秒，排除。
- 假设二：连接池不够。`db.pool.acquire` 占了整个请求，DBStats 里 InUse 六条全满、WaitCount 每秒涨五十多，成立。
- 那谁占着？pg_stat_activity 里六条都是 `idle in transaction`，等待事件 `Client:ClientRead`，最后一条语句是锁房的 `FOR UPDATE`：数据库在等应用。读代码，事务中间调了 quote。

**怎么解决**：quote 挪到拿连接之前；`SELECT ... FOR UPDATE` 加 `UPDATE` 合成一条 `UPDATE ... WHERE rooms_left > 0 RETURNING`。外加拿连接超时、舱壁、报价超时和熔断作为兜底。

```go
// 修复前：连接在整个 quote 调用期间被占着
tx.QueryRowContext(ctx, `SELECT rooms_left ... FOR UPDATE`, ...)
price, err := s.Quote.Price(ctx, ...) // network call, connection still held
// 修复后：先报价，再用一条语句锁、查、扣
price, err := s.price(ctx, req, cfg)
tx.QueryRowContext(ctx, `UPDATE room_nights SET rooms_left = rooms_left - 1 WHERE ... AND rooms_left > 0 RETURNING rooms_left`, ...)
```

**怎么证明**：`TestIncidentReproduces_LegacyHoldsStarveAvailability` 先证明事故能复现（legacy 下 p99 三秒多）；`TestFix_QuoteOutsideTxReleasesTheConnection` 只开这一层，p99 3.8 ms。`TAILHUNT_SABOTAGE=txscope` 关掉这一层，同一个测试变红：`availability p99 3284.7 ms with quote outside the tx; want <= 100`。

**面试怎么讲**：搜索 p99 间歇性到秒级（S）。我顺着 trace 看到时间全在拿连接，DBStats 证实池子满，pg_stat_activity 显示连接都是 idle in transaction（T）。根因是锁房事务里调了报价服务，我把调用挪出事务，并加了超时和舱壁兜底（A）。同样的故障下 p99 从十秒级回到个位数毫秒，每一层都有关掉就变红的测试（R）。

<a id="q02-bulkhead-scope"></a>
## 2. 加了舱壁，搜索好了，锁房却错了百分之七十二（真实踩坑）

**现象**：第一版修复跑出来的测试输出：

```
incident_test.go:99: availability: n=200 p50=1.2ms p99=4.1ms err=0.00% | holds: p99=598ms err=72.00% | pool wait_count=0 wait=0ms
--- FAIL: TestFix_AllLayersKeepAvailabilityFast (6.54s)
```

**在哪里**：修复前 [bulkhead_too_wide.go#bulkhead-too-wide](problems/snippets/bulkhead_too_wide.go#bulkhead-too-wide)，修复后 [hold.go#hold-fixed](../internal/inventory/hold.go#hold-fixed)。

**怎么定位**

- 搜索 p99 四毫秒，说明保护本身有效；问题只在锁房。
- 锁房 p99 五百九十八毫秒，恰好是「等三百毫秒舱壁超时 + 三百毫秒报价」。
- 假设：舱壁槽位在调报价期间也被占着。两个槽位，每个占三百毫秒，每秒最多放七个，到达是二十五个，剩下的都 429。读代码，确实是先拿槽位再调 quote。

**怎么解决**：舱壁只包「拿着连接」的那一段。legacy 路径（事务里调 quote）包整段；修复路径在报价之后才拿槽位。

```go
// 修复前：槽位从 quote 之前就占着
release, err := s.holdSlots.Acquire(ctx, holdAcquireTimeout)
defer release()
return s.holdQuoteFirst(ctx, req, cfg) // calls quote while holding the slot
// 修复后：先报价，再拿槽位
price, err := s.price(ctx, req, cfg)
release, err := s.holdSlot(ctx, cfg)
```

**怎么证明**：同一个测试 `TestFix_AllLayersKeepAvailabilityFast` 断言锁房错误率不超过百分之五，修复后 `holds: p99=311ms err=0.00%`。

**面试怎么讲**：舱壁加上后搜索恢复了，但锁房大面积 429（S）。我注意到锁房 p99 正好是舱壁超时加报价耗时（T）。原因是舱壁保护的是连接，我却把网络调用也包了进去，改成只包持有连接的那段（A）。锁房错误率从七成二降到零，这条断言留在测试里防回退（R）。

<a id="q03-bash32-empty-array"></a>
## 3. 失败对照脚本：「干净」的五次根本没跑（真实踩坑）

**现象**：`scripts/failing-controls.sh` 第一次跑：

```
timeout    clean     TestFix_AcquireTimeoutFailsFastInsteadOfQueueing        none (UNEXPECTED)
./scripts/failing-controls.sh: line 25: env[@]: unbound variable
SOME CONTROLS DID NOT BEHAVE
```

**在哪里**：修复前 [failing_controls_bash32.sh#sabotage-loop-bash32](problems/snippets/failing_controls_bash32.sh#sabotage-loop-bash32)，修复后 [failing-controls.sh#failing-controls](../scripts/failing-controls.sh#failing-controls)。

**怎么定位**

- 「破坏」的五次都 FAIL，看起来对；「干净」的五次一个 PASS 都没有，而且 verdict 是空的。
- 空 verdict 说明 go test 根本没输出，也就是没跑。
- 报错是 `env[@]: unbound variable`：macOS 自带 bash 3.2，`set -u` 下展开空数组会报未定义。

**怎么解决**：不用空数组，改成始终设置变量，干净时设为空字符串（`sabotaged()` 把空串当「什么都不关」）。

```bash
# 修复前
env=(); [ "$mode" = sabotaged ] && env=(TAILHUNT_SABOTAGE="$sab")
res=$(env "${env[@]}" go test ...)
# 修复后
val=""; [ "$mode" = sabotaged ] && val=$sab
res=$(TAILHUNT_SABOTAGE="$val" go test ...)
```

**怎么证明**：重跑后十次全部符合预期，最后一行 `all sabotaged runs failed and all clean runs passed`（`docs/results/failing-controls.txt`）。这个问题之所以被发现，是因为判据写的是「必须看到 PASS」，而不是「没看到 FAIL」。

**面试怎么讲**：失败对照脚本第一版里，对照组根本没执行（S）。我发现 verdict 为空，查到是 bash 3.2 下空数组加 set -u 的行为（T）。改成始终传环境变量（A）。更重要的经验是判据要正向：要求看到 PASS，所以它红了，而不是悄悄变绿（R）。

<a id="q04-rank-prefix-quirk"></a>
## 4. 优化排序时，等价性测试抓到旧代码的排序怪癖（真实踩坑）

**现象**：

```
--- FAIL: TestFixedRankingMatchesLegacy (0.00s)
    rank_test.go:40: round 44, position 39:
        legacy {ID:178 Name:Maple Inn 178 ...} then {ID:1 Name:Maple Inn 1 ...}
        fixed  {ID:1 Name:Maple Inn 1 ...} then {ID:178 Name:Maple Inn 178 ...}
```

**在哪里**：[rank.go#rank-legacy](../internal/search/rank.go#rank-legacy)，[rank.go#rank-fixed](../internal/search/rank.go#rank-fixed)。

**怎么定位**

- 价格、星级都相同，只差名字，而且一个名字是另一个的前缀。
- 旧代码的排序键是 `...|maple inn 1|gold`，名字后面跟着分隔符 `|`。
- `|` 的字节值是 0x7C，比数字 `8` 大，所以「1」排到了「178」后面。

**怎么解决**：新实现保持注释承诺的「按名字排、短前缀在前」；等价性测试改在名字不互为前缀的数据上比较，另写 `TestPrefixNamesSortShortestFirst` 把新旧两种行为都钉住。这是一次有意识的行为变更，写在测试注释里。

```go
// 修复前：每次比较两次 Sprintf
return fmt.Sprintf("%010d|%d|%s|%s", r.PriceCents, 9-r.Stars, strings.ToLower(r.Name), tier)
// 修复后：直接比字段，名字逐字节大小写无关比较，零分配
if a.PriceCents != b.PriceCents { return a.PriceCents < b.PriceCents }
```

**怎么证明**：`TestFixedRankingMatchesLegacy`（五百轮随机）和 `TestPrefixNamesSortShortestFirst` 都绿；`TestFixedRankingAllocatesNothing` 断言每次排序不超过四次分配。基准：92 µs、2953 次分配 → 4.1 µs、3 次分配。

**面试怎么讲**：pprof 显示排序比较函数占了 search 三分之一的分配（S）。我先写等价性测试再改，结果测试第一次就红（T）。原因是旧排序键的分隔符让前缀名字排反了，我决定修正这个行为并把两种行为都写进测试（A）。服务每请求分配少了约四成，没有引入无意的行为变化（R）。

<a id="q05-jaeger-oom"></a>
## 5. 事故演练到一半，Jaeger 自己挂了（真实踩坑）

**现象**：截 Jaeger 的图时：

```
page.goto: net::ERR_CONNECTION_REFUSED at http://localhost:16698/trace/164375ac...
$ docker ps -a --format '{{.Names}} {{.Status}}'
tailhunt-jaeger-1 Exited (137) About a minute ago
$ docker inspect tailhunt-jaeger-1 --format '{{.State.OOMKilled}} {{.State.ExitCode}} {{.RestartCount}}'
true 137 0
```

**在哪里**：修复前 [jaeger_before.yml#jaeger-before](problems/snippets/jaeger_before.yml#jaeger-before)，修复后 [docker-compose.yml#jaeger-memory](../docker-compose.yml#jaeger-memory)。

**怎么定位**

- 退出码 137 = 被 SIGKILL；`OOMKilled=true`：撞到了容器内存上限一百六十兆。
- all-in-one 用内存存储，`MEMORY_MAX_TRACES=8000`。事故期间每秒五十五个请求、每条约八个 span，九十秒就是五千条 trace。
- `RestartCount=0`：基础设施容器没写重启策略，挂了就一直挂着。

**怎么解决**：trace 上限降到三千，加 `GOMEMLIMIT=150MiB` 让 Go 运行时提前回收，容器上限放到两百兆，所有基础设施容器加 `restart: unless-stopped`。

**怎么证明**：同样九十秒的事故，每二十秒采一次内存：`41.7 → 64.1 → 97.9 → 115.6 → 121.5 → 121.7 MiB`，在上限内持平，`oom=false restarts=0`。

**面试怎么讲**：做故障演练时，观测系统本身先挂了（S）。退出码 137 加 OOMKilled，说明是内存存储的 trace 上限设得超过了容器内存（T）。我按流量算了 trace 体积，降上限、加软内存限制和重启策略（A）。同样负载下内存稳定在一百二十兆左右；教训是观测组件也要按事故时的流量做容量规划（R）。

<a id="q06-awk-label-space"></a>
## 6. pprof 脚本报「每个请求分配七十一兆」（真实踩坑）

**现象**：

```
| impl | requests per 10 s (median) | bytes allocated per request (median) |
| legacy | 0 | 71170000 B |
| fixed  | 0 | 53080000 B |
```

**在哪里**：修复前 [profile_count_before.sh#profile-count-before](problems/snippets/profile_count_before.sh#profile-count-before)，修复后 [profile.sh#profile-count](../scripts/profile.sh#profile-count)。

**怎么定位**

- 请求数是零，所以「每请求」就是总量。问题在计数，不在 profile。
- 直接看指标那一行：`tailhunt_http_server_duration_seconds_count{code="200",route="GET /search",svc="search"} 4662`。
- 标签值 `GET /search` 里有空格，awk 默认按空格切，`$2` 不是数字而是半截标签。

**怎么解决**：取最后一个字段 `$NF`。

```bash
# 修复前
... {s+=$2} END {printf "%d", s}
# 修复后
... {s+=$NF} END {printf "%d", s}
```

**怎么证明**：重跑后每十秒 602 个请求（每秒六十，和设定一致），legacy 每请求 133 KB、fixed 82 KB（`docs/results/profile.md`）。用「请求数是否等于设定的速率乘时长」做健全性检查，下次能第一时间发现。

**面试怎么讲**：性能脚本给出一个离谱的数字（S）。我没有先怀疑 profile，而是先查分母，发现请求数是零（T）。原因是 Prometheus 标签里的空格让 awk 取错了列，改成取最后一列（A）。从此每个指标脚本都加一个「数量对不对得上」的检查（R）。

<a id="q07-window-by-finish"></a>
## 7. 修好了，教学页面却说修复后 p99 还是九秒（真实踩坑）

**现象**：第一次跑教学模式的「修复前后对比」：

```
"after":  {"requests": 343, "p50_ms": 2.85, "p95_ms": 83.15, "p99_ms": 9029.46, "error_rate": 0.0612}
"before": {"requests": 408, "p50_ms": 1089.56, "p95_ms": 5532.23, "p99_ms": 9083.64, "error_rate": 0.0098}
"pool_waits_during_after": 0
```

**在哪里**：修复前 [window_by_finish.go#window-by-finish](problems/snippets/window_by_finish.go#window-by-finish)，修复后 [window.go#window-observe](../internal/gateway/window.go#window-observe)。

**怎么定位**

- 修复后连接池等待是零，p50 也正常，只有 p99 还在九秒。这两件事矛盾。
- 假设：修复后窗口里混进了修复前就开始排队的请求。它们在旧代码里排了九秒，修复后才结束。
- 读代码，窗口按请求**结束**的那一秒分桶，确实会把它们算进「修复后」。

**怎么解决**：按请求**开始**的那一秒分桶；对比时只统计修复后八秒才开始的请求，等积压排空。

```go
// 修复前
now := time.Now()
b := &w.buckets[now.Unix()%int64(len(w.buckets))]
// 修复后
start := time.Now().Add(-d)
b := &w.buckets[start.Unix()%int64(len(w.buckets))]
```

**怎么证明**：`TestWindowKeepsPerSecondP99AndTheSlowestTrace` 里那个九百毫秒的请求现在落在更早的一秒。修复后再跑：`p99 7.62 s → 10.1 ms`，修复后十秒内连接池新增等待 0 次。

**面试怎么讲**：修复上线后看板上 p99 没降（S）。我注意到连接池等待已经为零，两个信号矛盾，于是怀疑统计口径（T）。原来是按结束时间分桶，把修复前排队的请求算到了修复后，改成按开始时间（A）。做前后对比，口径和排空时间要先想清楚（R）。

<a id="q08-trace-root-late"></a>
## 8. trace 瀑布图偶尔从第二跳开始（真实踩坑）

**现象**：教学模式里抽到的慢 trace，第一行是 `search GET /search 7.62 s`，没有 gateway 那一行。

**在哪里**：修复前 [trace_without_root.go#trace-without-root](problems/snippets/trace_without_root.go#trace-without-root)，修复后 [learn.go#stage-trace](../internal/gateway/learn.go#stage-trace)。

**怎么定位**

- 同一个 trace 稍后在 Jaeger 里打开，gateway 的 span 是在的。说明不是没发，是取得太早。
- 每个服务的 span 在**自己结束时**导出，批处理最多等五百毫秒。gateway 的 server span 最后结束，所以最后到。
- 旧的判据只要求「有 db.pool.acquire」，那个 span 早就到了。

**怎么解决**：判据加一条：必须有 gateway 的根 span（深度为零）。

```go
// 修复后
root = root || (spans[i].Service == "gateway" && spans[i].Depth == 0)
if err == nil && acquire != nil && root {
```

**怎么证明**：e2e 断言瀑布图里有 `db.pool.acquire` 那一行，并且 trace 总时长不小于 acquire 时长；之后的运行里瀑布图都从 gateway 开始。

**面试怎么讲**：自动取证有时拿到残缺的 trace（S）。我意识到分布式 trace 是各服务异步上报的，根 span 往往最后到（T）。把「trace 完整」的判据改成以根 span 为准（A）。这也是为什么线上用 tail-based sampling 时要设等待窗口（R）。

<a id="q09-naive-alert-ranking"></a>
## 9. 按告警数量排，结论指向磁盘；按影响排，才是连接池（生产典型）

**现象**（合成数据，见 analysis/report.md）：

```
告警量第一的规则：DiskUsageHigh，1035 次 fire，噪音率 94%
次数第一的原因：dependency_timeout，51 次，总影响 19.4 h
影响第一的原因：db_pool_exhaustion，33 次，总影响 32.1 h
```

**在哪里**：[30_cause_ranking.sql#sql-cause-ranking](../analysis/sql/30_cause_ranking.sql#sql-cause-ranking)，[analyze_alerts.py#rank-column](../analysis/analyze_alerts.py#rank-column)。

**怎么定位**

- 先合并重复告警（三千零四十九次 fire 合并成一千五百一十一个 episode）。
- 再按时间把 episode 关联到事故，没关联上的算噪音。
- 同时算按次数和按总影响两种排名，两者第一名不同，原因是 MTTR：依赖超时十几分钟就好，连接池耗尽中位数五十四分钟。

**怎么解决**：排名用总影响（次数 × 时长），并给每条规则出建议：噪音高的降级，重复响的加 `repeat_interval`。

**怎么证明**：`test_planted_top_cause_ranks_first_by_impact` 不经过 SQL、直接从原始数据算真值再比对。`TAILHUNT_ANALYSIS_RANK=count` 换成按次数，测试变红：`analysis ranks dependency_timeout first (rank_by=count)`。

**面试怎么讲**：团队想知道下季度该先治哪类故障（S）。我用 SQL 合并重复告警、按时间关联事故，再用 pandas 排名（T）。按次数和按告警量都会把人引到错的地方，按总影响才是连接池，所以 runbook 和告警规则优先围绕它做（A）。结论用一个可以失败的测试守着；数据是合成的，简历上那段的真实数字是公司里的结果（R）。


<a id="q10-create-db-race"></a>
## 10. 第一次在 GitHub 上跑 CI：测试库「已经存在」（真实踩坑）

**现象**：仓库推到 GitHub，第一次 CI 的 go job 只红了一个测试，而且红在第一行：

```text
--- FAIL: TestSlowQuoteShowsAsIdleInTransaction (0.02s)
    diag_test.go:18: ERROR: duplicate key value violates unique constraint "pg_database_datname_index" (SQLSTATE 23505)
```

本机跑了几十遍都没见过。

**在哪里**：修复后 [pgtest.go#create-db](../internal/pgtest/pgtest.go#create-db)，测试 [pgtest_test.go#create-db-test](../internal/pgtest/pgtest_test.go#create-db-test)；修复前 [create_db_before.go#create-db-before](problems/snippets/create_db_before.go#create-db-before)。

**怎么定位**

- 报错不是测试逻辑，是建测试库那一步：`pg_database` 上的唯一键冲突，意思是有人同时在建同一个库。
- 旧写法是「先查有没有，没有就建」。`go test ./...` 会把几个包放在几个进程里并行跑，CI 上的 Postgres 是空的，两个包同时查到「没有」，同时去建，后到的那个就撞了。
- 本机为什么从来没撞：库早就建好了，「没有」这条路径根本走不到。在本机把库删掉连跑三遍旧代码也没复现，因为各包编译完的时间错开了，窗口太窄。
- 所以写了一个确定能复现的测试：八个 goroutine 同时去建同一个新库。

**怎么解决**：建库失败时再查一次，库已经在了就算成功（输给别人没关系，只要最后库存在）。

```go
// 修复前
if !exists {
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		t.Fatal(err)
	}
}
// 修复后
if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
	if ok, err2 := exists(); err2 == nil && ok {
		return nil // another process created it first
	}
	return err
}
```

**怎么证明**：`TestEnsureDBConcurrentCallersAllSucceed` 在旧逻辑下八个调用里七个报同样的错：

```text
pgtest_test.go:41: caller 0: ERROR: duplicate key value violates unique constraint "pg_database_datname_index" (SQLSTATE 23505)
...
--- FAIL: TestEnsureDBConcurrentCallersAllSucceed (0.06s)
```

修复后连跑三遍都过；推上去以后 CI 是绿的。

**面试怎么讲**：项目推上 GitHub 后，CI 第一次跑就有个测试在建库时报唯一键冲突，本机从没见过（S）。我要判断是测试本身的问题还是环境差异（T）。我从报错认出是并发建库，找到「先查后建」的竞态，因为本机库早就在所以从来走不到；写了一个八个协程同时建库的测试稳定复现它，再改成「建失败就再查一次」（A）。旧逻辑八个里七个失败，修复后全过，CI 也绿了（R）。
