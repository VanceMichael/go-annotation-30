# BENZHI_README

## 项目说明

- 项目：VanceMichael/go-annotation-30
- 项目用途：面向微短剧行业提质转型阶段的后端骨架：把剧目备案、剧集登记、内容审核流转、 分级判定、播放量统计与收益分账串成一条可复现的流水线。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/dramactl

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-30-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-30-arm64 linux/arm64
docker run -it benzhi-task-30-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-30-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/settle/ ./internal/report/ -run "TestSharesSumToTotal|TestSharesNoRoundingLeak|TestComputeBalancesEverySeededSeries|TestComputeAcrossRatesStaysBalanced|TestSettlementReportIsBalanced" -count=1`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
