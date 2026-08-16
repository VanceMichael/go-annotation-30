package review_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"microdrama/internal/catalog"
	"microdrama/internal/model"
	"microdrama/internal/review"
	"microdrama/internal/seed"
)

func svc(t *testing.T, limitTimes int, attempts int) (*catalog.Registry, *review.FlakyChannel, *review.Service) {
	t.Helper()
	reg, err := seed.LoadCatalog()
	if err != nil {
		t.Fatalf("加载台账失败: %v", err)
	}
	ch := review.NewFlakyChannel(limitTimes, 0)
	return reg, ch, review.NewService(reg, ch, review.Options{
		MaxAttempts: attempts,
		Backoff:     time.Millisecond,
	})
}

// TestRateLimitedIsRetryable 覆盖限流错误的可重试判定。
func TestRateLimitedIsRetryable(t *testing.T) {
	_, ch, _ := svc(t, 1, 4)
	_, err := ch.Scan(context.Background(), "MD-2026-001", []model.Episode{
		{SeriesID: "MD-2026-001", No: 1, DurationSec: 90, Tags: []string{"daily"}},
	})
	if err == nil {
		t.Fatal("首次调用应返回限流错误")
	}
	if !review.Retryable(err) {
		t.Fatalf("限流错误应判定为可重试, 实际错误 %v", err)
	}
	if !errors.Is(err, model.ErrRateLimited) {
		t.Fatalf("限流错误应可判定为 ErrRateLimited, 实际 %v", err)
	}
}

func TestRetryableRejectsOtherErrors(t *testing.T) {
	if review.Retryable(nil) {
		t.Fatal("nil 不应判定为可重试")
	}
	if review.Retryable(fmt.Errorf("%w: 剧目缺失", model.ErrSeriesUnknown)) {
		t.Fatal("剧目缺失不应判定为可重试")
	}
}

func TestChannelErrorExposesUnderlyingCause(t *testing.T) {
	inner := fmt.Errorf("%w: 每分钟仅允许 2 次", model.ErrRateLimited)
	err := error(&review.ChannelError{SeriesID: "MD-T-001", Op: "machine-scan", Attempt: 1, Err: inner})
	if !errors.Is(err, model.ErrRateLimited) {
		t.Fatalf("包装后的错误应可判定为 ErrRateLimited, 实际 %v", err)
	}
	var ce *review.ChannelError
	if !errors.As(err, &ce) {
		t.Fatal("errors.As 应能取出 ChannelError")
	}
	if ce.Attempt != 1 {
		t.Fatalf("Attempt = %d, 期望 1", ce.Attempt)
	}
}

// TestSubmitRetriesOnRateLimit 覆盖限流后的退避重试。
func TestSubmitRetriesOnRateLimit(t *testing.T) {
	_, ch, s := svc(t, 2, 4)
	out, err := s.Submit(context.Background(), "MD-2026-001")
	if err != nil {
		t.Fatalf("限流 2 次后审核应成功, 实际 %v", err)
	}
	if out.Retries != 2 {
		t.Fatalf("重试次数 = %d, 期望 2", out.Retries)
	}
	if got := ch.Calls("MD-2026-001"); got != 3 {
		t.Fatalf("通道调用次数 = %d, 期望 3", got)
	}
	if out.Stage != model.StageApproved {
		t.Fatalf("最终阶段 = %q, 期望 approved", out.Stage)
	}
}

func TestSubmitRetriesUpToMaxAttempts(t *testing.T) {
	_, ch, s := svc(t, 3, 4)
	out, err := s.Submit(context.Background(), "MD-2026-002")
	if err != nil {
		t.Fatalf("限流 3 次后审核应成功, 实际 %v", err)
	}
	if out.Retries != 3 {
		t.Fatalf("重试次数 = %d, 期望 3", out.Retries)
	}
	if got := ch.Calls("MD-2026-002"); got != 4 {
		t.Fatalf("通道调用次数 = %d, 期望 4", got)
	}
}

func TestSubmitGivesUpAfterMaxAttempts(t *testing.T) {
	_, ch, s := svc(t, 10, 3)
	_, err := s.Submit(context.Background(), "MD-2026-003")
	if !errors.Is(err, model.ErrRateLimited) {
		t.Fatalf("持续限流应最终返回 ErrRateLimited, 实际 %v", err)
	}
	if got := ch.Calls("MD-2026-003"); got != 3 {
		t.Fatalf("通道调用次数 = %d, 期望 3", got)
	}
}

