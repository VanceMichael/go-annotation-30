// Package gateway 封装结算通道客户端：提交结算单、轮询结算状态与探测通道可用性。
package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"microdrama/internal/model"
)

// Options 配置结算通道客户端。
type Options struct {
	// Channel 是通道名称。
	Channel string
	// Latency 是提交与探测调用的模拟耗时。
	Latency time.Duration
	// PollLatency 是单轮轮询的模拟耗时，为零时沿用 Latency。
	PollLatency time.Duration
}

// Result 是一次轮询得到的结算状态。
type Result struct {
	TaskID string `json:"task_id"`
	// State 是结算状态：pending、settled 或 failed。
	State string `json:"state"`
	// AmountFen 是通道回执金额，单位分。
	AmountFen int64 `json:"amount_fen"`
	// Rounds 是本次轮询实际发生的轮次。
	Rounds int `json:"rounds"`
}

// Settled 报告该状态是否为已结算终态。
func (r Result) Settled() bool {
	return r.State == "settled"
}

// Receipt 是提交结算单后的通道回执。
type Receipt struct {
	TaskID   string `json:"task_id"`
	SerialNo string `json:"serial_no"`
	// AcceptedFen 是通道受理金额，单位分。
	AcceptedFen int64 `json:"accepted_fen"`
}

// Client 是结算通道客户端。
type Client struct {
	channel     string
	latency     time.Duration
	pollLatency time.Duration

	mu     sync.Mutex
	closed bool
	seq    int64
	// submits 是累计提交次数。
	submits int64
	// polls 是累计轮询次数。
	polls int64
}

// New 构造一个结算通道客户端。
func New(opts Options) *Client {
	ch := opts.Channel
	if ch == "" {
		ch = "settle-gw-01"
	}
	poll := opts.PollLatency
	if poll <= 0 {
		poll = opts.Latency
	}
	return &Client{channel: ch, latency: opts.Latency, pollLatency: poll}
}

// Channel 返回通道名称。
func (c *Client) Channel() string {
	return c.channel
}

// Alive 报告通道是否仍可用。
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// Close 关闭通道客户端。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

// Submits 返回累计提交次数。
func (c *Client) Submits() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.submits
}

// Polls 返回累计轮询次数。
func (c *Client) Polls() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.polls
}

// wait 等待通道响应或调用方 context 结束。
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Submit 向结算通道提交一张结算单。
func (c *Client) Submit(ctx context.Context, taskID string, amountFen int64) (Receipt, error) {
	if !c.Alive() {
		return Receipt{}, fmt.Errorf("%w: 通道 %s 已关闭", model.ErrGatewayUnavailable, c.channel)
	}
	if err := c.wait(ctx, c.latency); err != nil {
		return Receipt{}, fmt.Errorf("提交结算单 %s 中止: %w", taskID, err)
	}
	c.mu.Lock()
	c.seq++
	c.submits++
	serial := fmt.Sprintf("%s-%06d", c.channel, c.seq)
	c.mu.Unlock()
	return Receipt{TaskID: taskID, SerialNo: serial, AcceptedFen: amountFen}, nil
}

// Poll 轮询结算状态，直到进入终态或调用方 context 结束。
//
// 调用方设定的超时到达或调用被取消时，必须返回可通过 errors.Is
// 判定的 context 错误。
func (c *Client) Poll(ctx context.Context, taskID string, rounds int) (Result, error) {
	if !c.Alive() {
		return Result{}, fmt.Errorf("%w: 通道 %s 已关闭", model.ErrGatewayUnavailable, c.channel)
	}
	if rounds <= 0 {
		rounds = 1
	}
	res := Result{TaskID: taskID, State: "pending"}
	for i := 0; i < rounds; i++ {
		timer := time.NewTimer(c.pollLatency)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{TaskID: taskID, State: "aborted", Rounds: res.Rounds},
				fmt.Errorf("轮询结算单 %s 中止: %w", taskID, ctx.Err())
		case <-timer.C:
		}
		c.mu.Lock()
		c.polls++
		c.mu.Unlock()
		res.Rounds++
	}
	res.State = "settled"
	res.AmountFen = 0
	return res, nil
}

// Probe 探测通道可用性。
func (c *Client) Probe(ctx context.Context) error {
	if !c.Alive() {
		return fmt.Errorf("%w: 通道 %s 已关闭", model.ErrGatewayUnavailable, c.channel)
	}
	if err := c.wait(ctx, c.latency); err != nil {
		return fmt.Errorf("探测通道 %s 中止: %w", c.channel, err)
	}
	return nil
}

// SubmitAndPoll 提交结算单并轮询到终态。
func (c *Client) SubmitAndPoll(ctx context.Context, taskID string, amountFen int64, rounds int) (Result, error) {
	rec, err := c.Submit(ctx, taskID, amountFen)
	if err != nil {
		return Result{}, err
	}
	res, err := c.Poll(ctx, rec.TaskID, rounds)
	if err != nil {
		return res, err
	}
	res.AmountFen = rec.AcceptedFen
	return res, nil
}

// Stats 是通道客户端的调用统计。
type Stats struct {
	Channel string `json:"channel"`
	Alive   bool   `json:"alive"`
	Submits int64  `json:"submits"`
	Polls   int64  `json:"polls"`
}

// Stats 导出调用统计。
func (c *Client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Channel: c.channel, Alive: !c.closed, Submits: c.submits, Polls: c.polls}
}
