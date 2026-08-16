// Package report 汇总台账、分级、播放量与分账结算报表。
package report

import (
	"fmt"
	"sort"

	"microdrama/internal/catalog"
	"microdrama/internal/model"
	"microdrama/internal/rating"
	"microdrama/internal/settle"
	"microdrama/internal/tally"
)

// Builder 生成各类报表。
type Builder struct {
	reg *catalog.Registry
	tl  *tally.Tally
}

// NewBuilder 构造报表生成器。
func NewBuilder(reg *catalog.Registry, tl *tally.Tally) *Builder {
	return &Builder{reg: reg, tl: tl}
}

// CatalogRow 是台账报表的一行。
type CatalogRow struct {
	SeriesID string `json:"series_id"`
	Title    string `json:"title"`
	Genre    string `json:"genre"`
	Studio   string `json:"studio"`
	Stage    string `json:"stage"`
	Rating   string `json:"rating"`
	Episodes int    `json:"episodes"`
	// Registered 是已登记剧集数。
	Registered int `json:"registered"`
	// DurationSec 是已登记剧集总时长，单位秒。
	DurationSec int    `json:"duration_sec"`
	FiledAt     string `json:"filed_at"`
}

// CatalogReport 是剧目台账报表。
type CatalogReport struct {
	Rows []CatalogRow `json:"rows"`
	// ByGenre 是各题材剧目数。
	ByGenre map[string]int `json:"by_genre"`
	// ByStage 是各审核阶段剧目数。
	ByStage map[string]int `json:"by_stage"`
	Counts  catalog.Counts `json:"counts"`
	// Incomplete 是剧集登记不齐的剧目数。
	Incomplete int `json:"incomplete"`
}

// Catalog 生成剧目台账报表。
func (b *Builder) Catalog() (CatalogReport, error) {
	rep := CatalogReport{
		ByGenre: map[string]int{},
		ByStage: map[string]int{},
		Counts:  b.reg.Counts(),
	}
	for _, g := range model.AllGenres() {
		rep.ByGenre[string(g)] = 0
	}
	for _, st := range model.AllStages() {
		rep.ByStage[string(st)] = 0
	}
	for _, s := range b.reg.Series() {
		eps, err := b.reg.Episodes(s.ID)
		if err != nil {
			return CatalogReport{}, err
		}
		dur, err := b.reg.TotalDurationSec(s.ID)
		if err != nil {
			return CatalogReport{}, err
		}
		rep.Rows = append(rep.Rows, CatalogRow{
			SeriesID:    s.ID,
			Title:       s.Title,
			Genre:       string(s.Genre),
			Studio:      s.Studio,
			Stage:       string(s.Stage),
			Rating:      string(s.Rating),
			Episodes:    s.Episodes,
			Registered:  len(eps),
			DurationSec: dur,
			FiledAt:     s.FiledAt.Format("2006-01-02"),
		})
		rep.ByGenre[string(s.Genre)]++
		rep.ByStage[string(s.Stage)]++
		if len(eps) == 0 {
			rep.Incomplete++
		}
	}
	return rep, nil
}

// RatingReport 是分级判定报表。
type RatingReport struct {
	Decisions []rating.Decision `json:"decisions"`
	Summary   rating.Summary    `json:"summary"`
	// BlockingTags 是导致不予播出的标签。
	BlockingTags []string `json:"blocking_tags"`
}

// Rating 生成分级判定报表。
func (b *Builder) Rating() (RatingReport, error) {
	rep := RatingReport{}
	blocking := map[string]bool{}
	for _, s := range b.reg.Series() {
		eps, err := b.reg.Episodes(s.ID)
		if err != nil {
			return RatingReport{}, err
		}
		if len(eps) == 0 {
			continue
		}
		d, err := rating.Assess(s.ID, eps)
		if err != nil {
			return RatingReport{}, err
		}
		rep.Decisions = append(rep.Decisions, d)
		for _, tag := range rating.BlockingTags(eps) {
			blocking[tag] = true
		}
	}
	rep.Summary = rating.Summarise(rep.Decisions)
	for tag := range blocking {
		rep.BlockingTags = append(rep.BlockingTags, tag)
	}
	sort.Strings(rep.BlockingTags)
	return rep, nil
}

// PlaysReport 是播放量报表。
type PlaysReport struct {
	Snapshot tally.Snapshot `json:"snapshot"`
	// TopTags 是播放量最高的标签。
	TopTags []tally.Cell `json:"top_tags"`
	// BySeries 是各剧目累计播放量。
	BySeries map[string]int `json:"by_series"`
	// DayTotals 是各自然日播放量合计。
	DayTotals map[string]int `json:"day_totals"`
}

// Plays 生成播放量报表。
func (b *Builder) Plays() PlaysReport {
	rep := PlaysReport{
		Snapshot:  b.tl.Snapshot(),
		TopTags:   b.tl.TopTags(5),
		BySeries:  map[string]int{},
		DayTotals: map[string]int{},
	}
	for _, s := range b.reg.Series() {
		rep.BySeries[s.ID] = b.tl.SeriesTotal(s.ID)
	}
	for _, day := range b.tl.Days() {
		rep.DayTotals[day] = b.tl.DayTotal(day)
	}
	return rep
}

// SettlementReport 是分账结算报表。
type SettlementReport struct {
	Book settle.Book `json:"book"`
	// RateFenPerKilo 是每千次播放的结算单价，单位分。
	RateFenPerKilo int64 `json:"rate_fen_per_kilo"`
	// Unbalanced 是不闭合的结算单数量。
	Unbalanced int `json:"unbalanced"`
	// DiffFen 是各方分得金额之和相对待分账总额的偏差，单位分。
	DiffFen int64 `json:"diff_fen"`
	// TotalYuan 是待分账总额，单位元。
	TotalYuan string `json:"total_yuan"`
}

// Settlement 生成分账结算报表。
func (b *Builder) Settlement(rateFenPerKilo int64) (SettlementReport, error) {
	if rateFenPerKilo <= 0 {
		return SettlementReport{}, fmt.Errorf("%w: 结算单价必须为正", model.ErrSettleMismatch)
	}
	statements := make([]settle.Statement, 0, 8)
	for _, s := range b.reg.Series() {
		plays := b.tl.SeriesTotal(s.ID)
		if plays == 0 {
			continue
		}
		st, err := settle.Compute(s, plays, rateFenPerKilo)
		if err != nil {
			return SettlementReport{}, err
		}
		statements = append(statements, st)
	}
	book := settle.BuildBook(statements)
	return SettlementReport{
		Book:           book,
		RateFenPerKilo: rateFenPerKilo,
		Unbalanced:     book.Unbalanced,
		DiffFen:        book.AllocatedFen - book.TotalFen,
		TotalYuan:      settle.Yuan(book.TotalFen),
	}, nil
}

// Overview 是首页概览。
type Overview struct {
	Counts catalog.Counts `json:"counts"`
	// Publishable 是允许播出的剧目数。
	Publishable int `json:"publishable"`
	// TotalPlays 是全部播放量合计。
	TotalPlays int `json:"total_plays"`
	// Days 是已入账的自然日数。
	Days int `json:"days"`
}

// Overview 生成首页概览。
func (b *Builder) Overview() Overview {
	return Overview{
		Counts:      b.reg.Counts(),
		Publishable: len(b.reg.Publishable()),
		TotalPlays:  b.tl.Total(),
		Days:        len(b.tl.Days()),
	}
}
