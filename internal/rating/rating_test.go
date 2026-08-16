package rating_test

import (
	"errors"
	"testing"

	"microdrama/internal/model"
	"microdrama/internal/rating"
	"microdrama/internal/seed"
)

func eps(tagSets ...[]string) []model.Episode {
	out := make([]model.Episode, 0, len(tagSets))
	for i, tags := range tagSets {
		out = append(out, model.Episode{
			SeriesID: "MD-T-001", No: i + 1, DurationSec: 90, Tags: tags,
		})
	}
	return out
}

func TestAssessPicksStrictestRating(t *testing.T) {
	d, err := rating.Assess("MD-T-001", eps(
		[]string{"daily"},
		[]string{"romance"},
		[]string{"violence-mild"},
	))
	if err != nil {
		t.Fatalf("判定失败: %v", err)
	}
	if d.Rating != model.RatingAdult {
		t.Fatalf("分级 = %q, 期望 adult", d.Rating)
	}
	if !d.Publishable {
		t.Fatal("adult 分级应允许播出")
	}
}

func TestAssessBlocksIllegalContent(t *testing.T) {
	d, err := rating.Assess("MD-T-001", eps([]string{"daily"}, []string{"illegal-content"}))
	if err != nil {
		t.Fatalf("判定失败: %v", err)
	}
	if d.Rating != model.RatingBlocked {
		t.Fatalf("分级 = %q, 期望 blocked", d.Rating)
	}
	if d.Publishable {
		t.Fatal("blocked 分级不应允许播出")
	}
}

func TestAssessRequiresEpisodes(t *testing.T) {
	if _, err := rating.Assess("MD-T-001", nil); !errors.Is(err, model.ErrEpisodeUnknown) {
		t.Fatalf("无剧集应返回 ErrEpisodeUnknown, 实际 %v", err)
	}
	if _, err := rating.Assess("  ", eps([]string{"daily"})); !errors.Is(err, model.ErrInvalidSeries) {
		t.Fatalf("空剧目编号应返回 ErrInvalidSeries, 实际 %v", err)
	}
}

func TestAssessEvidenceSortedAndScoped(t *testing.T) {
	d, err := rating.Assess("MD-T-001", eps(
		[]string{"romance", "conflict"},
		[]string{"daily"},
		[]string{"substance"},
	))
	if err != nil {
		t.Fatalf("判定失败: %v", err)
	}
	if len(d.Evidence) != 3 {
		t.Fatalf("判定依据条数 = %d, 期望 3", len(d.Evidence))
	}
	for i := 1; i < len(d.Evidence); i++ {
		a, b := d.Evidence[i-1], d.Evidence[i]
		if a.EpisodeNo > b.EpisodeNo || (a.EpisodeNo == b.EpisodeNo && a.Tag > b.Tag) {
			t.Fatalf("判定依据未按集号与标签排序: %+v", d.Evidence)
		}
	}
	if d.TagCounts["daily"] != 1 {
		t.Fatalf("daily 计数 = %d, 期望 1", d.TagCounts["daily"])
	}
}

func TestStricterAndWeight(t *testing.T) {
	if rating.Stricter(model.RatingAll, model.RatingTeen) != model.RatingTeen {
		t.Fatal("teen 应比 all 严格")
	}
	if rating.Stricter(model.RatingBlocked, model.RatingAdult) != model.RatingBlocked {
		t.Fatal("blocked 应比 adult 严格")
	}
	if rating.Weight(model.RatingAll) >= rating.Weight(model.RatingBlocked) {
		t.Fatal("权重排序错误")
	}
	if rating.Weight("unknown") != -1 {
		t.Fatal("未知分级权重应为 -1")
	}
}

func TestBlockingTagsSortedAndDeduped(t *testing.T) {
	got := rating.BlockingTags(eps(
		[]string{"violence-graphic", "illegal-content"},
		[]string{"illegal-content"},
		[]string{"daily"},
	))
	want := []string{"illegal-content", "violence-graphic"}
	if len(got) != len(want) {
		t.Fatalf("阻断标签 = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("阻断标签 = %v, 期望 %v", got, want)
		}
	}
}

func TestSummariseCountsByRating(t *testing.T) {
	ds := []rating.Decision{
		{Rating: model.RatingAll, Publishable: true},
		{Rating: model.RatingTeen, Publishable: true},
		{Rating: model.RatingBlocked, Publishable: false},
	}
	s := rating.Summarise(ds)
	if s.Series != 3 || s.Publishable != 2 || s.Blocked != 1 {
		t.Fatalf("汇总结果异常: %+v", s)
	}
	if s.ByRating["teen"] != 1 {
		t.Fatalf("teen 计数 = %d, 期望 1", s.ByRating["teen"])
	}
}

func TestDistributionCoversAllRatings(t *testing.T) {
	d := rating.Distribution(nil)
	if len(d) != len(model.AllRatings()) {
		t.Fatalf("分布键数 = %d, 期望 %d", len(d), len(model.AllRatings()))
	}
}

func TestAssessOverSeedData(t *testing.T) {
	reg, err := seed.LoadCatalog()
	if err != nil {
		t.Fatalf("加载台账失败: %v", err)
	}
	for _, id := range seed.SeriesIDs() {
		items, err := reg.Episodes(id)
		if err != nil {
			t.Fatalf("查询剧集失败: %v", err)
		}
		d, err := rating.Assess(id, items)
		if err != nil {
			t.Fatalf("剧目 %s 判定失败: %v", id, err)
		}
		if rating.Weight(d.Rating) < 0 {
			t.Fatalf("剧目 %s 判定出未知分级 %q", id, d.Rating)
		}
	}
}

func TestTagRatingMatchesModel(t *testing.T) {
	for _, tag := range []string{"daily", "romance", "conflict", "violence-mild", "substance", "illegal-content"} {
		if rating.TagRating(tag) != model.RatingFor([]string{tag}) {
			t.Fatalf("标签 %q 的分级与模型不一致", tag)
		}
	}
}
