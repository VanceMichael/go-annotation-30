package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"microdrama/internal/cli"
	"microdrama/internal/seed"
)

func run(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(args, &out, &errOut)
	var payload map[string]any
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			return code, nil, out.String() + errOut.String()
		}
	}
	return code, payload, errOut.String()
}

func TestHelpAndVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := cli.Run(nil, &out, &errOut); code != cli.ExitOK {
		t.Fatalf("无参数退出码 = %d, 期望 0", code)
	}
	if !strings.Contains(out.String(), "dramactl") {
		t.Fatalf("帮助信息异常: %s", out.String())
	}
	out.Reset()
	if code := cli.Run([]string{"version"}, &out, &errOut); code != cli.ExitOK {
		t.Fatalf("version 退出码 = %d, 期望 0", code)
	}
	if !strings.Contains(out.String(), cli.Version) {
		t.Fatalf("版本输出异常: %s", out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, _ := run(t, "nope")
	if code != cli.ExitUsage {
		t.Fatalf("未知命令退出码 = %d, 期望 %d", code, cli.ExitUsage)
	}
}

func TestSeriesList(t *testing.T) {
	code, body, _ := run(t, "series", "list")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	items, ok := body["series"].([]any)
	if !ok || len(items) != len(seed.Series()) {
		t.Fatalf("剧目数异常: %v", body["series"])
	}
}

func TestSeriesShowUnknown(t *testing.T) {
	code, _, _ := run(t, "series", "show", "--id", "MD-9999-999")
	if code != cli.ExitNotFound {
		t.Fatalf("退出码 = %d, 期望 %d", code, cli.ExitNotFound)
	}
}

func TestSeriesShowRequiresID(t *testing.T) {
	code, _, _ := run(t, "series", "show")
	if code != cli.ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, cli.ExitBadRequest)
	}
}

func TestEpisodeList(t *testing.T) {
	code, body, _ := run(t, "episode", "list", "--series", "MD-2026-003")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	eps, ok := body["episodes"].([]any)
	if !ok || len(eps) != 20 {
		t.Fatalf("剧集数异常: %v", body["episodes"])
	}
}

func TestRatingAssessAll(t *testing.T) {
	code, body, _ := run(t, "rating", "assess")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	if _, ok := body["summary"]; !ok {
		t.Fatalf("响应缺少 summary: %v", body)
	}
}

// TestTallyBuildCoversEveryDay 覆盖播放量统计表的构建。
func TestTallyBuildCoversEveryDay(t *testing.T) {
	code, body, stderr := run(t, "tally", "build")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["ok"] != true {
		t.Fatalf("统计表构建未成功: %v", body)
	}
	if body["days"] != float64(len(seed.Days())) {
		t.Fatalf("自然日数 = %v, 期望 %d", body["days"], len(seed.Days()))
	}
	if body["total_plays"] != body["expected"] {
		t.Fatalf("合计播放量 %v != 期望 %v", body["total_plays"], body["expected"])
	}
}

func TestTallyBuildSingleDay(t *testing.T) {
	code, body, _ := run(t, "tally", "build", "--day", "2026-08-12")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	if body["total"] == nil {
		t.Fatalf("响应缺少 total: %v", body)
	}
}

// TestReviewSubmitRetriesOnThrottle 覆盖限流后的退避重试。
func TestReviewSubmitRetriesOnThrottle(t *testing.T) {
	code, body, stderr := run(t, "review", "submit", "--series", "MD-2026-001", "--limit-times", "2")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["ok"] != true {
		t.Fatalf("审核未成功: %v", body)
	}
	if body["retries"] != float64(2) {
		t.Fatalf("重试次数 = %v, 期望 2", body["retries"])
	}
	if body["calls"] != float64(3) {
		t.Fatalf("通道调用次数 = %v, 期望 3", body["calls"])
	}
}

func TestReviewSubmitAllRetriesOnThrottle(t *testing.T) {
	code, body, stderr := run(t, "review", "submit", "--limit-times", "2")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["retries"] != float64(2*len(seed.SeriesIDs())) {
		t.Fatalf("累计重试 = %v, 期望 %d", body["retries"], 2*len(seed.SeriesIDs()))
	}
}

func TestReviewSubmitGivesUpBeyondMaxAttempts(t *testing.T) {
	code, _, _ := run(t, "review", "submit", "--series", "MD-2026-002",
		"--limit-times", "10", "--max-attempts", "2")
	if code != cli.ExitThrottled {
		t.Fatalf("退出码 = %d, 期望 %d", code, cli.ExitThrottled)
	}
}

// TestSettleComputeBalanced 覆盖分账明细与总额的一致性。
func TestSettleComputeBalanced(t *testing.T) {
	code, body, stderr := run(t, "settle", "compute")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["ok"] != true {
		t.Fatalf("分账未闭合: %v", body)
	}
	if body["unbalanced"] != float64(0) {
		t.Fatalf("不闭合结算单数 = %v, 期望 0", body["unbalanced"])
	}
	if body["diff_fen"] != float64(0) {
		t.Fatalf("分账偏差 = %v 分, 期望 0", body["diff_fen"])
	}
	if body["total_fen"] != body["allocated_fen"] {
		t.Fatalf("待分账 %v != 各方合计 %v", body["total_fen"], body["allocated_fen"])
	}
}

