// Package review 实现内容审核流转：机器初审、人工复审与限流退避重试。
package review

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"microdrama/internal/catalog"
	"microdrama/internal/model"
	"microdrama/internal/rating"
)

// ChannelError 描述一次审核通道调用失败。
type ChannelError struct {
	// SeriesID 是发生失败的剧目编号。
	SeriesID string
	// Op 是失败的操作名，例如 machine-scan、human-audit。
	Op string
	// Attempt 是本次失败发生在第几次尝试上，从 1 开始。
	Attempt int
	// Err 是底层原因。
	Err error
}

// Error 实现 error 接口。
func (e *ChannelError) Error() string {
	return fmt.Sprintf("review: 剧目 %s 的 %s 第 %d 次尝试失败: %v",
		e.SeriesID, e.Op, e.Attempt, e.Err)
}

// Unwrap 暴露底层原因，供 errors.Is 与 errors.As 沿错误链判定。
func (e *ChannelError) Unwrap() error {
	return e.Err
}

// Attempt 记录一次审核尝试。
type Attempt struct {
	No      int    `json:"no"`
	Op      string `json:"op"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	// Backoff 是本次失败后实际等待的退避时长，单位毫秒。
	BackoffMS int64 `json:"backoff_ms,omitempty"`
}

// Outcome 是一部剧目的审核结果。
type Outcome struct {
	SeriesID string       `json:"series_id"`
	Stage    model.Stage  `json:"stage"`
	Rating   model.Rating `json:"rating"`
	Attempts []Attempt    `json:"attempts"`
	// Retries 是限流触发的重试次数。
	Retries int `json:"retries"`
}

// Channel 抽象外部审核通道。
type Channel interface {
	Scan(ctx context.Context, seriesID string, episodes []model.Episode) (rating.Decision, error)
}

// FlakyChannel 是一个可配置的审核通道：前 limitTimes 次返回限流错误，之后正常返回。
type FlakyChannel struct {
	mu sync.Mutex
	// LimitTimes 是每个剧目返回限流错误的次数。
	LimitTimes int
	// Latency 是单次调用耗时。
	Latency time.Duration
	calls   map[string]int
}

// NewFlakyChannel 构造一个按剧目计数的限流通道。
func NewFlakyChannel(limitTimes int, latency time.Duration) *FlakyChannel {
	return &FlakyChannel{LimitTimes: limitTimes, Latency: latency, calls: map[string]int{}}
}

// Calls 返回某剧目已经发生的调用次数。
func (c *FlakyChannel) Calls(seriesID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[seriesID]
}

// Scan 执行一次机器初审。
func (c *FlakyChannel) Scan(ctx context.Context, seriesID string, episodes []model.Episode) (rating.Decision, error) {
	c.mu.Lock()
	c.calls[seriesID]++
	n := c.calls[seriesID]
	limit := c.LimitTimes
	c.mu.Unlock()

	if c.Latency > 0 {
		timer := time.NewTimer(c.Latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return rating.Decision{}, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return rating.Decision{}, err
	}
	if n <= limit {
		return rating.Decision{}, &ChannelError{
			SeriesID: seriesID,
			Op:       "machine-scan",
			Attempt:  n,
			Err:      fmt.Errorf("%w: 通道每分钟仅允许 %d 次调用", model.ErrRateLimited, limit),
		}
	}
	return rating.Assess(seriesID, episodes)
}

// Options 配置审核服务。
type Options struct {
	// MaxAttempts 是单个剧目允许的最大尝试次数。
	MaxAttempts int
	// Backoff 是首次退避时长，后续按次数线性放大。
	Backoff time.Duration
}

// Service 编排审核流转。
type Service struct {
	reg     *catalog.Registry
	channel Channel
	opts    Options
}

// NewService 构造审核服务。
func NewService(reg *catalog.Registry, ch Channel, opts Options) *Service {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 4
	}
	if opts.Backoff <= 0 {
		opts.Backoff = 2 * time.Millisecond
	}
	return &Service{reg: reg, channel: ch, opts: opts}
}

// Retryable 报告某个错误是否值得退避后重试。
//
// 判定沿错误链进行，因此被包装过的限流错误同样成立。
func Retryable(err error) bool {
	return errors.Is(err, model.ErrRateLimited)
}

// Submit 提交一部剧目进入审核流转。限流错误会退避后重试。
func (s *Service) Submit(ctx context.Context, seriesID string) (Outcome, error) {
	series, err := s.reg.Lookup(seriesID)
	if err != nil {
		return Outcome{}, err
	}
	episodes, err := s.reg.Episodes(seriesID)
	if err != nil {
		return Outcome{}, err
	}
	if len(episodes) == 0 {
		return Outcome{}, fmt.Errorf("%w: 剧目 %s 未登记剧集", model.ErrEpisodeUnknown, seriesID)
	}

	out := Outcome{SeriesID: seriesID, Stage: series.Stage, Rating: series.Rating}
	if series.Stage == model.StageSubmitted {
		if _, err := s.reg.SetStage(seriesID, model.StageMachine); err != nil {
			return out, err
		}
		out.Stage = model.StageMachine
	}

	var decision rating.Decision
	var lastErr error
	for attempt := 1; attempt <= s.opts.MaxAttempts; attempt++ {
		decision, lastErr = s.channel.Scan(ctx, seriesID, episodes)
		if lastErr == nil {
			out.Attempts = append(out.Attempts, Attempt{No: attempt, Op: "machine-scan", OK: true})
			break
		}
		rec := Attempt{No: attempt, Op: "machine-scan", OK: false, Message: lastErr.Error()}
		if !Retryable(lastErr) || attempt == s.opts.MaxAttempts {
			out.Attempts = append(out.Attempts, rec)
			return out, lastErr
		}
		backoff := time.Duration(attempt) * s.opts.Backoff
		rec.BackoffMS = backoff.Milliseconds()
		out.Attempts = append(out.Attempts, rec)
		out.Retries++
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return out, ctx.Err()
		case <-timer.C:
		}
	}
	if lastErr != nil {
		return out, lastErr
	}

	if _, err := s.reg.SetRating(seriesID, decision.Rating); err != nil {
		return out, err
	}
	out.Rating = decision.Rating

	if _, err := s.reg.SetStage(seriesID, model.StageHuman); err != nil {
		return out, err
	}
	out.Stage = model.StageHuman
	out.Attempts = append(out.Attempts, Attempt{No: len(out.Attempts) + 1, Op: "human-audit", OK: true})

	next := model.StageApproved
	if !decision.Publishable {
		next = model.StageRejected
	}
	if _, err := s.reg.SetStage(seriesID, next); err != nil {
		return out, err
	}
	out.Stage = next
	if next == model.StageRejected {
		return out, fmt.Errorf("%w: 剧目 %s 分级为 %s", model.ErrReviewRejected,
			seriesID, decision.Rating.DisplayName())
	}
	return out, nil
}

// Batch 批量提交审核，返回按剧目编号排序的结果。
type Batch struct {
	Outcomes []Outcome         `json:"outcomes"`
	Failures map[string]string `json:"failures"`
}

// SubmitAll 依次提交多部剧目。
func (s *Service) SubmitAll(ctx context.Context, seriesIDs []string) Batch {
	b := Batch{Failures: map[string]string{}}
	for _, id := range seriesIDs {
		out, err := s.Submit(ctx, id)
		b.Outcomes = append(b.Outcomes, out)
		if err != nil {
			b.Failures[id] = err.Error()
		}
	}
	sort.SliceStable(b.Outcomes, func(i, j int) bool {
		return b.Outcomes[i].SeriesID < b.Outcomes[j].SeriesID
	})
	return b
}
