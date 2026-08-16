package model_test

import (
	"errors"
	"testing"
	"time"

	"microdrama/internal/model"
)

func sample() model.Series {
	return model.Series{
		ID: "MD-T-001", Title: "测试剧目", Genre: model.GenreUrban,
		Studio: "测试厂牌", Episodes: 12, Rating: model.RatingAll,
		Stage: model.StageSubmitted, FiledAt: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC),
		Parties: []model.Party{
			{ID: "P-1", Name: "甲", Role: "producer", ShareBP: 6000},
			{ID: "P-2", Name: "乙", Role: "platform", ShareBP: 4000},
		},
	}
}

func TestParseGenreRoundTrip(t *testing.T) {
	for _, g := range model.AllGenres() {
		got, err := model.ParseGenre(string(g))
		if err != nil {
			t.Fatalf("ParseGenre(%q) 返回错误 %v", g, err)
		}
		if got != g {
			t.Fatalf("ParseGenre(%q) = %q", g, got)
		}
		if got.DisplayName() == "" {
			t.Fatalf("题材 %q 缺少中文名", g)
		}
	}
	if _, err := model.ParseGenre("wuxia"); !errors.Is(err, model.ErrUnknownGenre) {
		t.Fatalf("未知题材应返回 ErrUnknownGenre, 实际 %v", err)
	}
}

func TestParseRatingAndPublishable(t *testing.T) {
	cases := map[model.Rating]bool{
		model.RatingAll:     true,
		model.RatingTeen:    true,
		model.RatingAdult:   true,
		model.RatingBlocked: false,
	}
	for r, want := range cases {
		got, err := model.ParseRating(string(r))
		if err != nil {
			t.Fatalf("ParseRating(%q) 返回错误 %v", r, err)
		}
		if got.Publishable() != want {
			t.Fatalf("分级 %q 的 Publishable = %v, 期望 %v", r, got.Publishable(), want)
		}
	}
	if _, err := model.ParseRating("R18"); !errors.Is(err, model.ErrUnknownRating) {
		t.Fatalf("未知分级应返回 ErrUnknownRating, 实际 %v", err)
	}
}

func TestParseStageAndTerminal(t *testing.T) {
	terminal := map[model.Stage]bool{
		model.StageSubmitted: false,
		model.StageMachine:   false,
		model.StageHuman:     false,
		model.StageApproved:  true,
		model.StageRejected:  true,
		model.StageWithdrawn: true,
	}
	for st, want := range terminal {
		got, err := model.ParseStage(string(st))
		if err != nil {
			t.Fatalf("ParseStage(%q) 返回错误 %v", st, err)
		}
		if got.Terminal() != want {
			t.Fatalf("阶段 %q 的 Terminal = %v, 期望 %v", st, got.Terminal(), want)
		}
	}
	if _, err := model.ParseStage("archived"); !errors.Is(err, model.ErrUnknownStage) {
		t.Fatalf("未知阶段应返回 ErrUnknownStage, 实际 %v", err)
	}
}

func TestSeriesValidateAcceptsCompleteRecord(t *testing.T) {
	if err := sample().Validate(); err != nil {
		t.Fatalf("完整剧目应通过校验, 实际 %v", err)
	}
}

func TestSeriesValidateRejectsShareMismatch(t *testing.T) {
	s := sample()
	s.Parties[1].ShareBP = 3000
	err := s.Validate()
	if !errors.Is(err, model.ErrInvalidSeries) {
		t.Fatalf("分账比例不足应返回 ErrInvalidSeries, 实际 %v", err)
	}
}

func TestSeriesValidateRejectsMissingFields(t *testing.T) {
	mutate := map[string]func(*model.Series){
		"空编号":  func(s *model.Series) { s.ID = "" },
		"空剧名":  func(s *model.Series) { s.Title = "" },
		"非法题材": func(s *model.Series) { s.Genre = "wuxia" },
		"非法分级": func(s *model.Series) { s.Rating = "R18" },
		"零集数":  func(s *model.Series) { s.Episodes = 0 },
		"无备案日": func(s *model.Series) { s.FiledAt = time.Time{} },
		"无参与方": func(s *model.Series) { s.Parties = nil },
	}
	for name, fn := range mutate {
		s := sample()
		fn(&s)
		if err := s.Validate(); err == nil {
			t.Fatalf("%s 应校验失败", name)
		}
	}
}

func TestEpisodeValidate(t *testing.T) {
	good := model.Episode{SeriesID: "MD-T-001", No: 1, Title: "第 1 集", DurationSec: 90}
	if err := good.Validate(); err != nil {
		t.Fatalf("合法剧集应通过校验, 实际 %v", err)
	}
	bad := []model.Episode{
		{No: 1, DurationSec: 90},
		{SeriesID: "MD-T-001", No: 0, DurationSec: 90},
		{SeriesID: "MD-T-001", No: 1, DurationSec: 0},
	}
	for i, e := range bad {
		if err := e.Validate(); !errors.Is(err, model.ErrInvalidEpisode) {
			t.Fatalf("第 %d 个非法剧集应返回 ErrInvalidEpisode, 实际 %v", i, err)
		}
	}
}

func TestSortSeriesIsStableByFilingDate(t *testing.T) {
	items := []model.Series{
		{ID: "B", FiledAt: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "A", FiledAt: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "C", FiledAt: time.Date(2026, time.January, 9, 0, 0, 0, 0, time.UTC)},
	}
	model.SortSeries(items)
	want := []string{"C", "A", "B"}
	for i, id := range want {
		if items[i].ID != id {
			t.Fatalf("排序结果第 %d 项 = %s, 期望 %s", i, items[i].ID, id)
		}
	}
}

func TestRatingForPicksStrictestTag(t *testing.T) {
	cases := []struct {
		tags []string
		want model.Rating
	}{
		{[]string{"daily"}, model.RatingAll},
		{[]string{"romance"}, model.RatingTeen},
		{[]string{"conflict", "daily"}, model.RatingTeen},
		{[]string{"violence-mild"}, model.RatingAdult},
		{[]string{"romance", "substance"}, model.RatingAdult},
		{[]string{"romance", "illegal-content"}, model.RatingBlocked},
		{[]string{"violence-graphic"}, model.RatingBlocked},
		{nil, model.RatingAll},
	}
	for _, c := range cases {
		if got := model.RatingFor(c.tags); got != c.want {
			t.Fatalf("RatingFor(%v) = %q, 期望 %q", c.tags, got, c.want)
		}
	}
}
