# 第 1 章 · 六个服务先跑起来

> 讲解视频：[p0-overview.mp4](../videos/p0-overview.mp4)（视频由配音流水线之后生成，没生成前这个链接是空的）

## 这一章解决什么问题？

要排查「多服务之间的 p99 尖峰」，先得有一套真的多服务系统。
场景是一个**酒店房态搜索**：客人搜「京都、十二月十号入住、住两晚」，系统返回有房的酒店，按会员价排好序。

为什么要六个服务，而不是一个？
因为线上真实的尖峰，往往不在你盯着的那个服务里。
一个请求穿过网关、搜索，再扇出到画像、风控、库存。
尖峰可能藏在其中任何一跳。只有一跳的系统，练不出「顺着链路找」的手感。

## 六个服务分别干什么？

| 服务 | 端口 | 职责 |
|---|---|---|
| gateway | 8880 | 唯一入口。之后加鉴权和运维控制台 |
| search | 8881 | 一次搜索扇出三路：profile、risk、inventory，再算价、排序 |
| inventory | 8882 | **唯一碰数据库的服务**。房态查询、锁房（第 2 章） |
| quote | 8883 | 报价。锁房时算最终价格（第 2 章开始被调用） |
| profile | 8884 | 用户画像：会员等级 gold / silver / basic |
| risk | 8885 | 风控分：分高的客人不能「到店付」 |

```
browser ──> gateway ──> search ──┬──> profile
                                 ├──> risk
                                 └──> inventory ──> Postgres
                       quote  (第 2 章起由 inventory 调用)
```

## 数据库连接池在哪？为什么现在就要关心？

假设：后面的事故一定和 inventory 的连接池有关。所以从第一行代码起就把池子的上限钉死。

看 [db.go#pool-open](../../internal/inventory/db.go#pool-open)：`SetMaxOpenConns(maxOpen)`。
compose 里 `DB_MAX_OPEN=6`。也就是说，inventory 同一时刻最多只有六条连接在干活。
第七个请求来了，不会报错，而是**在 `database/sql` 里面排队等**。
这一章它还看不出问题，因为每个查询只要一两毫秒。

## 一次搜索长什么样？（真跑的 trace）

```
$ curl -s 'localhost:8880/api/search?user=guest&city=kyoto&checkin=2026-12-10&nights=2'
{"city":"kyoto","tier":"basic","results":[{"id":138,"name":"Cedar Lodge 138","stars":4,
 "price_cents":30822,"rooms_left":400,"pay_at_hotel":true,"member_price":false}, ...

$ curl -s -o /dev/null -w 'HTTP %{http_code}  %{time_total}s\n' 'localhost:8880/api/search?...'
HTTP 200  0.006169s
```

六毫秒，一切正常。这就是之后对比用的「健康基线」的样子。

参数不全时，错误从 search 原样返回：

```
$ curl -s 'localhost:8880/api/search?city=kyoto'
{"error":"user, city, checkin and nights are required"}
```

## 扇出是怎么写的？

看 [search.go#search-fanout](../../internal/search/search.go#search-fanout)。
三路请求用三个 goroutine 并发发出，`wg.Wait()` 等齐。
所以一次搜索的耗时 ≈ 三路里**最慢的那一路**。
记住这一点：后面 inventory 一慢，整个搜索就跟着慢，profile 和 risk 再快也没用。

还有一个细节：inventory 返回 503，search 会原样透传 503（`passError`）。
这样上游能分清「忙」和「坏了」。测试 `TestSearchKeepsInventoryBusyAs503` 守着它。

## 排序那段为什么写得这么啰嗦？

[rank.go#rank-legacy](../../internal/search/rank.go#rank-legacy) 是「第一版上线的写法」：比较函数里每次都 `fmt.Sprintf` 拼一个字符串当排序键。
它是对的，测试也过。但它在热路径上每个请求分配几百个字符串。
这一章先不动它，第 6 章用 pprof 把它找出来再修。

## 测试

```
$ go test ./internal/inventory ./internal/search -v
--- PASS: TestAvailabilityReturnsHotelsFreeForTheWholeStay (0.11s)
--- PASS: TestAvailabilityRejectsBadInput (0.00s)
--- PASS: TestMigrateIsIdempotent (0.20s)
ok   .../internal/inventory
ok   .../internal/search
```

数据库测试用独立的 `tailhunt_test` 库（[pgtest.go](../../internal/pgtest/pgtest.go)），不会碰 demo 用的数据。

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-01
docker-compose -p tailhunt up -d --build
curl -s 'localhost:8880/api/search?user=guest&city=kyoto&checkin=2026-12-10&nights=2'
go test ./...
```
