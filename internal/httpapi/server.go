// Package httpapi 暴露平台的只读查询与运维接口。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"microdrama/internal/catalog"
	"microdrama/internal/gateway"
	"microdrama/internal/model"
	"microdrama/internal/rating"
	"microdrama/internal/report"
	"microdrama/internal/review"
	"microdrama/internal/settle"
	"microdrama/internal/tally"
)

// Options 配置 HTTP 服务。
type Options struct {
	Registry *catalog.Registry
	Tally    *tally.Tally
	Reports  *report.Builder
	Gateway  *gateway.Client
	Review   *review.Service
	// RateFenPerKilo 是默认结算单价，单位分。
	RateFenPerKilo int64
}

// Server 是 HTTP 服务。
type Server struct {
	opts Options
}

// New 构造 HTTP 服务。
func New(opts Options) *Server {
	if opts.RateFenPerKilo <= 0 {
		opts.RateFenPerKilo = 1187
	}
	return &Server{opts: opts}
}

// Handler 返回路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/series", s.handleSeries)
	mux.HandleFunc("/api/series/", s.handleSeriesItem)
	mux.HandleFunc("/api/ratings", s.handleRatings)
	mux.HandleFunc("/api/plays", s.handlePlays)
	mux.HandleFunc("/api/settlement", s.handleSettlement)
	mux.HandleFunc("/api/settlement/poll", s.handlePoll)
	mux.HandleFunc("/api/gateway/stats", s.handleGatewayStats)
	return mux
}

// statusFor 把领域错误映射为 HTTP 状态码。
func statusFor(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, model.ErrGatewayUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, model.ErrSeriesUnknown), errors.Is(err, model.ErrEpisodeUnknown),
		errors.Is(err, model.ErrPartyUnknown):
		return http.StatusNotFound
	case errors.Is(err, model.ErrStageConflict), errors.Is(err, model.ErrReviewRejected),
		errors.Is(err, model.ErrRatingBlocked):
		return http.StatusConflict
	case errors.Is(err, model.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, model.ErrSettleMismatch), errors.Is(err, model.ErrShareMismatch):
		return http.StatusUnprocessableEntity
	case errors.Is(err, model.ErrUnknownGenre), errors.Is(err, model.ErrUnknownRating),
		errors.Is(err, model.ErrUnknownStage), errors.Is(err, model.ErrInvalidSeries),
		errors.Is(err, model.ErrInvalidEpisode), errors.Is(err, model.ErrInvalidParty):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func writeErr(w http.ResponseWriter, err error) {
	code := statusFor(err)
	writeJSON(w, code, map[string]any{"ok": false, "error": err.Error(), "status": code})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "microdrama"})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Reports.Overview())
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	if g := r.URL.Query().Get("genre"); g != "" {
		genre, err := model.ParseGenre(g)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"series": s.opts.Registry.SeriesByGenre(genre)})
		return
	}
	if st := r.URL.Query().Get("stage"); st != "" {
		stage, err := model.ParseStage(st)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"series": s.opts.Registry.SeriesByStage(stage)})
		return
	}
	rep, err := s.opts.Reports.Catalog()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleSeriesItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/series/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	id := parts[0]
	if id == "" {
		writeErr(w, fmt.Errorf("%w: 缺少剧目编号", model.ErrSeriesUnknown))
		return
	}

	if len(parts) == 2 && parts[1] == "review" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
			return
		}
		if s.opts.Review == nil {
			writeErr(w, fmt.Errorf("%w: 审核服务未启用", model.ErrGatewayUnavailable))
			return
		}
		ctx := r.Context()
		if ms := intParam(r, "timeout_ms", 0); ms > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
			defer cancel()
		}
		out, err := s.opts.Review.Submit(ctx, id)
		if err != nil {
			writeJSON(w, statusFor(err), map[string]any{
				"ok": false, "error": err.Error(), "outcome": out,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "outcome": out})
		return
	}

	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	series, err := s.opts.Registry.Lookup(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	eps, err := s.opts.Registry.Episodes(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(parts) == 2 && parts[1] == "episodes" {
		writeJSON(w, http.StatusOK, map[string]any{"series_id": id, "episodes": eps})
		return
	}
	payload := map[string]any{
		"series":   series,
		"episodes": len(eps),
		"plays":    s.opts.Tally.SeriesTotal(id),
	}
	if len(eps) > 0 {
		d, aerr := rating.Assess(id, eps)
		if aerr != nil {
			writeErr(w, aerr)
			return
		}
		payload["rating_decision"] = d
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleRatings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	rep, err := s.opts.Reports.Rating()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handlePlays(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Reports.Plays())
}

func (s *Server) handleSettlement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	rate := int64(intParam(r, "rate_fen_per_kilo", int(s.opts.RateFenPerKilo)))
	rep, err := s.opts.Reports.Settlement(rate)
	if err != nil {
		writeErr(w, err)
		return
	}
	code := http.StatusOK
	if rep.Unbalanced > 0 {
		code = http.StatusUnprocessableEntity
	}
	writeJSON(w, code, map[string]any{
		"ok":         rep.Unbalanced == 0,
		"report":     rep,
		"total_yuan": settle.Yuan(rep.Book.TotalFen),
	})
}

func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	if s.opts.Gateway == nil {
		writeErr(w, fmt.Errorf("%w: 结算通道未启用", model.ErrGatewayUnavailable))
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		taskID = "T-PROBE"
	}
	rounds := intParam(r, "rounds", 1)
	ctx := r.Context()
	if ms := intParam(r, "timeout_ms", 0); ms > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}
	begin := time.Now()
	res, err := s.opts.Gateway.Poll(ctx, taskID, rounds)
	payload := map[string]any{
		"task_id":    taskID,
		"result":     res,
		"settled":    res.Settled(),
		"elapsed_ms": time.Since(begin).Milliseconds(),
		"ok":         err == nil,
	}
	if err != nil {
		payload["error"] = err.Error()
		writeJSON(w, statusFor(err), payload)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleGatewayStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	if s.opts.Gateway == nil {
		writeErr(w, fmt.Errorf("%w: 结算通道未启用", model.ErrGatewayUnavailable))
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Gateway.Stats())
}

func intParam(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
