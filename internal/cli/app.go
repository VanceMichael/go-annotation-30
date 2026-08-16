// Package cli 实现 dramactl 命令行界面。
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"microdrama/internal/catalog"
	"microdrama/internal/gateway"
	"microdrama/internal/httpapi"
	"microdrama/internal/model"
	"microdrama/internal/rating"
	"microdrama/internal/report"
	"microdrama/internal/review"
	"microdrama/internal/seed"
	"microdrama/internal/settle"
	"microdrama/internal/tally"
)

// 退出码约定。上层脚本依赖这些取值区分失败类别。
const (
	// ExitOK 正常结束。
	ExitOK = 0
	// ExitUsage 命令行用法错误或未归类的内部错误。
	ExitUsage = 1
	// ExitBadRequest 参数非法。
	ExitBadRequest = 2
	// ExitConflict 业务冲突：阶段流转不允许、审核驳回、分级不予播出。
	ExitConflict = 3
	// ExitAborted 外部通道被取消或超时。
	ExitAborted = 4
	// ExitNotFound 资源不存在。
	ExitNotFound = 5
	// ExitData 数据一致性问题：分账不闭合、比例不符、统计缺失。
	ExitData = 6
	// ExitThrottled 审核通道限流且重试仍未成功。
	ExitThrottled = 7
)

// Version 是当前构建版本。
const Version = "0.5.0"

const usage = `dramactl —— 微短剧内容审核与分账结算平台命令行

用法:
  dramactl <命令> [子命令] [参数]

命令:
  series list        列出剧目台账
  series show        查看单部剧目
  episode list       列出某剧目的剧集
  rating assess      判定剧目内容分级
  review submit      提交剧目进入审核流转
  tally build        构建播放量统计表
  tally append       补报一条播放量上报
  settle compute     生成分账结算报表
  settle meter       并发入账分账明细并核对计量器
  settle poll        向结算通道提交并轮询结算状态
  report catalog     输出剧目台账报表
  report plays       输出播放量报表
  report settlement  输出分账结算报表
  serve              启动 HTTP 服务
  selfcheck          运行内置自检
  version            输出版本信息

退出码:
  0 成功  1 用法或内部错误  2 参数非法  3 业务冲突
  4 外部通道中止  5 资源不存在  6 数据一致性问题  7 通道限流
`

type app struct {
	registry *catalog.Registry
	tally    *tally.Tally
	reports  *report.Builder
	gateway  *gateway.Client
	stdout   io.Writer
	stderr   io.Writer
}

func newApp(stdout, stderr io.Writer, latency, pollLatency time.Duration) (*app, error) {
	reg, tl, err := seed.Load()
	if err != nil {
		return nil, err
	}
	gw := gateway.New(gateway.Options{Latency: latency, PollLatency: pollLatency})
	return &app{
		registry: reg,
		tally:    tl,
		reports:  report.NewBuilder(reg, tl),
		gateway:  gw,
		stdout:   stdout,
		stderr:   stderr,
	}, nil
}

// Run 执行一次命令行调用并返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	a, err := newApp(stdout, stderr,
		parseDuration(args, "--gateway-latency"), parseDuration(args, "--poll-latency"))
	if err != nil {
		fmt.Fprintf(stderr, "初始化失败: %v\n", err)
		return ExitUsage
	}
	defer a.gateway.Close()

	code, err := a.route(args)
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
	}
	return code
}

// parseDuration 从参数中提取指定的时长开关，供结算通道客户端初始化。
func parseDuration(args []string, name string) time.Duration {
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				return d
			}
		}
	}
	return 0
}

func (a *app) route(args []string) (int, error) {
	switch args[0] {
	case "version":
		fmt.Fprintf(a.stdout, "dramactl %s\n", Version)
		return ExitOK, nil
	case "series":
		return a.runSeries(args[1:])
	case "episode":
		return a.runEpisode(args[1:])
	case "rating":
		return a.runRating(args[1:])
	case "review":
		return a.runReview(args[1:])
	case "tally":
		return a.runTally(args[1:])
	case "settle":
		return a.runSettle(args[1:])
	case "report":
		return a.runReport(args[1:])
	case "serve":
		return a.runServe(args[1:])
	case "selfcheck":
		return a.runSelfcheck(args[1:])
	default:
		fmt.Fprint(a.stderr, usage)
		return ExitUsage, fmt.Errorf("未知命令 %q", args[0])
	}
}