func TestSettleComputeSingleSeries(t *testing.T) {
	code, body, stderr := run(t, "settle", "compute", "--series", "MD-2026-002")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["balanced"] != true {
		t.Fatalf("结算单不闭合: %v", body)
	}
}

func TestSettleComputeRejectsBadRate(t *testing.T) {
	code, _, _ := run(t, "settle", "compute", "--rate", "0")
	if code != cli.ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, cli.ExitBadRequest)
	}
}

// TestSettleMeterCountsEveryCall 覆盖并发入账的计量准确性。
func TestSettleMeterCountsEveryCall(t *testing.T) {
	code, body, stderr := run(t, "settle", "meter", "--workers", "32", "--per-worker", "2000")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["ok"] != true {
		t.Fatalf("计量器入账有丢失: %v", body)
	}
	if body["calls"] != body["expected_calls"] {
		t.Fatalf("入账次数 %v != 期望 %v", body["calls"], body["expected_calls"])
	}
	if body["consistent"] != true {
		t.Fatalf("交叉核对失败: %v", body)
	}
}

// TestSettlePollSurfacesTimeout 覆盖轮询超时的上报。
func TestSettlePollSurfacesTimeout(t *testing.T) {
	code, body, _ := run(t, "settle", "poll", "--timeout", "60ms", "--poll-latency", "5s")
	if code != cli.ExitAborted {
		t.Fatalf("退出码 = %d, 期望 %d, 响应 %v", code, cli.ExitAborted, body)
	}
	if body["ok"] != false {
		t.Fatalf("超时后 ok 应为 false: %v", body)
	}
	if body["settled"] != false {
		t.Fatalf("超时后 settled 应为 false: %v", body)
	}
}

func TestSettlePollSucceedsWhenFast(t *testing.T) {
	code, body, stderr := run(t, "settle", "poll", "--rounds", "2")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["settled"] != true {
		t.Fatalf("快速通道应报告已结算: %v", body)
	}
}

func TestReportSubcommands(t *testing.T) {
	for _, sub := range []string{"catalog", "plays", "settlement"} {
		code, body, stderr := run(t, "report", sub)
		if code != cli.ExitOK {
			t.Fatalf("report %s 退出码 = %d, stderr=%s", sub, code, stderr)
		}
		if len(body) == 0 {
			t.Fatalf("report %s 输出为空", sub)
		}
	}
	code, _, _ := run(t, "report", "nope")
	if code != cli.ExitUsage {
		t.Fatalf("未知子命令退出码 = %d, 期望 %d", code, cli.ExitUsage)
	}
}

func TestSelfcheckPasses(t *testing.T) {
	code, body, stderr := run(t, "selfcheck")
	if code != cli.ExitOK {
		t.Fatalf("selfcheck 退出码 = %d, stderr=%s, 响应 %v", code, stderr, body)
	}
	if body["failed"] != float64(0) {
		t.Fatalf("自检失败项 = %v, 期望 0", body["failed"])
	}
	checks, ok := body["checks"].([]any)
	if !ok || len(checks) < 6 {
		t.Fatalf("自检项数异常: %v", body["checks"])
	}
}

// TestTallyAppendNewDay 覆盖统计窗口之外新自然日的补报。
func TestTallyAppendNewDay(t *testing.T) {
	code, body, stderr := run(t, "tally", "append", "--day", "2026-08-20",
		"--tag", "romance", "--plays", "12000")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["ok"] != true {
		t.Fatalf("补报未成功: %v", body)
	}
	if body["known_day"] != false {
		t.Fatalf("2026-08-20 不在统计窗口内, known_day 应为 false: %v", body)
	}
	if body["day_total"] != float64(12000) {
		t.Fatalf("当日合计 = %v, 期望 12000", body["day_total"])
	}
	if body["total_after"] != body["total_before"].(float64)+12000 {
		t.Fatalf("补报前 %v, 补报后 %v", body["total_before"], body["total_after"])
	}
	if body["days"] != float64(len(seed.Days())+1) {
		t.Fatalf("自然日数 = %v, 期望 %d", body["days"], len(seed.Days())+1)
	}
}

func TestTallyAppendKnownDay(t *testing.T) {
	code, body, stderr := run(t, "tally", "append", "--day", "2026-08-11",
		"--tag", "daily", "--plays", "500")
	if code != cli.ExitOK {
		t.Fatalf("退出码 = %d, 期望 0, stderr=%s", code, stderr)
	}
	if body["known_day"] != true {
		t.Fatalf("2026-08-11 在统计窗口内, known_day 应为 true: %v", body)
	}
	if body["days"] != float64(len(seed.Days())) {
		t.Fatalf("自然日数 = %v, 期望 %d", body["days"], len(seed.Days()))
	}
}

func TestTallyAppendRequiresDay(t *testing.T) {
	code, _, _ := run(t, "tally", "append")
	if code != cli.ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, cli.ExitBadRequest)
	}
}

func TestTallyAppendUnknownSeries(t *testing.T) {
	code, _, _ := run(t, "tally", "append", "--day", "2026-08-20", "--series", "MD-9999-999")
	if code != cli.ExitNotFound {
		t.Fatalf("退出码 = %d, 期望 %d", code, cli.ExitNotFound)
	}
}
