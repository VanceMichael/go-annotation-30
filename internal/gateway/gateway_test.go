package gateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"microdrama/internal/gateway"
	"microdrama/internal/model"
)

// TestPollSurfacesContextError 覆盖轮询过程中调用方超时的返回值。
func TestPollSurfacesContextError(t *testing.T) {
	c := gateway.New(gateway.Options{PollLatency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	begin := time.Now()
	_, err := c.Poll(ctx, "T-0001", 3)
	elapsed := time.Since(begin)

	if err == nil {
		t.Fatal("调用方超时后轮询应返回错误")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("错误应可判定为 context.DeadlineExceeded, 实际 %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("轮询耗时 %v, 应在调用方超时后立即返回", elapsed)
	}
}

// TestPollDoesNotReportSuccessOnTimeout 覆盖超时后不得报告已结算终态。
func TestPollDoesNotReportSuccessOnTimeout(t *testing.T) {
	c := gateway.New(gateway.Options{PollLatency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	res, err := c.Poll(ctx, "T-0002", 4)
	if err == nil {
		t.Fatalf("超时后返回了 nil 错误, 结果 = %+v", res)
	}
	if res.Settled() {
		t.Fatalf("超时后不应报告已结算, 状态 = %q", res.State)
	}
	if res.Rounds >= 4 {
		t.Fatalf("超时后轮次 = %d, 不应达到请求轮次 4", res.Rounds)
	}
}

func TestPollHonoursCancel(t *testing.T) {
	c := gateway.New(gateway.Options{PollLatency: 3 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	begin := time.Now()
	res, err := c.Poll(ctx, "T-0003", 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled, 实际 %v", err)
	}
	if res.Settled() {
		t.Fatal("取消后不应报告已结算")
	}
	if time.Since(begin) > time.Second {
		t.Fatalf("取消后耗时 %v, 应立即返回", time.Since(begin))
	}
}

func TestPollAlreadyExpiredContext(t *testing.T) {
	c := gateway.New(gateway.Options{PollLatency: time.Second})
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := c.Poll(ctx, "T-0004", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消的 context 应返回 context.Canceled, 实际 %v", err)
	}
	if res.Settled() {
		t.Fatal("已取消的 context 不应报告已结算")
	}
}

func TestPollSucceedsWhenFast(t *testing.T) {
	c := gateway.New(gateway.Options{})
	defer c.Close()

	res, err := c.Poll(context.Background(), "T-0005", 3)
	if err != nil {
		t.Fatalf("快速通道轮询失败: %v", err)
	}
	if !res.Settled() {
		t.Fatalf("状态 = %q, 期望 settled", res.State)
	}
	if res.Rounds != 3 {
		t.Fatalf("轮次 = %d, 期望 3", res.Rounds)
	}
	if got := c.Polls(); got != 3 {
		t.Fatalf("累计轮询次数 = %d, 期望 3", got)
	}
}

func TestPollDefaultsToOneRound(t *testing.T) {
	c := gateway.New(gateway.Options{})
	defer c.Close()
	res, err := c.Poll(context.Background(), "T-0006", 0)
	if err != nil {
		t.Fatalf("轮询失败: %v", err)
	}
	if res.Rounds != 1 {
		t.Fatalf("轮次 = %d, 期望 1", res.Rounds)
	}
}

func TestSubmitHonoursCallerTimeout(t *testing.T) {
	c := gateway.New(gateway.Options{Latency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	begin := time.Now()
	_, err := c.Submit(ctx, "T-0007", 1000)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("提交应返回 context.DeadlineExceeded, 实际 %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatalf("提交耗时 %v, 应在超时后立即返回", time.Since(begin))
	}
}

func TestSubmitReturnsSerialNo(t *testing.T) {
	c := gateway.New(gateway.Options{})
	defer c.Close()
	first, err := c.Submit(context.Background(), "T-0008", 500)
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	second, err := c.Submit(context.Background(), "T-0009", 600)
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if first.SerialNo == second.SerialNo {
		t.Fatal("两次提交的通道流水号不应相同")
	}
	if first.AcceptedFen != 500 || second.AcceptedFen != 600 {
		t.Fatalf("受理金额错误: %d / %d", first.AcceptedFen, second.AcceptedFen)
	}
	if got := c.Submits(); got != 2 {
		t.Fatalf("累计提交次数 = %d, 期望 2", got)
	}
}

func TestClosedChannelRejectsCalls(t *testing.T) {
	c := gateway.New(gateway.Options{})
	c.Close()
	if _, err := c.Submit(context.Background(), "T-0010", 1); !errors.Is(err, model.ErrGatewayUnavailable) {
		t.Fatalf("已关闭通道提交应返回 ErrGatewayUnavailable, 实际 %v", err)
	}
	if _, err := c.Poll(context.Background(), "T-0010", 1); !errors.Is(err, model.ErrGatewayUnavailable) {
		t.Fatalf("已关闭通道轮询应返回 ErrGatewayUnavailable, 实际 %v", err)
	}
	if err := c.Probe(context.Background()); !errors.Is(err, model.ErrGatewayUnavailable) {
		t.Fatalf("已关闭通道探测应返回 ErrGatewayUnavailable, 实际 %v", err)
	}
	if c.Alive() {
		t.Fatal("已关闭通道 Alive 应为 false")
	}
}

func TestProbeHonoursCallerTimeout(t *testing.T) {
	c := gateway.New(gateway.Options{Latency: 3 * time.Second})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Probe(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("探测应返回 context.DeadlineExceeded, 实际 %v", err)
	}
}

func TestSubmitAndPollCarriesAmount(t *testing.T) {
	c := gateway.New(gateway.Options{})
	defer c.Close()
	res, err := c.SubmitAndPoll(context.Background(), "T-0011", 152472, 2)
	if err != nil {
		t.Fatalf("提交并轮询失败: %v", err)
	}
	if !res.Settled() {
		t.Fatalf("状态 = %q, 期望 settled", res.State)
	}
	if res.AmountFen != 152472 {
		t.Fatalf("回执金额 = %d, 期望 152472", res.AmountFen)
	}
}

func TestSubmitAndPollSurfacesPollTimeout(t *testing.T) {
	c := gateway.New(gateway.Options{PollLatency: 5 * time.Second})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	res, err := c.SubmitAndPoll(ctx, "T-0012", 1000, 3)
	if err == nil {
		t.Fatalf("轮询超时应返回错误, 结果 = %+v", res)
	}
	if res.Settled() {
		t.Fatalf("轮询超时后不应报告已结算, 状态 = %q", res.State)
	}
}

func TestStatsReflectCalls(t *testing.T) {
	c := gateway.New(gateway.Options{Channel: "settle-gw-test"})
	defer c.Close()
	if _, err := c.Submit(context.Background(), "T-0013", 10); err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if _, err := c.Poll(context.Background(), "T-0013", 2); err != nil {
		t.Fatalf("轮询失败: %v", err)
	}
	s := c.Stats()
	if s.Channel != "settle-gw-test" || s.Submits != 1 || s.Polls != 2 || !s.Alive {
		t.Fatalf("统计异常: %+v", s)
	}
}
