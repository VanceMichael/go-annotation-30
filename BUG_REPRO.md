# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

分账结算单全部不闭合，每张都多分出去 1 分钱，5 张一共多 5 分。

```
$ ./dramactl settle compute
  ...
  "total_fen": 6690044,
  "allocated_fen": 6690049,
  "diff_fen": 5,
  "statements": 5,
  "unbalanced": 5,
  "ok": false
}
错误: 分账不闭合
$ echo $?
6
```

拆开看第一张单，问题很清楚：

```
  "series_id": "MD-2026-001",
  "total_fen": 1524719,
  "shares": [
    { "party_id": "P-A01", "share_bp": 4500, "amount_fen": 686124 },
    { "party_id": "P-B01", "share_bp": 3000, "amount_fen": 457416 },
    { "party_id": "P-C01", "share_bp": 1500, "amount_fen": 228708 },
    { "party_id": "P-D01", "share_bp": 1000, "amount_fen": 152472 }
  ]
```

比例是 4500+3000+1500+1000 = 10000 个基点，合计没错。但四个金额加起来是 1524720，总额是 1524719 —— 多了 1 分。每一方单看都很合理（1524719×45% = 686123.55，给了 686124），可是加总就超了。

按 README，各方分得金额之和必须精确等于待分账总额，不允许出现尾差。

对照现象：5 张结算单的总额各不相同（1524719、1149291、1789286、881806、1344942），但每一张都恰好多 1 分，偏差规模跟播放量和费率的绝对大小没关系。`GET /api/report/settlement` 返回的也是同样的偏差。

请先不要修改代码。先帮我定位根因，讲清楚这 1 分是怎么多出来的、为什么偏差规模与总额大小无关，以及为什么比例校验和单方金额本身反而都是正常的，并给出实际执行过的复现命令与观察到的输出作为证据。结论确认后再讨论怎么改。

## 含 Bug 版本

- 仓库：VanceMichael/go-annotation-30
- 仓库地址：https://github.com/VanceMichael/go-annotation-30.git
- parent SHA：d0f7cbefe7fe9d6284c566526b12a1ce97d460c6

## 复现步骤

```bash
git clone -- https://github.com/VanceMichael/go-annotation-30.git bug-repro
cd bug-repro
git checkout --detach d0f7cbefe7fe9d6284c566526b12a1ce97d460c6
go test ./internal/settle/ ./internal/report/ -run "TestSharesSumToTotal|TestSharesNoRoundingLeak|TestComputeBalancesEverySeededSeries|TestComputeAcrossRatesStaysBalanced|TestSettlementReportIsBalanced" -count=1
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/settle/ ./internal/report/ -run "TestSharesSumToTotal|TestSharesNoRoundingLeak|TestComputeBalancesEverySeededSeries|TestComputeAcrossRatesStaysBalanced|TestSettlementReportIsBalanced" -count=1
--- FAIL: TestSharesSumToTotal (0.00s)
    settle_test.go:49: 待分账 1524719 分, 各方合计 1524720 分, 偏差 1 分
--- FAIL: TestSharesNoRoundingLeak (0.00s)
    settle_test.go:76: 比例 [3333 3333 3334] 拆分 1 分, 各方合计 0 分, 偏差 -1 分
--- FAIL: TestComputeBalancesEverySeededSeries (0.00s)
    settle_test.go:152: 剧目 MD-2026-001 结算单不闭合: model: 分账金额合计与总额不一致: 剧目 MD-2026-001 待分账 1524719 分, 各方合计 1524720 分, 偏差 1 分
--- FAIL: TestComputeAcrossRatesStaysBalanced (0.00s)
    settle_test.go:224: 单价 1、播放 1000 时结算单不闭合: 待分账 1 分, 各方合计 0 分
FAIL
FAIL	microdrama/internal/settle	0.036s
--- FAIL: TestSettlementReportIsBalanced (0.00s)
    report_test.go:98: 不闭合结算单数 = 5, 期望 0
FAIL
FAIL	microdrama/internal/report	0.037s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/settle/ ./internal/report/ -run "TestSharesSumToTotal|TestSharesNoRoundingLeak|TestComputeBalancesEverySeededSeries|TestComputeAcrossRatesStaysBalanced|TestSettlementReportIsBalanced" -count=1
--- FAIL: TestSharesSumToTotal (0.00s)
    settle_test.go:49: 待分账 1524719 分, 各方合计 1524720 分, 偏差 1 分
--- FAIL: TestSharesNoRoundingLeak (0.00s)
    settle_test.go:76: 比例 [3333 3333 3334] 拆分 1 分, 各方合计 0 分, 偏差 -1 分
--- FAIL: TestComputeBalancesEverySeededSeries (0.00s)
    settle_test.go:152: 剧目 MD-2026-001 结算单不闭合: model: 分账金额合计与总额不一致: 剧目 MD-2026-001 待分账 1524719 分, 各方合计 1524720 分, 偏差 1 分
--- FAIL: TestComputeAcrossRatesStaysBalanced (0.00s)
    settle_test.go:224: 单价 1、播放 1000 时结算单不闭合: 待分账 1 分, 各方合计 0 分
FAIL
FAIL	microdrama/internal/settle	0.002s
--- FAIL: TestSettlementReportIsBalanced (0.00s)
    report_test.go:98: 不闭合结算单数 = 5, 期望 0
FAIL
FAIL	microdrama/internal/report	0.002s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

目标仓库零改动（git status 干净，无新增、修改或删除文件）。
准确指出出问题的 Go 文件与具体符号。
说明各方金额的取整方式为什么会让误差彼此叠加而不是相互抵消，并解释这一点如何使各方金额之和超出待分账总额、导致每张结算单出现固定 1 分的偏差与 5 张全部判为不闭合。
解释为什么偏差规模只取决于参与方数量与取整方向、与总额绝对大小无关，以及为什么基点合计校验、金额非负与排序都不受影响。
给出实际执行过的复现命令与观察到的输出作为证据，而非仅凭阅读代码推断。
