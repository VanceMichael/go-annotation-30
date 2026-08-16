// Package catalog 维护剧目与剧集台账，提供登记、查询与阶段流转。
package catalog

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"microdrama/internal/model"
)

// Registry 是剧目与剧集台账。所有方法可并发调用。
type Registry struct {
	mu       sync.RWMutex
	series   map[string]model.Series
	episodes map[string][]model.Episode
	order    []string
}

// New 构造一个空台账。
func New() *Registry {
	return &Registry{
		series:   make(map[string]model.Series),
		episodes: make(map[string][]model.Episode),
	}
}

// Counts 汇总台账规模。
type Counts struct {
	Series   int `json:"series"`
	Episodes int `json:"episodes"`
	Parties  int `json:"parties"`
}

// AddSeries 登记一部剧目。
func (r *Registry) AddSeries(s model.Series) error {
	if err := s.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.series[s.ID]; ok {
		return fmt.Errorf("%w: 剧目 %s 已登记", model.ErrInvalidSeries, s.ID)
	}
	r.series[s.ID] = s
	r.order = append(r.order, s.ID)
	return nil
}

// AddEpisode 登记一集内容。
func (r *Registry) AddEpisode(e model.Episode) error {
	if err := e.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.series[e.SeriesID]
	if !ok {
		return fmt.Errorf("%w: %s", model.ErrSeriesUnknown, e.SeriesID)
	}
	if e.No > s.Episodes {
		return fmt.Errorf("%w: 剧目 %s 备案集数 %d, 提交集号 %d",
			model.ErrInvalidEpisode, s.ID, s.Episodes, e.No)
	}
	for _, x := range r.episodes[e.SeriesID] {
		if x.No == e.No {
			return fmt.Errorf("%w: 剧目 %s 第 %d 集已登记", model.ErrInvalidEpisode, s.ID, e.No)
		}
	}
	r.episodes[e.SeriesID] = append(r.episodes[e.SeriesID], e)
	return nil
}

// Series 返回全部剧目，按备案日期与编号稳定排序。
func (r *Registry) Series() []model.Series {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Series, 0, len(r.series))
	for _, id := range r.order {
		out = append(out, r.series[id])
	}
	model.SortSeries(out)
	return out
}

// Lookup 按编号查询剧目。
func (r *Registry) Lookup(id string) (model.Series, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.series[strings.TrimSpace(id)]
	if !ok {
		return model.Series{}, fmt.Errorf("%w: %s", model.ErrSeriesUnknown, id)
	}
	return s, nil
}

// Episodes 返回某剧目的全部剧集，按集号升序。
func (r *Registry) Episodes(seriesID string) ([]model.Episode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.series[seriesID]; !ok {
		return nil, fmt.Errorf("%w: %s", model.ErrSeriesUnknown, seriesID)
	}
	src := r.episodes[seriesID]
	out := make([]model.Episode, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool { return out[i].No < out[j].No })
	return out, nil
}

// Episode 按剧目编号与集号查询单集。
func (r *Registry) Episode(seriesID string, no int) (model.Episode, error) {
	items, err := r.Episodes(seriesID)
	if err != nil {
		return model.Episode{}, err
	}
	for _, e := range items {
		if e.No == no {
			return e, nil
		}
	}
	return model.Episode{}, fmt.Errorf("%w: 剧目 %s 第 %d 集", model.ErrEpisodeUnknown, seriesID, no)
}

// SeriesByGenre 按题材筛选剧目。
func (r *Registry) SeriesByGenre(g model.Genre) []model.Series {
	out := make([]model.Series, 0, 4)
	for _, s := range r.Series() {
		if s.Genre == g {
			out = append(out, s)
		}
	}
	return out
}

// SeriesByStage 按审核阶段筛选剧目。
func (r *Registry) SeriesByStage(st model.Stage) []model.Series {
	out := make([]model.Series, 0, 4)
	for _, s := range r.Series() {
		if s.Stage == st {
			out = append(out, s)
		}
	}
	return out
}

// Publishable 返回允许播出的剧目。
func (r *Registry) Publishable() []model.Series {
	out := make([]model.Series, 0, 4)
	for _, s := range r.Series() {
		if s.Stage == model.StageApproved && s.Rating.Publishable() {
			out = append(out, s)
		}
	}
	return out
}

// Party 查询某剧目下的分账参与方。
func (r *Registry) Party(seriesID, partyID string) (model.Party, error) {
	s, err := r.Lookup(seriesID)
	if err != nil {
		return model.Party{}, err
	}
	for _, p := range s.Parties {
		if p.ID == partyID {
			return p, nil
		}
	}
	return model.Party{}, fmt.Errorf("%w: 剧目 %s 参与方 %s", model.ErrPartyUnknown, seriesID, partyID)
}

// SetStage 推进剧目审核阶段，只接受允许的流转。
func (r *Registry) SetStage(seriesID string, next model.Stage) (model.Series, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.series[seriesID]
	if !ok {
		return model.Series{}, fmt.Errorf("%w: %s", model.ErrSeriesUnknown, seriesID)
	}
	if !AllowedTransition(s.Stage, next) {
		return model.Series{}, fmt.Errorf("%w: 剧目 %s 不能从 %s 流转到 %s",
			model.ErrStageConflict, seriesID, s.Stage.DisplayName(), next.DisplayName())
	}
	s.Stage = next
	r.series[seriesID] = s
	return s, nil
}

// SetRating 写回内容分级。
func (r *Registry) SetRating(seriesID string, rating model.Rating) (model.Series, error) {
	if _, err := model.ParseRating(string(rating)); err != nil {
		return model.Series{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.series[seriesID]
	if !ok {
		return model.Series{}, fmt.Errorf("%w: %s", model.ErrSeriesUnknown, seriesID)
	}
	s.Rating = rating
	r.series[seriesID] = s
	return s, nil
}

// AllowedTransition 报告某个审核阶段流转是否被允许。
func AllowedTransition(from, to model.Stage) bool {
	if from == to {
		return false
	}
	switch from {
	case model.StageSubmitted:
		return to == model.StageMachine || to == model.StageWithdrawn
	case model.StageMachine:
		return to == model.StageHuman || to == model.StageRejected || to == model.StageWithdrawn
	case model.StageHuman:
		return to == model.StageApproved || to == model.StageRejected || to == model.StageWithdrawn
	default:
		return false
	}
}

// Counts 统计台账规模。
func (r *Registry) Counts() Counts {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := Counts{Series: len(r.series)}
	for _, items := range r.episodes {
		c.Episodes += len(items)
	}
	for _, s := range r.series {
		c.Parties += len(s.Parties)
	}
	return c
}

// TotalDurationSec 汇总某剧目全部剧集时长。
func (r *Registry) TotalDurationSec(seriesID string) (int, error) {
	items, err := r.Episodes(seriesID)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, e := range items {
		total += e.DurationSec
	}
	return total, nil
}