func (a *app) emit(payload any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// classify 把领域错误映射为退出码，映射依据是错误链中的哨兵错误。
func classify(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, model.ErrGatewayUnavailable):
		return ExitAborted
	case errors.Is(err, model.ErrRateLimited):
		return ExitThrottled
	case errors.Is(err, model.ErrSettleMismatch), errors.Is(err, model.ErrShareMismatch),
		errors.Is(err, model.ErrTallyMissing), errors.Is(err, model.ErrReportIncomplete):
		return ExitData
	case errors.Is(err, model.ErrStageConflict), errors.Is(err, model.ErrReviewRejected),
		errors.Is(err, model.ErrRatingBlocked):
		return ExitConflict
	case errors.Is(err, model.ErrSeriesUnknown), errors.Is(err, model.ErrEpisodeUnknown),
		errors.Is(err, model.ErrPartyUnknown):
		return ExitNotFound
	case errors.Is(err, model.ErrInvalidSeries), errors.Is(err, model.ErrInvalidEpisode),
		errors.Is(err, model.ErrInvalidParty), errors.Is(err, model.ErrUnknownGenre),
		errors.Is(err, model.ErrUnknownRating), errors.Is(err, model.ErrUnknownStage):
		return ExitBadRequest
	default:
		return ExitUsage
	}
}

func (a *app) runSeries(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("series 需要子命令: list 或 show")
	}
	fs := flag.NewFlagSet("series "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	idFlag := fs.String("id", "", "剧目编号")
	genreFlag := fs.String("genre", "", "按题材筛选")
	stageFlag := fs.String("stage", "", "按审核阶段筛选")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	switch args[0] {
	case "list":
		if *genreFlag != "" {
			g, err := model.ParseGenre(*genreFlag)
			if err != nil {
				return classify(err), err
			}
			return ExitOK, a.emit(map[string]any{"series": a.registry.SeriesByGenre(g)})
		}
		if *stageFlag != "" {
			st, err := model.ParseStage(*stageFlag)
			if err != nil {
				return classify(err), err
			}
			return ExitOK, a.emit(map[string]any{"series": a.registry.SeriesByStage(st)})
		}
		return ExitOK, a.emit(map[string]any{
			"series": a.registry.Series(),
			"counts": a.registry.Counts(),
		})
	case "show":
		if *idFlag == "" {
			return ExitBadRequest, errors.New("series show 需要 --id")
		}
		s, err := a.registry.Lookup(*idFlag)
		if err != nil {
			return classify(err), err
		}
		eps, err := a.registry.Episodes(*idFlag)
		if err != nil {
			return classify(err), err
		}
		dur, err := a.registry.TotalDurationSec(*idFlag)
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(map[string]any{
			"series":       s,
			"episodes":     len(eps),
			"duration_sec": dur,
			"plays":        a.tally.SeriesTotal(*idFlag),
		})
	default:
		return ExitUsage, fmt.Errorf("未知子命令 series %q", args[0])
	}
}

func (a *app) runEpisode(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("episode 需要子命令: list")
	}
	fs := flag.NewFlagSet("episode list", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	seriesFlag := fs.String("series", "", "剧目编号")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *seriesFlag == "" {
		return ExitBadRequest, errors.New("episode list 需要 --series")
	}
	eps, err := a.registry.Episodes(*seriesFlag)
	if err != nil {
		return classify(err), err
	}
	return ExitOK, a.emit(map[string]any{"series_id": *seriesFlag, "episodes": eps})
}

