# 第 6 章 · 用 pprof 找热路径上的过度分配

> 讲解视频：[p4-fix.mp4](../videos/p4-fix.mp4) 的后半段（这一章没有单独的 learn 阶段）

## 这一章解决什么问题？

连接池修好以后，尖峰没了，但 search 服务的 GC 一直不低。
GC 是另一种尾延迟来源：分配越多，停顿越频繁。
这一章回答：**search 每个请求到底分配了多少，分配在哪？**

## 怎么拿到线上的分配 profile？

每个服务都挂了 `/debug/pprof/`（[obs.go#obs-wrap](../../internal/obs/obs.go)）。
`allocs?seconds=10` 是**增量** profile：只统计这十秒里新分配的，不混进启动时的东西。

[profile.sh](../../scripts/profile.sh) 做的事：网关每秒六十个搜索，抓十秒 allocs profile，同时读 search 自己的请求计数器，算「每个请求分配多少字节」。legacy 和 fixed 交替跑三轮。

## 看到了什么？（真跑的 pprof -top）

```
$ go tool pprof -top -sample_index=alloc_space legacy-r3.pb.gz
      flat  flat%   sum%        cum   cum%
      11MB 15.10% 15.10%       25MB 34.33%  .../internal/search.rankKey
   10.50MB 14.42% 29.52%    10.50MB 14.42%  fmt.Sprintf
    5.01MB  6.89% 36.41%     6.02MB  8.26%  encoding/json/jsontext.(*decoderState).fetch
```

先看 cum 列：`rankKey` 和它下面的 `fmt.Sprintf` 占了三分之一的分配。
一个排序函数，分配比解析 JSON、比 OTel 都多。这不正常。

## 为什么排序会分配这么多？

假设：分配和比较次数成正比。看 [rank.go#rank-legacy](../../internal/search/rank.go)：

```go
sort.Slice(rs, func(i, j int) bool { return rankKey(rs[i], tier) < rankKey(rs[j], tier) })
```

每次比较，两边各 `fmt.Sprintf` 一个新字符串，再 `strings.ToLower` 一次。
五十个酒店排序大约三百次比较，就是一千多个临时字符串，比完就扔。

## 怎么修？

[rank.go#rank-fixed](../../internal/search/rank.go)：直接比字段，名字用逐字节的大小写无关比较，不生成任何字符串。

修之前先写了等价性测试，五百轮随机数据对比两种实现的排序结果。**第一次跑就红了：**

```
--- FAIL: TestFixedRankingMatchesLegacy (0.00s)
    rank_test.go:40: round 44, position 39:
        legacy {ID:178 Name:Maple Inn 178 ...} then {ID:1 Name:Maple Inn 1 ...}
        fixed  {ID:1 Name:Maple Inn 1 ...} then {ID:178 Name:Maple Inn 178 ...}
```

原因在 legacy 的键 `...|maple inn 1|gold`：名字后面跟着分隔符 `|`。
`|` 的字节值大于数字 `8`，所以「Maple Inn 1」排在「Maple Inn 178」后面。legacy 一直有这个怪癖，没人发现。

决定：新实现按注释一直承诺的「按名字排」，短前缀在前。
等价性测试改成在「没有名字互为前缀」的数据上对比（编号补零），另加 `TestPrefixNamesSortShortestFirst` 把两种行为都钉住。
这个决定写在测试注释里，不是悄悄改掉。

## 修完的效果

微基准（`go test -bench Rank -benchmem -count=5`，在本机 Apple M5 Pro 上跑，取中位数；数据里价格经常打平，所以比较经常走到名字那一步）：

| 实现 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| legacy | 92408 | 59843 | 2953 |
| fixed | 4101 | 136 | 3 |

完整输出在 `docs/results/bench-rank.txt`。

线上服务（compose 栈，每秒六十个搜索，三轮中位数；`docs/results/profile.md`）：

| impl | requests per 10 s (median) | bytes allocated per request (median) | each round | share in ranking code |
|---|---:|---:|---|---:|
| legacy | 602 | 133389 B | 133389 / 136379 / 120980 | 28.0% |
| fixed | 602 | 82320 B | 75454 / 94585 / 82320 | 0.0% |

微基准快了二十多倍，但整个服务每请求的分配只少了大约四成。
因为剩下的分配在 JSON 编解码、OTel、net/http 里，排序只是其中一块。
面试时两个数字都要讲，只讲二十倍是误导。

## 怎么 checkout 到这一步跑起来

```bash
git checkout step-06
go test -run '^$' -bench Rank -benchmem -count=5 ./internal/search
docker-compose -p tailhunt up -d --build && ./scripts/profile.sh    # 约九十秒
```
