# 第 2 章 · 登录、锁房事务和过期清理

> 讲解视频：[p0-overview.mp4](../videos/p0-overview.mp4)（和第 1 章同一段：系统全貌）

## 这一章解决什么问题？

只会搜索的系统没有「写」。没有写，就没有事务；没有事务，就不会有人**长时间占着连接**。
这一章加三样东西：

1. gateway 的 JWT 登录，区分 `oncall`（值班，之后能用运维控制台）和 `guest`。
2. inventory 的「锁房」（hold）：客人付款前先占住一间房，两分钟不付就放回去。
3. 一个后台任务：每十秒清理过期的 hold，把房间还回去。

## 鉴权怎么做？为什么账号表不放在 inventory？

[auth.go#jwt-check](../../internal/gateway/auth.go)：标准 HS256，`jwt.WithValidMethods` 钉死算法，防止 `alg: none` 这类攻击。
secret 运行时从 `JWT_SECRET` 读，代码里的默认值是明显的 `dev-only-...` 占位。

账号表放在 gateway 自己的连接池里（最多两条连接），**不和 inventory 共用池子**。
这是故意的。后面 inventory 的池子会被打满，如果登录也走那个池子，值班的人连控制台都登不进去。

## 锁房为什么要在事务里调报价？

看 [hold.go#hold-legacy](../../internal/inventory/hold.go)。这是「第一版」的写法：

1. `BEGIN`
2. `SELECT rooms_left ... FOR UPDATE` 锁住这一晚的这一行
3. **调 quote 服务拿最终价格**（一次网络请求）
4. `UPDATE` 减库存，`INSERT` 一条 hold
5. `COMMIT`

当时的理由听起来很合理：价格和库存要一致，所以锁着行去报价。
问题在第 3 步：报价那一来一回，连接一直被这个事务攥着。
现在 quote 只要一毫秒，看不出来。第 4 章我们让 quote 变慢，它就是元凶。

假设先写在这里：**事务里调下游 = 把下游的延迟原封不动地加到连接占用时间上。**

## 真跑一次

```
$ TOK=$(curl -s localhost:8880/api/login -d '{"username":"guest","password":"guest-demo"}' | jq -r .token)
$ curl -s localhost:8880/api/holds -H "Authorization: Bearer $TOK" -d '{"hotel_id":43,"night":"2026-12-10"}'
{"id":1,"hotel_id":43,"night":"2026-12-10","price_cents":34517,"expires_at":"2026-11-25T18:14:42.220582Z"}
  HTTP 201 0.008167s

$ curl -s 'localhost:8880/api/search?city=kyoto&checkin=2026-12-10&nights=2'
{"error":"missing bearer token"}
  HTTP 401

$ docker exec tailhunt-postgres-1 psql -U tailhunt -c "select id,hotel_id,night,guest,price_cents,status from holds"
 id | hotel_id |   night    | guest | price_cents | status
----+----------+------------+-------+-------------+--------
  1 |       43 | 2026-12-10 | guest |       34517 | held
```

## 过期清理为什么写成一条 SQL？

[sweeper.go#sweeper](../../internal/inventory/sweeper.go) 用一个 CTE：先把过期 hold 标成 `expired`，再按晚汇总、加回 `rooms_left`。
一条语句就是一个隐式事务。进程在中间崩掉，也不会出现「hold 过期了，房间没还回去」。

注意：清理任务**也用同一个连接池**。池子满的时候，它也得排队。这一点在第 5 章的 runbook 里会用到。

## 测试

```
--- PASS: TestHoldTakesARoomAndPricesIt
--- PASS: TestHoldOnTheLastRoomThenSoldOut          (最后一间：201，再来一次：409，不存在的酒店：404)
--- PASS: TestSweepExpiresHoldsAndReturnsRooms
--- PASS: TestLoginIssuesATokenOnlyForTheRightPassword
--- PASS: TestAuthRejectsMissingForgedAndExpiredTokens
--- PASS: TestRoleIsEnforced
```

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-02
docker-compose -p tailhunt up -d --build
go test ./...
```
