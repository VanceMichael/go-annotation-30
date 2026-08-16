package catalog_test

import (
	"errors"
	"testing"

	"microdrama/internal/catalog"
	"microdrama/internal/model"
	"microdrama/internal/seed"
)

func loaded(t *testing.T) *catalog.Registry {
	t.Helper()
	reg, err := seed.LoadCatalog()
	if err != nil {
		t.Fatalf("加载台账失败: %v", err)
	}
	return reg
}

func TestSeedLoadsEverySeriesAndEpisode(t *testing.T) {
	reg := loaded(t)
	c := reg.Counts()
	if c.Series != len(seed.Series()) {
		t.Fatalf("剧目数 = %d, 期望 %d", c.Series, len(seed.Series()))
	}
	if c.Episodes != len(seed.Episodes()) {
		t.Fatalf("剧集数 = %d, 期望 %d", c.Episodes, len(seed.Episodes()))
	}
}

func TestSeriesSortedByFilingDate(t *testing.T) {
	reg := loaded(t)
	items := reg.Series()
	for i := 1; i < len(items); i++ {
		if items[i].FiledAt.Before(items[i-1].FiledAt) {
			t.Fatalf("第 %d 项备案日期早于前一项", i)
		}
	}
}

func TestLookupUnknownSeries(t *testing.T) {
	reg := loaded(t)
	if _, err := reg.Lookup("MD-9999-999"); !errors.Is(err, model.ErrSeriesUnknown) {
		t.Fatalf("未知剧目应返回 ErrSeriesUnknown, 实际 %v", err)
	}
}

func TestEpisodesSortedByNumber(t *testing.T) {
	reg := loaded(t)
	for _, id := range seed.SeriesIDs() {
		eps, err := reg.Episodes(id)
		if err != nil {
			t.Fatalf("查询剧目 %s 剧集失败: %v", id, err)
		}
		for i, e := range eps {
			if e.No != i+1 {
				t.Fatalf("剧目 %s 第 %d 项集号 = %d", id, i, e.No)
			}
		}
	}
}

func TestEpisodeLookup(t *testing.T) {
	reg := loaded(t)
	e, err := reg.Episode("MD-2026-001", 3)
	if err != nil {
		t.Fatalf("查询第 3 集失败: %v", err)
	}
	if e.No != 3 || e.SeriesID != "MD-2026-001" {
		t.Fatalf("返回了错误的剧集: %+v", e)
	}
	if _, err := reg.Episode("MD-2026-001", 999); !errors.Is(err, model.ErrEpisodeUnknown) {
		t.Fatalf("不存在的集号应返回 ErrEpisodeUnknown, 实际 %v", err)
	}
}

func TestAddSeriesRejectsDuplicate(t *testing.T) {
	reg := loaded(t)
	if err := reg.AddSeries(seed.Series()[0]); !errors.Is(err, model.ErrInvalidSeries) {
		t.Fatalf("重复登记应返回 ErrInvalidSeries, 实际 %v", err)
	}
}

func TestAddEpisodeRejectsUnknownSeries(t *testing.T) {
	reg := loaded(t)
	err := reg.AddEpisode(model.Episode{SeriesID: "MD-9999-999", No: 1, DurationSec: 60})
	if !errors.Is(err, model.ErrSeriesUnknown) {
		t.Fatalf("未知剧目应返回 ErrSeriesUnknown, 实际 %v", err)
	}
}

func TestAddEpisodeRejectsOverfiling(t *testing.T) {
	reg := loaded(t)
	err := reg.AddEpisode(model.Episode{SeriesID: "MD-2026-001", No: 999, DurationSec: 60})
	if !errors.Is(err, model.ErrInvalidEpisode) {
		t.Fatalf("超出备案集数应返回 ErrInvalidEpisode, 实际 %v", err)
	}
}

