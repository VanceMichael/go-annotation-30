package report_test

import (
	"errors"
	"testing"

	"microdrama/internal/model"
	"microdrama/internal/report"
	"microdrama/internal/seed"
)

func builder(t *testing.T) *report.Builder {
	t.Helper()
	reg, tl, err := seed.Load()
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}
	return report.NewBuilder(reg, tl)
}

func TestCatalogReportCoversEverySeries(t *testing.T) {
	rep, err := builder(t).Catalog()
	if err != nil {
		t.Fatalf("生成台账报表失败: %v", err)
	}
	if len(rep.Rows) != len(seed.Series()) {
		t.Fatalf("报表行数 = %d, 期望 %d", len(rep.Rows), len(seed.Series()))
	}
	if rep.Incomplete != 0 {
		t.Fatalf("剧集登记不齐剧目数 = %d, 期望 0", rep.Incomplete)
	}
	sum := 0
	for _, v := range rep.ByGenre {
		sum += v
	}
	if sum != len(seed.Series()) {
		t.Fatalf("按题材分组合计 = %d, 期望 %d", sum, len(seed.Series()))
	}
	for _, row := range rep.Rows {
		if row.Registered != row.Episodes {
			t.Fatalf("剧目 %s 已登记 %d 集, 备案 %d 集", row.SeriesID, row.Registered, row.Episodes)
		}
		if row.DurationSec <= 0 {
			t.Fatalf("剧目 %s 总时长 = %d", row.SeriesID, row.DurationSec)
		}
	}
}

func TestRatingReportCoversEverySeries(t *testing.T) {
	rep, err := builder(t).Rating()
	if err != nil {
		t.Fatalf("生成分级报表失败: %v", err)
	}
	if rep.Summary.Series != len(seed.Series()) {
		t.Fatalf("判定剧目数 = %d, 期望 %d", rep.Summary.Series, len(seed.Series()))
	}
	if rep.Summary.Publishable+rep.Summary.Blocked != rep.Summary.Series {
		t.Fatalf("允许播出 %d + 不予播出 %d != 总数 %d",
			rep.Summary.Publishable, rep.Summary.Blocked, rep.Summary.Series)
	}
}

func TestPlaysReportMatchesTally(t *testing.T) {
	rep := builder(t).Plays()
	records := seed.PlayRecords()
	want := 0
	perSeries := map[string]int{}
	for _, rec := range records {
		want += rec.Plays
		perSeries[rec.SeriesID] += rec.Plays
	}
	if rep.Snapshot.Total != want {
		t.Fatalf("报表合计播放量 = %d, 期望 %d", rep.Snapshot.Total, want)
	}
	for id, v := range perSeries {
		if rep.BySeries[id] != v {
			t.Fatalf("剧目 %s 播放量 = %d, 期望 %d", id, rep.BySeries[id], v)
		}
	}
	if len(rep.DayTotals) != len(seed.Days()) {
		t.Fatalf("自然日数 = %d, 期望 %d", len(rep.DayTotals), len(seed.Days()))
	}
	daySum := 0
	for _, v := range rep.DayTotals {
		daySum += v
	}
	if daySum != want {
		t.Fatalf("逐日合计 = %d, 期望 %d", daySum, want)
	}
}

func TestSettlementReportIsBalanced(t *testing.T) {
	rep, err := builder(t).Settlement(seed.RateFenPerKilo)
	if err != nil {
		t.Fatalf("生成结算报表失败: %v", err)
	}
	if rep.Unbalanced != 0 {
		t.Fatalf("不闭合结算单数 = %d, 期望 0", rep.Unbalanced)
	}
	if rep.DiffFen != 0 {
		t.Fatalf("分账偏差 = %d 分, 期望 0", rep.DiffFen)
	}
	if !rep.Book.Balanced() {
		t.Fatalf("整本账不闭合: 待分账 %d, 各方合计 %d", rep.Book.TotalFen, rep.Book.AllocatedFen)
	}
	if len(rep.Book.Statements) != len(seed.Series()) {
		t.Fatalf("结算单数 = %d, 期望 %d", len(rep.Book.Statements), len(seed.Series()))
	}
}

func TestSettlementReportAcrossRates(t *testing.T) {
	b := builder(t)
	for _, rate := range []int64{1, 7, 99, 1187, 20001} {
		rep, err := b.Settlement(rate)
		if err != nil {
			t.Fatalf("单价 %d 结算失败: %v", rate, err)
		}
		if rep.DiffFen != 0 || rep.Unbalanced != 0 {
			t.Fatalf("单价 %d 时偏差 %d 分, 不闭合 %d 张", rate, rep.DiffFen, rep.Unbalanced)
		}
	}
}

func TestSettlementRejectsNonPositiveRate(t *testing.T) {
	if _, err := builder(t).Settlement(0); !errors.Is(err, model.ErrSettleMismatch) {
		t.Fatalf("零单价应返回 ErrSettleMismatch, 实际 %v", err)
	}
}

func TestOverviewSummarises(t *testing.T) {
	ov := builder(t).Overview()
	if ov.Counts.Series != len(seed.Series()) {
		t.Fatalf("剧目数 = %d, 期望 %d", ov.Counts.Series, len(seed.Series()))
	}
	if ov.Days != len(seed.Days()) {
		t.Fatalf("自然日数 = %d, 期望 %d", ov.Days, len(seed.Days()))
	}
	if ov.TotalPlays <= 0 {
		t.Fatalf("合计播放量 = %d", ov.TotalPlays)
	}
	if ov.Publishable != 0 {
		t.Fatalf("初始允许播出剧目数 = %d, 期望 0", ov.Publishable)
	}
}

func TestPlaysReportTopTagsBounded(t *testing.T) {
	rep := builder(t).Plays()
	if len(rep.TopTags) == 0 || len(rep.TopTags) > 5 {
		t.Fatalf("TopTags 条数 = %d, 期望 1..5", len(rep.TopTags))
	}
	for i := 1; i < len(rep.TopTags); i++ {
		if rep.TopTags[i-1].Plays < rep.TopTags[i].Plays {
			t.Fatal("TopTags 未按播放量降序")
		}
	}
}