func (a *app) runRating(args []string) (int, error) {
	if len(args) == 0 || args[0] != "assess" {
		return ExitUsage, errors.New("rating 需要子命令: assess")
	}
	fs := flag.NewFlagSet("rating assess", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	seriesFlag := fs.String("series", "", "剧目编号，默认全部")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *seriesFlag != "" {
		eps, err := a.registry.Episodes(*seriesFlag)
		if err != nil {
			return classify(err), err
		}
		d, err := rating.Assess(*seriesFlag, eps)
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(d)
	}
	rep, err := a.reports.Rating()
	if err != nil {
		return classify(err), err
	}
	return ExitOK, a.emit(rep)
}

func (a *app) runReview(args []string) (int, error) {
	if len(args) == 0 || args[0] != "submit" {
		return ExitUsage, errors.New("review 需要子命令: submit")
	}
	fs := flag.NewFlagSet("review submit", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	seriesFlag := fs.String("series", "", "剧目编号，默认全部")
	limitFlag := fs.Int("limit-times", 2, "审核通道对每个剧目返回限流的次数")
	attemptsFlag := fs.Int("max-attempts", 4, "单个剧目允许的最大尝试次数")
	backoffFlag := fs.Duration("backoff", 2*time.Millisecond, "首次退避时长")
	timeoutFlag := fs.Duration("timeout", 10*time.Second, "整体超时")
	_ = fs.Duration("gateway-latency", 0, "模拟结算通道响应耗时")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *limitFlag < 0 || *attemptsFlag <= 0 {
		return ExitBadRequest, errors.New("--limit-times 不能为负, --max-attempts 必须为正")
	}

	ch := review.NewFlakyChannel(*limitFlag, 0)
	svc := review.NewService(a.registry, ch, review.Options{
		MaxAttempts: *attemptsFlag,
		Backoff:     *backoffFlag,
	})
	ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
	defer cancel()

	if *seriesFlag != "" {
		out, err := svc.Submit(ctx, *seriesFlag)
		payload := map[string]any{
			"outcome":     out,
			"retries":     out.Retries,
			"limit_times": *limitFlag,
			"calls":       ch.Calls(*seriesFlag),
			"ok":          err == nil,
		}
		if err != nil {
			payload["message"] = err.Error()
			payload["exit_code"] = classify(err)
		}
		if eerr := a.emit(payload); eerr != nil {
			return ExitUsage, eerr
		}
		if err != nil {
			return classify(err), err
		}
		return ExitOK, nil
	}

	batch := svc.SubmitAll(ctx, seed.SeriesIDs())
	retries := 0
	for _, o := range batch.Outcomes {
		retries += o.Retries
	}
	payload := map[string]any{
		"outcomes":    len(batch.Outcomes),
		"failures":    batch.Failures,
		"retries":     retries,
		"limit_times": *limitFlag,
		"detail":      batch.Outcomes,
		"ok":          len(batch.Failures) == 0,
	}
	if eerr := a.emit(payload); eerr != nil {
		return ExitUsage, eerr
	}
	if len(batch.Failures) > 0 {
		return ExitThrottled, fmt.Errorf("%d 部剧目未能完成审核流转", len(batch.Failures))
	}
	return ExitOK, nil
}

func (a *app) runTally(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("tally 需要子命令: build 或 append")
	}
	if args[0] == "append" {
		return a.runTallyAppend(args[1:])
	}
	if args[0] != "build" {
		return ExitUsage, fmt.Errorf("未知子命令 tally %q", args[0])
	}
	fs := flag.NewFlagSet("tally build", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dayFlag := fs.String("day", "", "只输出某一天")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	tl := tally.NewFor(seed.Days())
	records := seed.PlayRecords()
	if err := tl.AddAll(records); err != nil {
		return classify(err), err
	}
	if *dayFlag != "" {
		return ExitOK, a.emit(map[string]any{
			"day":   *dayFlag,
			"tags":  tl.Tags(*dayFlag),
			"total": tl.DayTotal(*dayFlag),
		})
	}
	snap := tl.Snapshot()
	expected := 0
	for _, rec := range records {
		expected += rec.Plays
	}
	payload := map[string]any{
		"records":     len(records),
		"days":        snap.Days,
		"tags":        snap.Tags,
		"rows":        snap.Rows,
		"total_plays": snap.Total,
		"expected":    expected,
		"cells":       snap.Cells,
		"ok":          snap.Total == expected && snap.Days == len(seed.Days()),
	}
	if eerr := a.emit(payload); eerr != nil {
		return ExitUsage, eerr
	}
	if snap.Total != expected || snap.Days != len(seed.Days()) {
		err := fmt.Errorf("%w: 入账合计 %d, 期望 %d; 自然日 %d, 期望 %d",
			model.ErrTallyMissing, snap.Total, expected, snap.Days, len(seed.Days()))
		return classify(err), err
	}
	return ExitOK, nil
}

// runTallyAppend 在已有统计表上补报一条播放量，自然日可以是统计窗口之外的新日期。
func (a *app) runTallyAppend(args []string) (int, error) {
	fs := flag.NewFlagSet("tally append", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dayFlag := fs.String("day", "", "补报的自然日，例如 2026-08-20")
	tagFlag := fs.String("tag", "romance", "内容标签")
	playsFlag := fs.Int("plays", 12000, "补报播放量")
	seriesFlag := fs.String("series", "MD-2026-001", "剧目编号")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	if *dayFlag == "" {
		return ExitBadRequest, errors.New("tally append 需要 --day")
	}
	if *playsFlag < 0 {
		return ExitBadRequest, errors.New("--plays 不能为负")
	}
	if _, err := a.registry.Lookup(*seriesFlag); err != nil {
		return classify(err), err
	}

	tl := tally.NewFor(seed.Days())
	if err := tl.AddAll(seed.PlayRecords()); err != nil {
		return classify(err), err
	}
	before := tl.Total()
	knownDay := false
	for _, d := range tl.Days() {
		if d == *dayFlag {
			knownDay = true
			break
		}
	}

	rec := model.PlayRecord{
		SeriesID: *seriesFlag,
		Day:      *dayFlag,
		Tag:      *tagFlag,
		Plays:    *playsFlag,
		At:       time.Now().UTC(),
	}
	if err := tl.Add(rec); err != nil {
		return classify(err), err
	}

	payload := map[string]any{
		"day":          *dayFlag,
		"tag":          *tagFlag,
		"series_id":    *seriesFlag,
		"known_day":    knownDay,
		"appended":     *playsFlag,
		"day_total":    tl.DayTotal(*dayFlag),
		"day_tags":     tl.Tags(*dayFlag),
		"days":         len(tl.Days()),
		"total_before": before,
		"total_after":  tl.Total(),
		"series_total": tl.SeriesTotal(*seriesFlag),
		"rows":         tl.Rows(),
		"ok":           tl.Total() == before+*playsFlag && tl.Plays(*dayFlag, *tagFlag) >= *playsFlag,
	}
	if eerr := a.emit(payload); eerr != nil {
		return ExitUsage, eerr
	}
	if tl.Total() != before+*playsFlag {
		err := fmt.Errorf("%w: 补报前 %d, 补报 %d, 补报后 %d",
			model.ErrTallyMissing, before, *playsFlag, tl.Total())
		return classify(err), err
	}
	return ExitOK, nil
}

func (a *app) runSettle(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("settle 需要子命令: compute、meter 或 poll")
	}
	switch args[0] {
	case "compute":
		return a.runSettleCompute(args[1:])
	case "meter":
		return a.runSettleMeter(args[1:])
	case "poll":
		return a.runSettlePoll(args[1:])
	default:
		return ExitUsage, fmt.Errorf("未知子命令 settle %q", args[0])
	}
}

func (a *app) runSettleCompute(args []string) (int, error) {
	fs := flag.NewFlagSet("settle compute", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	rateFlag := fs.Int64("rate", seed.RateFenPerKilo, "每千次播放结算单价，单位分")
	seriesFlag := fs.String("series", "", "只结算某部剧目")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	if *rateFlag <= 0 {
		return ExitBadRequest, errors.New("--rate 必须为正")
	}

	if *seriesFlag != "" {
		s, err := a.registry.Lookup(*seriesFlag)
		if err != nil {
			return classify(err), err
		}
		st, err := settle.Compute(s, a.tally.SeriesTotal(*seriesFlag), *rateFlag)
		if err != nil {
			return classify(err), err
		}
		verr := settle.Verify(st)
		payload := map[string]any{
			"statement":  st,
			"balanced":   st.Balanced(),
			"diff_fen":   st.DiffFen(),
			"total_yuan": settle.Yuan(st.TotalFen),
			"ok":         verr == nil,
		}
		if verr != nil {
			payload["message"] = verr.Error()
		}
		if eerr := a.emit(payload); eerr != nil {
			return ExitUsage, eerr
		}
		if verr != nil {
			return classify(verr), verr
		}
		return ExitOK, nil
	}

	rep, err := a.reports.Settlement(*rateFlag)
	if err != nil {
		return classify(err), err
	}
	payload := map[string]any{
		"statements":    len(rep.Book.Statements),
		"total_fen":     rep.Book.TotalFen,
		"allocated_fen": rep.Book.AllocatedFen,
		"diff_fen":      rep.DiffFen,
		"unbalanced":    rep.Unbalanced,
		"total_yuan":    rep.TotalYuan,
		"by_party":      rep.Book.ByParty,
		"detail":        rep.Book.Statements,
		"ok":            rep.Book.Balanced(),
	}
	if eerr := a.emit(payload); eerr != nil {
		return ExitUsage, eerr
	}
	if !rep.Book.Balanced() {
		err := fmt.Errorf("%w: %d 张结算单不闭合, 合计偏差 %d 分",
			model.ErrSettleMismatch, rep.Unbalanced, rep.DiffFen)
		return classify(err), err
	}
	return ExitOK, nil
}

func (a *app) runSettleMeter(args []string) (int, error) {
	fs := flag.NewFlagSet("settle meter", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	workersFlag := fs.Int("workers", 64, "并发入账通道数")
	perWorkerFlag := fs.Int("per-worker", 5000, "每个通道的入账次数")
	amountFlag := fs.Int64("amount", 7, "单次入账金额，单位分")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	if *workersFlag <= 0 || *perWorkerFlag <= 0 || *amountFlag <= 0 {
		return ExitBadRequest, errors.New("--workers/--per-worker/--amount 必须为正")
	}

	meter := settle.NewMeter()
	ids := seed.SeriesIDs()
	var wg sync.WaitGroup
	wg.Add(*workersFlag)
	start := make(chan struct{})
	for w := 0; w < *workersFlag; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			series := ids[w%len(ids)]
			party := fmt.Sprintf("P-W%02d", w%len(ids))
			for i := 0; i < *perWorkerFlag; i++ {
				meter.Bump(series, party, *amountFlag)
			}
		}(w)
	}
	close(start)
	wg.Wait()

	expectedCalls := int64(*workersFlag) * int64(*perWorkerFlag)
	expectedAmount := expectedCalls * *amountFlag
	snap := meter.Snapshot()
	ok := snap.Calls == expectedCalls && snap.AmountFen == expectedAmount && snap.Consistent()
	payload := map[string]any{
		"workers":          *workersFlag,
		"per_worker":       *perWorkerFlag,
		"expected_calls":   expectedCalls,
		"calls":            snap.Calls,
		"series_calls":     snap.SeriesCalls,
		"expected_amount":  expectedAmount,
		"amount_fen":       snap.AmountFen,
		"party_amount_fen": snap.PartyAmountFen,
		"lost_calls":       expectedCalls - snap.Calls,
		"lost_amount_fen":  expectedAmount - snap.AmountFen,
		"consistent":       snap.Consistent(),
		"ok":               ok,
	}
	if eerr := a.emit(payload); eerr != nil {
		return ExitUsage, eerr
	}
	if !ok {
		return ExitData, fmt.Errorf("计量器入账丢失: 期望 %d 次/%d 分, 实际 %d 次/%d 分",
			expectedCalls, expectedAmount, snap.Calls, snap.AmountFen)
	}
	return ExitOK, nil
}

func (a *app) runSettlePoll(args []string) (int, error) {
	fs := flag.NewFlagSet("settle poll", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	taskFlag := fs.String("task", "T-2026-0816", "结算任务号")
	roundsFlag := fs.Int("rounds", 3, "轮询轮次")
	amountFlag := fs.Int64("amount", 152472, "提交金额，单位分")
	timeoutFlag := fs.Duration("timeout", 5*time.Second, "单次调用超时")
	_ = fs.Duration("gateway-latency", 0, "模拟结算通道响应耗时")
	_ = fs.Duration("poll-latency", 0, "模拟单轮轮询耗时")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	if *roundsFlag <= 0 {
		return ExitBadRequest, errors.New("--rounds 必须为正")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
	defer cancel()

	begin := time.Now()
	rec, err := a.gateway.Submit(ctx, *taskFlag, *amountFlag)
	if err != nil {
		payload := map[string]any{
			"task_id":    *taskFlag,
			"stage":      "submit",
			"timeout_ms": timeoutFlag.Milliseconds(),
			"elapsed_ms": time.Since(begin).Milliseconds(),
			"ok":         false,
			"message":    err.Error(),
			"exit_code":  classify(err),
		}
		if eerr := a.emit(payload); eerr != nil {
			return ExitUsage, eerr
		}
		return classify(err), err
	}

	res, perr := a.gateway.Poll(ctx, rec.TaskID, *roundsFlag)
	elapsed := time.Since(begin)
	payload := map[string]any{
		"task_id":     rec.TaskID,
		"serial_no":   rec.SerialNo,
		"stage":       "poll",
		"state":       res.State,
		"rounds":      res.Rounds,
		"want_rounds": *roundsFlag,
		"settled":     res.Settled(),
		"timeout_ms":  timeoutFlag.Milliseconds(),
		"elapsed_ms":  elapsed.Milliseconds(),
		"stats":       a.gateway.Stats(),
		"ok":          perr == nil,
	}
	if perr != nil {
		payload["message"] = perr.Error()
		payload["exit_code"] = classify(perr)
	}
	if eerr := a.emit(payload); eerr != nil {
		return ExitUsage, eerr
	}
	if perr != nil {
		return classify(perr), perr
	}
	return ExitOK, nil
}

func (a *app) runReport(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("report 需要子命令: catalog、plays 或 settlement")
	}
	switch args[0] {
	case "catalog":
		rep, err := a.reports.Catalog()
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(rep)
	case "plays":
		return ExitOK, a.emit(a.reports.Plays())
	case "settlement":
		rep, err := a.reports.Settlement(seed.RateFenPerKilo)
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(rep)
	default:
		return ExitUsage, fmt.Errorf("未知子命令 report %q", args[0])
	}
}

func (a *app) runServe(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	addrFlag := fs.String("addr", "127.0.0.1:8080", "监听地址")
	_ = fs.Duration("gateway-latency", 0, "模拟结算通道响应耗时")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	svc := review.NewService(a.registry, review.NewFlakyChannel(0, 0), review.Options{})
	srv := httpapi.New(httpapi.Options{
		Registry:       a.registry,
		Tally:          a.tally,
		Reports:        a.reports,
		Gateway:        a.gateway,
		Review:         svc,
		RateFenPerKilo: seed.RateFenPerKilo,
	})
	fmt.Fprintf(a.stdout, "dramactl serve 监听 %s\n", *addrFlag)
	server := &http.Server{
		Addr:              *addrFlag,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return ExitUsage, err
	}
	return ExitOK, nil
}

func (a *app) runSelfcheck(args []string) (int, error) {
	fs := flag.NewFlagSet("selfcheck", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	_ = fs.Duration("gateway-latency", 0, "模拟结算通道响应耗时")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}

	checks := make([]map[string]any, 0, 8)
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"check": name, "ok": ok, "detail": detail})
	}

	c := a.registry.Counts()
	add("catalog", c.Series == len(seed.Series()) && c.Episodes == len(seed.Episodes()),
		fmt.Sprintf("剧目 %d 部, 剧集 %d 集, 参与方 %d 个", c.Series, c.Episodes, c.Parties))

	// 播放量统计表必须按需扩展到每个自然日与标签。
	probe := tally.New()
	records := seed.PlayRecords()
	tallyErr := probe.AddAll(records)
	expected := 0
	for _, rec := range records {
		expected += rec.Plays
	}
	add("tally-covers-every-day", tallyErr == nil && probe.Total() == expected &&
		len(probe.Days()) == len(seed.Days()),
		fmt.Sprintf("入账 %d 条, 自然日 %d 个, 合计播放 %d（期望 %d）",
			probe.Rows(), len(probe.Days()), probe.Total(), expected))

	// 限流错误必须能沿错误链判定，从而触发退避重试。
	ch := review.NewFlakyChannel(2, 0)
	svc := review.NewService(a.registry, ch, review.Options{MaxAttempts: 4, Backoff: time.Millisecond})
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	rout, rerr := svc.Submit(rctx, seed.SeriesIDs()[0])
	add("review-retries-on-throttle", rerr == nil && rout.Retries == 2,
		fmt.Sprintf("限流 2 次后审核结果 = %v, 重试 %d 次, 通道调用 %d 次",
			rerr, rout.Retries, ch.Calls(seed.SeriesIDs()[0])))

	// 并发入账不得丢失计数。
	meter := settle.NewMeter()
	const workers, per = 32, 2000
	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < per; i++ {
				meter.Bump("MD-PROBE", fmt.Sprintf("P-%02d", w%4), 3)
			}
		}(w)
	}
	close(start)
	wg.Wait()
	snap := meter.Snapshot()
	add("settle-meter-no-lost-updates",
		snap.Calls == int64(workers*per) && snap.AmountFen == int64(workers*per*3) && snap.Consistent(),
		fmt.Sprintf("入账 %d 次（期望 %d）, 金额 %d 分（期望 %d）, 交叉核对 = %v",
			snap.Calls, workers*per, snap.AmountFen, workers*per*3, snap.Consistent()))

	// 分账各方金额之和必须精确等于待分账总额。
	settleRep, serr := a.reports.Settlement(seed.RateFenPerKilo)
	if serr != nil {
		return classify(serr), serr
	}
	add("settle-shares-sum-to-total", settleRep.Book.Balanced(),
		fmt.Sprintf("结算单 %d 张, 不闭合 %d 张, 待分账 %d 分, 各方合计 %d 分, 偏差 %d 分",
			len(settleRep.Book.Statements), settleRep.Unbalanced,
			settleRep.Book.TotalFen, settleRep.Book.AllocatedFen, settleRep.DiffFen))

	// 结算通道轮询必须如实上报调用方超时。
	slow := gateway.New(gateway.Options{Latency: 5 * time.Second})
	defer slow.Close()
	gctx, gcancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer gcancel()
	begin := time.Now()
	pres, perr := slow.Poll(gctx, "T-PROBE", 3)
	add("gateway-poll-surfaces-timeout",
		perr != nil && !pres.Settled() && time.Since(begin) < time.Second,
		fmt.Sprintf("40ms 超时下轮询返回错误 = %v, 状态 = %q, 耗时 %v",
			perr != nil, pres.State, time.Since(begin).Round(time.Millisecond)))

	// 分级判定必须覆盖全部剧目。
	ratingRep, rerr2 := a.reports.Rating()
	if rerr2 != nil {
		return classify(rerr2), rerr2
	}
	add("rating-covers-every-series", ratingRep.Summary.Series == len(seed.Series()),
		fmt.Sprintf("判定 %d 部, 允许播出 %d 部, 不予播出 %d 部",
			ratingRep.Summary.Series, ratingRep.Summary.Publishable, ratingRep.Summary.Blocked))

	sort.Slice(checks, func(i, j int) bool {
		return checks[i]["check"].(string) < checks[j]["check"].(string)
	})
	failed := 0
	for _, ck := range checks {
		if !ck["ok"].(bool) {
			failed++
		}
	}
	if err := a.emit(map[string]any{"checks": checks, "failed": failed}); err != nil {
		return ExitUsage, err
	}
	if failed > 0 {
		return ExitUsage, fmt.Errorf("自检失败 %d 项", failed)
	}
	return ExitOK, nil
}