func TestSubmitRecordsBackoffPerAttempt(t *testing.T) {
	_, _, s := svc(t, 2, 4)
	out, err := s.Submit(context.Background(), "MD-2026-004")
	if err != nil {
		t.Fatalf("审核失败: %v", err)
	}
	backoffs := 0
	for _, a := range out.Attempts {
		if !a.OK && a.BackoffMS > 0 {
			backoffs++
		}
	}
	if backoffs != 2 {
		t.Fatalf("记录到 %d 次退避, 期望 2", backoffs)
	}
}

func TestSubmitWithoutThrottleNeedsOneCall(t *testing.T) {
	_, ch, s := svc(t, 0, 4)
	out, err := s.Submit(context.Background(), "MD-2026-005")
	if err != nil {
		t.Fatalf("审核失败: %v", err)
	}
	if out.Retries != 0 {
		t.Fatalf("无限流时重试次数 = %d, 期望 0", out.Retries)
	}
	if got := ch.Calls("MD-2026-005"); got != 1 {
		t.Fatalf("通道调用次数 = %d, 期望 1", got)
	}
}

func TestSubmitUnknownSeries(t *testing.T) {
	_, _, s := svc(t, 0, 4)
	if _, err := s.Submit(context.Background(), "MD-9999-999"); !errors.Is(err, model.ErrSeriesUnknown) {
		t.Fatalf("未知剧目应返回 ErrSeriesUnknown, 实际 %v", err)
	}
}

func TestSubmitHonoursContextCancel(t *testing.T) {
	_, _, s := svc(t, 0, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Submit(ctx, "MD-2026-001"); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消的 context 应返回 context.Canceled, 实际 %v", err)
	}
}

func TestSubmitAllReportsFailuresPerSeries(t *testing.T) {
	_, _, s := svc(t, 10, 2)
	batch := s.SubmitAll(context.Background(), seed.SeriesIDs())
	if len(batch.Outcomes) != len(seed.SeriesIDs()) {
		t.Fatalf("结果条数 = %d, 期望 %d", len(batch.Outcomes), len(seed.SeriesIDs()))
	}
	if len(batch.Failures) != len(seed.SeriesIDs()) {
		t.Fatalf("失败条数 = %d, 期望 %d", len(batch.Failures), len(seed.SeriesIDs()))
	}
	for i := 1; i < len(batch.Outcomes); i++ {
		if batch.Outcomes[i-1].SeriesID > batch.Outcomes[i].SeriesID {
			t.Fatal("批量结果未按剧目编号排序")
		}
	}
}

func TestSubmitAllSucceedsUnderTolerableThrottle(t *testing.T) {
	_, _, s := svc(t, 2, 4)
	batch := s.SubmitAll(context.Background(), seed.SeriesIDs())
	if len(batch.Failures) != 0 {
		t.Fatalf("限流可重试范围内不应有失败, 实际 %v", batch.Failures)
	}
	retries := 0
	for _, o := range batch.Outcomes {
		retries += o.Retries
	}
	if retries != 2*len(seed.SeriesIDs()) {
		t.Fatalf("累计重试 %d 次, 期望 %d", retries, 2*len(seed.SeriesIDs()))
	}
}

func TestSubmitRejectsBlockedContent(t *testing.T) {
	reg := catalog.New()
	if err := reg.AddSeries(model.Series{
		ID: "MD-T-900", Title: "待整改样片", Genre: model.GenreSuspense,
		Studio: "测试厂牌", Episodes: 2, Rating: model.RatingAll,
		Stage: model.StageSubmitted, FiledAt: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		Parties: []model.Party{
			{ID: "P-1", Name: "甲", Role: "producer", ShareBP: 7000},
			{ID: "P-2", Name: "乙", Role: "platform", ShareBP: 3000},
		},
	}); err != nil {
		t.Fatalf("登记剧目失败: %v", err)
	}
	for no, tags := range map[int][]string{1: {"daily"}, 2: {"illegal-content"}} {
		if err := reg.AddEpisode(model.Episode{
			SeriesID: "MD-T-900", No: no, Title: fmt.Sprintf("第 %d 集", no),
			DurationSec: 100, Tags: tags,
		}); err != nil {
			t.Fatalf("登记剧集失败: %v", err)
		}
	}
	s := review.NewService(reg, review.NewFlakyChannel(0, 0), review.Options{})
	out, err := s.Submit(context.Background(), "MD-T-900")
	if !errors.Is(err, model.ErrReviewRejected) {
		t.Fatalf("不予播出内容应返回 ErrReviewRejected, 实际 %v", err)
	}
	if out.Stage != model.StageRejected {
		t.Fatalf("最终阶段 = %q, 期望 rejected", out.Stage)
	}
}