func TestAllowedTransition(t *testing.T) {
	ok := [][2]model.Stage{
		{model.StageSubmitted, model.StageMachine},
		{model.StageMachine, model.StageHuman},
		{model.StageHuman, model.StageApproved},
		{model.StageHuman, model.StageRejected},
		{model.StageMachine, model.StageWithdrawn},
	}
	for _, p := range ok {
		if !catalog.AllowedTransition(p[0], p[1]) {
			t.Fatalf("%s -> %s 应被允许", p[0], p[1])
		}
	}
	bad := [][2]model.Stage{
		{model.StageSubmitted, model.StageApproved},
		{model.StageApproved, model.StageHuman},
		{model.StageRejected, model.StageApproved},
		{model.StageHuman, model.StageHuman},
	}
	for _, p := range bad {
		if catalog.AllowedTransition(p[0], p[1]) {
			t.Fatalf("%s -> %s 不应被允许", p[0], p[1])
		}
	}
}

func TestSetStageRejectsIllegalTransition(t *testing.T) {
	reg := loaded(t)
	if _, err := reg.SetStage("MD-2026-001", model.StageApproved); !errors.Is(err, model.ErrStageConflict) {
		t.Fatalf("跨阶段流转应返回 ErrStageConflict, 实际 %v", err)
	}
	if _, err := reg.SetStage("MD-2026-001", model.StageMachine); err != nil {
		t.Fatalf("合法流转失败: %v", err)
	}
	s, err := reg.Lookup("MD-2026-001")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if s.Stage != model.StageMachine {
		t.Fatalf("阶段 = %q, 期望 machine", s.Stage)
	}
}

func TestSetRatingRejectsUnknownRating(t *testing.T) {
	reg := loaded(t)
	if _, err := reg.SetRating("MD-2026-001", "R18"); !errors.Is(err, model.ErrUnknownRating) {
		t.Fatalf("未知分级应返回 ErrUnknownRating, 实际 %v", err)
	}
}

func TestPublishableRequiresApproval(t *testing.T) {
	reg := loaded(t)
	if got := len(reg.Publishable()); got != 0 {
		t.Fatalf("初始状态允许播出剧目数 = %d, 期望 0", got)
	}
	for _, st := range []model.Stage{model.StageMachine, model.StageHuman, model.StageApproved} {
		if _, err := reg.SetStage("MD-2026-001", st); err != nil {
			t.Fatalf("流转到 %s 失败: %v", st, err)
		}
	}
	if got := len(reg.Publishable()); got != 1 {
		t.Fatalf("允许播出剧目数 = %d, 期望 1", got)
	}
}

func TestPartyLookup(t *testing.T) {
	reg := loaded(t)
	p, err := reg.Party("MD-2026-001", "P-B01")
	if err != nil {
		t.Fatalf("查询参与方失败: %v", err)
	}
	if p.ShareBP != 3000 {
		t.Fatalf("参与方比例 = %d, 期望 3000", p.ShareBP)
	}
	if _, err := reg.Party("MD-2026-001", "P-ZZZ"); !errors.Is(err, model.ErrPartyUnknown) {
		t.Fatalf("未知参与方应返回 ErrPartyUnknown, 实际 %v", err)
	}
}

func TestEpisodesReturnsCopy(t *testing.T) {
	reg := loaded(t)
	first, err := reg.Episodes("MD-2026-001")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	first[0].Title = "被改写"
	second, err := reg.Episodes("MD-2026-001")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if second[0].Title == "被改写" {
		t.Fatal("调用方修改返回值污染了台账内部数据")
	}
}

func TestTotalDurationSecPositive(t *testing.T) {
	reg := loaded(t)
	for _, id := range seed.SeriesIDs() {
		dur, err := reg.TotalDurationSec(id)
		if err != nil {
			t.Fatalf("统计剧目 %s 时长失败: %v", id, err)
		}
		if dur <= 0 {
			t.Fatalf("剧目 %s 总时长 = %d", id, dur)
		}
	}
}

func TestSeriesByGenreAndStage(t *testing.T) {
	reg := loaded(t)
	total := 0
	for _, g := range model.AllGenres() {
		total += len(reg.SeriesByGenre(g))
	}
	if total != len(seed.Series()) {
		t.Fatalf("按题材分组合计 = %d, 期望 %d", total, len(seed.Series()))
	}
	if got := len(reg.SeriesByStage(model.StageSubmitted)); got != len(seed.Series()) {
		t.Fatalf("已提交剧目数 = %d, 期望 %d", got, len(seed.Series()))
	}
}
