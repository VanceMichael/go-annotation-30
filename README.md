# microdrama —— 微短剧内容审核与分账结算平台

面向微短剧行业提质转型阶段的后端骨架：把剧目备案、剧集登记、内容审核流转、
分级判定、播放量统计与收益分账串成一条可复现的流水线。

纯 Go 标准库实现，无第三方依赖，不访问网络与数据库，全部演示数据内置。

## 目录结构

```
cmd/dramactl          命令行入口
internal/model        领域模型与哨兵错误
internal/catalog      剧目与剧集台账、审核阶段流转
internal/rating       依据内容标签判定分级
internal/review       审核流转编排与限流退避重试
internal/tally        按自然日与标签汇总播放量
internal/settle       收益分账拆分与结算计量
internal/gateway      结算通道客户端（提交、轮询、探测）
internal/report       台账/分级/播放量/结算报表
internal/httpapi      只读查询与运维接口
internal/seed         内置演示数据
```

## 构建与运行

```
export GOTOOLCHAIN=local
go build -o bin/dramactl ./cmd/dramactl
./bin/dramactl selfcheck
```

或使用 Makefile：

```
make build      # 编译到 bin/dramactl
make test       # go test ./...
make race       # go test -race ./...
make vet        # go vet ./...
make selfcheck  # 编译后运行内置自检
make docker     # 构建容器镜像
```

容器镜像基于 `golang:1.22` 多阶段构建，运行阶段使用 distroless，
同时支持 `linux/amd64` 与 `linux/arm64`：

```
docker build --platform linux/arm64 -t microdrama:local .
docker run --rm microdrama:local selfcheck
```

## 命令一览

```
dramactl series list                    列出剧目台账
dramactl series show --id <剧目号>       查看单部剧目
dramactl episode list --series <剧目号>  列出剧集
dramactl rating assess [--series ID]    判定内容分级
dramactl review submit [--series ID]    提交审核流转
dramactl tally build [--day 日期]        构建播放量统计表
dramactl tally append --day <日期>       补报一条播放量上报
dramactl settle compute [--series ID]   生成分账结算报表
dramactl settle meter                   并发入账并核对计量器
dramactl settle poll                    提交并轮询结算状态
dramactl report catalog|plays|settlement 输出报表
dramactl serve [--addr host:port]       启动 HTTP 服务
dramactl selfcheck                      运行内置自检
dramactl version                        输出版本
```

## 退出码约定

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 用法错误或未归类的内部错误 |
| 2 | 参数非法 |
| 3 | 业务冲突（阶段流转不允许、审核驳回、不予播出） |
| 4 | 外部通道被取消或超时 |
| 5 | 资源不存在 |
| 6 | 数据一致性问题（分账不闭合、比例不符、统计缺失） |
| 7 | 审核通道限流且重试后仍未成功 |

HTTP 侧对应：400 参数非法、404 资源不存在、409 业务冲突、
422 数据一致性问题、429 通道限流、503 通道不可用或超时。

## 业务口径

### 审核阶段流转

允许的流转只有以下几条，其余组合一律拒绝：

```
submitted -> machine | withdrawn
machine   -> human | rejected | withdrawn
human     -> approved | rejected | withdrawn
```

`approved`、`rejected`、`withdrawn` 是终态，不能再流出。
只有处于 `approved` 且分级允许播出的剧目才计入可播出集合。

### 内容分级

分级由剧集内容标签决定，取全部剧集中最严格的一档：

| 标签 | 最低分级 |
| --- | --- |
| `violence-graphic`、`illegal-content` | `blocked`（不予播出） |
| `violence-mild`、`substance` | `adult` |
| `romance`、`conflict` | `teen` |
| 其余 | `all` |

### 审核通道限流与重试

审核通道在高峰期会返回限流错误。限流是**可重试**的：
调用方必须能沿错误链判定出限流原因，并在退避后重试，
退避时长按尝试次数线性放大。达到最大尝试次数仍未成功才向上报错。
非限流原因（剧目不存在、内容不予播出等）不重试。

### 播放量统计

统计表是「自然日 -> 内容标签 -> 播放量」两级结构，**按需扩展**：
首次遇到某个自然日或某个标签时自动建立对应分表，不需要预先声明日期范围。
同一格重复上报按累加处理。

可以预先声明一批自然日（`tally.NewFor`），但预建只是为了让报表输出包含
零播放量的自然日；**预建范围之外的自然日同样可以直接入账**，
`dramactl tally append --day <新日期>` 就是这条路径。

统计表可以在构造时预建一批自然日，让报表输出包含零播放量的日期。
预建只影响输出的完整性，**不限制入账范围**：
统计窗口之外的新日期（例如跨月补报）同样可以直接入账。

### 收益分账

待分账总额按每千次播放单价计算：`总额（分） = 播放量 × 单价（分/千次） / 1000`。

各方按万分之一（基点）比例分账，比例合计必须为 10000 个基点。
分账明细按参与方编号排序输出，各方分得金额非负，且
**各方分得金额之和必须精确等于待分账总额**，不允许出现尾差。
结算单不闭合时视为数据一致性问题。

### 结算计量

结算过程中的入账次数与入账金额由计量器统计，
并按参与方、按剧目分别留存一份，用于交叉核对。
并发入账下三组统计必须彼此一致，不得丢失任何一次入账。

### 结算通道

提交、轮询、探测都接受调用方的 `context`。
调用方的超时或取消必须如实反映为错误返回，
并且不得把中止的轮询当作已结算终态上报。

## HTTP 接口

```
GET  /healthz
GET  /api/overview
GET  /api/series?genre=&stage=
GET  /api/series/{id}
GET  /api/series/{id}/episodes
POST /api/series/{id}/review?timeout_ms=
GET  /api/ratings
GET  /api/plays
GET  /api/settlement?rate_fen_per_kilo=
POST /api/settlement/poll?task_id=&rounds=&timeout_ms=
GET  /api/gateway/stats
```

## 内置自检

`dramactl selfcheck` 逐项核对上述业务口径，输出 JSON 结果与失败项计数，
任一项失败时退出码非 0。容器镜像的默认命令就是 `selfcheck`。
