# 第 4 章 · 把事故复现出来

> 讲解视频：[p2-trace.mp4](../videos/p2-trace.mp4) 和 [p3-pool.mp4](../videos/p3-pool.mp4)

## 这一章解决什么问题？

线上的尖峰是间歇的，等它再来一次可能要几天。
不能复现的问题，修了也证明不了修好了。
所以这一章要做到：**一条命令把尖峰造出来，一个测试把它钉住。**

需要三样东西：

1. 故障注入：让 quote 慢下来（[fault.go#quote-fault](../../internal/quote/fault.go#quote-fault)）；
2. 压力：一边有客人搜索，一边有客人锁房；
3. 一个能说「复现成功」的测试。

## 为什么压测要用开放模型（open model）？

假设你用最常见的写法：十个 goroutine，每个「发一个、等返回、再发一个」。这叫闭环（closed loop）。
系统一慢，发送也跟着慢，到达率自动下降，尾延迟被**藏掉了**。这个坑有个名字，叫 coordinated omission。

[loadgen.go#open-model](../../internal/loadgen/loadgen.go#open-model) 按固定时刻表发：第 i 个请求在 `i / rps` 秒发出，不管前面的回没回来。
测试 `TestRunKeepsTheScheduleWhenRequestsAreSlow` 证明这一点：每个请求两百毫秒，每十毫秒发一个，同时在飞的请求超过十个；闭环的话只会有一个。

## 先算一笔账：池子什么时候会满？

- 每个 legacy 锁房在事务里等 quote 三百毫秒；
- 六条连接，每秒最多做 6 ÷ 0.3 = 20 个锁房；
- 锁房的到达率是每秒 25 个。

到达 > 处理能力，排队只会越来越长。
而搜索的房态查询**和锁房排同一个队**。它自己只要两毫秒，却要先等前面那些攥着连接等 quote 的事务。

## 在线上栈里复现（真跑的输出）

健康时，每秒三十个搜索：

```
$ curl -s "localhost:8880/api/admin/window?seconds=4" -H "$A"
{"t":1795630844,"n":30,"p50_ms":2.93,"p99_ms":9.03,"errors":0}, ...
```

打开故障：quote 慢三百毫秒，锁房每秒二十五个：

```
$ curl -s localhost:8880/api/admin/fault   -H "$A" -d '{"latency_ms":300,"ratio":1}'
$ curl -s localhost:8880/api/admin/traffic -H "$A" -d '{"search_rps":30,"hold_rps":25}'
（十二秒后）
$ curl -s "localhost:8880/api/admin/window?seconds=4" -H "$A"
{"t":1795630856,"n":24,"p50_ms":1594.66,"p99_ms":6319.64,"errors":0}, ...
```

p99 从九毫秒涨到六秒多。注意搜索本身一行代码都没改。

## 三个信号分别说了什么？

连接池（`sql.DBStats`）：六条全借出去了，等待次数六百多次，累计等了九分多钟：

```
$ curl -s localhost:8880/api/admin/dbstats -H "$A"
{"max_open":6,"open":6,"in_use":6,"idle":0,"wait_count":651,"wait_duration_ms":549574.987}
```

Postgres 那一侧（`pg_stat_activity`）：连接都在 `idle in transaction`，卡在 `FOR UPDATE` 之后，等的是 `Client:ClientRead`。
意思是：**数据库在等应用发下一条语句**。慢的不是数据库，是应用拿着连接去干别的了。

```
{"pid":2659,"state":"idle in transaction","xact_age_ms":270.184,"wait_event":"Client:ClientRead",
 "query":"SELECT rooms_left FROM room_nights WHERE hotel_id = $1 AND night = $2 FOR UPDATE"}
```

告警（Prometheus）：两条都响了，`activeAt` 是同一刻：

```
{"labels":{"alertname":"SearchP99High",...},"annotations":{"summary":"search p99 is 5s (threshold 300ms)"},"state":"firing"}
{"labels":{"alertname":"InventoryDBPoolSaturated",...},"annotations":{"summary":"inventory pool full, 6 in use, callers are queueing"},"state":"firing"}
```

（p99 告警显示 5s，是因为直方图最大的桶就是五秒；真实值在网关窗口里，六秒多。）

## 把复现钉成测试

[incident_test.go#test-reproduce](../../internal/inventory/incident_test.go#test-reproduce) 在进程里搭同样的局面：真 Postgres、六条连接、quote 桩慢三百毫秒、锁房每秒二十五、房态查询每秒四十。
断言：房态查询 p99 **至少** 三百毫秒，并且池子确实让人等过。

```
$ go test -run Incident -v ./internal/inventory
incident_test.go:80: availability: n=200 p50=620.1ms p99=4047.3ms err=0.00% | holds: p99=4731ms | pool wait_count=344 wait=304122ms
--- PASS: TestIncidentReproduces_LegacyHoldsStarveAvailability (8.01s)
```

这个测试是「复现必须复现」：如果哪天它变绿不了，说明我们的模型已经和事故对不上了，后面所有「修好了」的结论都不可信。

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-04
docker-compose -p tailhunt up -d --build
go run ./cmd/loadgen -search-rps 30 -hold-rps 25 -duration 15s   # 需要先打开 quote 故障
go test -run Incident -v ./internal/inventory
```
