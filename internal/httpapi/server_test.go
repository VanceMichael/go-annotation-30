package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"microdrama/internal/gateway"
	"microdrama/internal/httpapi"
	"microdrama/internal/report"
	"microdrama/internal/review"
	"microdrama/internal/seed"
)

type harness struct {
	srv *httptest.Server
	gw  *gateway.Client
}

func newHarness(t *testing.T, latency, pollLatency time.Duration) *harness {
	t.Helper()
	reg, tl, err := seed.Load()
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}
	gw := gateway.New(gateway.Options{Latency: latency, PollLatency: pollLatency})
	svc := review.NewService(reg, review.NewFlakyChannel(0, 0), review.Options{})
	s := httpapi.New(httpapi.Options{
		Registry:       reg,
		Tally:          tl,
		Reports:        report.NewBuilder(reg, tl),
		Gateway:        gw,
		Review:         svc,
		RateFenPerKilo: seed.RateFenPerKilo,
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		srv.Close()
		gw.Close()
	})
	return &harness{srv: srv, gw: gw}
}

func (h *harness) do(t *testing.T, method, path string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	return resp.StatusCode, payload
}

func TestHealthz(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if body["ok"] != true {
		t.Fatalf("响应异常: %v", body)
	}
}

func TestOverviewEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/overview")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if _, ok := body["total_plays"]; !ok {
		t.Fatalf("响应缺少 total_plays: %v", body)
	}
}

func TestSeriesListEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/series")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	rows, ok := body["rows"].([]any)
	if !ok || len(rows) != len(seed.Series()) {
		t.Fatalf("剧目行数异常: %v", body["rows"])
	}
}

func TestSeriesFilterByGenre(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/series?genre=urban")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if _, ok := body["series"].([]any); !ok {
		t.Fatalf("响应缺少 series: %v", body)
	}
	code, _ = h.do(t, http.MethodGet, "/api/series?genre=wuxia")
	if code != http.StatusBadRequest {
		t.Fatalf("未知题材状态码 = %d, 期望 400", code)
	}
}

func TestSeriesItemEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/series/MD-2026-001")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if _, ok := body["rating_decision"]; !ok {
		t.Fatalf("响应缺少 rating_decision: %v", body)
	}
	code, _ = h.do(t, http.MethodGet, "/api/series/MD-9999-999")
	if code != http.StatusNotFound {
		t.Fatalf("未知剧目状态码 = %d, 期望 404", code)
	}
}

func TestSeriesEpisodesEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/series/MD-2026-003/episodes")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	eps, ok := body["episodes"].([]any)
	if !ok || len(eps) != 20 {
		t.Fatalf("剧集数异常: %v", body["episodes"])
	}
}

func TestReviewEndpointAdvancesStage(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodPost, "/api/series/MD-2026-001/review")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, 响应 %v", code, body)
	}
	if body["ok"] != true {
		t.Fatalf("审核未成功: %v", body)
	}
}

func TestRatingsEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/ratings")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if _, ok := body["summary"]; !ok {
		t.Fatalf("响应缺少 summary: %v", body)
	}
}

func TestPlaysEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/plays")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if _, ok := body["snapshot"]; !ok {
		t.Fatalf("响应缺少 snapshot: %v", body)
	}
}

func TestSettlementEndpointBalanced(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/settlement")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, 响应 %v", code, body)
	}
	if body["ok"] != true {
		t.Fatalf("分账未闭合: %v", body)
	}
}

func TestPollEndpointSurfacesTimeout(t *testing.T) {
	h := newHarness(t, 0, 5*time.Second)
	begin := time.Now()
	code, body := h.do(t, http.MethodPost, "/api/settlement/poll?rounds=3&timeout_ms=40")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, 期望 503, 响应 %v", code, body)
	}
	if body["ok"] != false {
		t.Fatalf("超时后 ok 应为 false: %v", body)
	}
	if body["settled"] != false {
		t.Fatalf("超时后 settled 应为 false: %v", body)
	}
	if time.Since(begin) > 2*time.Second {
		t.Fatalf("请求耗时 %v, 应在超时后立即返回", time.Since(begin))
	}
}

func TestPollEndpointSucceedsWhenFast(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodPost, "/api/settlement/poll?rounds=2")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, 响应 %v", code, body)
	}
	if body["settled"] != true {
		t.Fatalf("快速通道应报告已结算: %v", body)
	}
}

func TestGatewayStatsEndpoint(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, body := h.do(t, http.MethodGet, "/api/gateway/stats")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", code)
	}
	if body["alive"] != true {
		t.Fatalf("通道应为可用: %v", body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newHarness(t, 0, 0)
	code, _ := h.do(t, http.MethodPost, "/api/plays")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405", code)
	}
	code, _ = h.do(t, http.MethodGet, "/api/settlement/poll")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405", code)
	}
}
