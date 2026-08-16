package tally_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"microdrama/internal/model"
	"microdrama/internal/seed"
	"microdrama/internal/tally"
)

// TestRecordInitialisesInnerMap 覆盖首次遇到某个自然日时的入账。
func TestRecordInitialisesInnerMap(t *testing.T) {
	tl := tally.New()
	tl.Record("2026-08-10", "romance", 120)
	if got := tl.Plays("2026-08-10", "romance"); got != 120 {
		t.Fatalf("播放量 = %d, 期望 120", got)
	}
	if got := tl.DayTotal("2026-08-10"); got != 120 {
		t.Fatalf("当日合计 = %d, 期望 120", got)
	}
	if days := tl.Days(); len(days) != 1 || days[0] != "2026-08-10" {
		t.Fatalf("自然日列表 = %v, 期望 [2026-08-10]", days)
	}
}

// TestRecordAcrossDaysAndTags 覆盖多日多标签的累加。
func TestRecordAcrossDaysAndTags(t *testing.T) {
	tl := tally.New()
	days := []string{"2026-08-10", "2026-08-11", "2026-08-12"}
	tags := []string{"daily", "romance", "conflict"}
	want := 0
	for di, d := range days {
		for ti, tag := range tags {
			plays := 100*(di+1) + ti
			tl.Record(d, tag, plays)
			want += plays
		}
	}
	if got := len(tl.Days()); got != len(days) {
		t.Fatalf("自然日数 = %d, 期望 %d", got, len(days))
	}
	for _, d := range days {
		if got := len(tl.Tags(d)); got != len(tags) {
			t.Fatalf("%s 的标签数 = %d, 期望 %d", d, got, len(tags))
		}
	}
	if got := tl.Total(); got != want {
		t.Fatalf("合计播放量 = %d, 期望 %d", got, want)
	}
}

func TestRecordAccumulatesSameCell(t *testing.T) {
	tl := tally.New()
	tl.Record("2026-08-10", "daily", 10)
	tl.Record("2026-08-10", "daily", 32)
	if got := tl.Plays("2026-08-10", "daily"); got != 42 {
		t.Fatalf("同一格累加结果 = %d, 期望 42", got)
	}
}

func TestAddValidatesRecord(t *testing.T) {
	tl := tally.New()
	bad := []model.PlayRecord{
		{SeriesID: "MD-T-001", Day: "", Tag: "daily", Plays: 1},
		{SeriesID: "MD-T-001", Day: "2026-08-10", Tag: " ", Plays: 1},
		{SeriesID: "MD-T-001", Day: "2026-08-10", Tag: "daily", Plays: -1},
	}
	for i, rec := range bad {
		if err := tl.Add(rec); !errors.Is(err, model.ErrTallyMissing) {
			t.Fatalf("第 %d 条非法上报应返回 ErrTallyMissing, 实际 %v", i, err)
		}
	}
	if got := tl.Rows(); got != 0 {
		t.Fatalf("非法上报不应入账, 实际条数 %d", got)
	}
}

func TestAddAllTracksSeriesTotals(t *testing.T) {
	tl := tally.New()
	records := seed.PlayRecords()
	if err := tl.AddAll(records); err != nil {
		t.Fatalf("批量入账失败: %v", err)
	}
	perSeries := map[string]int{}
	total := 0
	for _, rec := range records {
		perSeries[rec.SeriesID] += rec.Plays
		total += rec.Plays
	}
	for id, want := range perSeries {
		if got := tl.SeriesTotal(id); got != want {
			t.Fatalf("剧目 %s 累计播放量 = %d, 期望 %d", id, got, want)
		}
	}
	if got := tl.Total(); got != total {
		t.Fatalf("合计播放量 = %d, 期望 %d", got, total)
	}
	if got := tl.Rows(); got != len(records) {
		t.Fatalf("入账条数 = %d, 期望 %d", got, len(records))
	}
}

func TestSeedRecordsCoverEveryDay(t *testing.T) {
	tl := tally.New()
	if err := tl.AddAll(seed.PlayRecords()); err != nil {
		t.Fatalf("批量入账失败: %v", err)
	}
	got := tl.Days()
	want := seed.Days()
	if len(got) != len(want) {
		t.Fatalf("自然日 = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("自然日 = %v, 期望 %v", got, want)
		}
	}
}

func TestCellsSortedByDayThenTag(t *testing.T) {
	tl := tally.New()
	tl.Record("2026-08-11", "romance", 5)
	tl.Record("2026-08-10", "romance", 5)
	tl.Record("2026-08-10", "conflict", 5)
	cells := tl.Cells()
	if len(cells) != 3 {
		t.Fatalf("统计格数 = %d, 期望 3", len(cells))
	}
	for i := 1; i < len(cells); i++ {
		a, b := cells[i-1], cells[i]
		if a.Day > b.Day || (a.Day == b.Day && a.Tag > b.Tag) {
			t.Fatalf("统计格未排序: %+v", cells)
		}
	}
}

func TestTagTotalAcrossDays(t *testing.T) {
	tl := tally.New()
	tl.Record("2026-08-10", "romance", 7)
	tl.Record("2026-08-11", "romance", 13)
	tl.Record("2026-08-11", "daily", 100)
	if got := tl.TagTotal("romance"); got != 20 {
		t.Fatalf("romance 跨日合计 = %d, 期望 20", got)
	}
}

func TestTopTagsOrderedByPlays(t *testing.T) {
	tl := tally.New()
	tl.Record("2026-08-10", "daily", 10)
	tl.Record("2026-08-10", "romance", 50)
	tl.Record("2026-08-11", "conflict", 30)
	top := tl.TopTags(2)
	if len(top) != 2 {
		t.Fatalf("TopTags 返回 %d 项, 期望 2", len(top))
	}
	if top[0].Tag != "romance" || top[1].Tag != "conflict" {
		t.Fatalf("TopTags 顺序错误: %+v", top)
	}
}

func TestSnapshotMatchesCells(t *testing.T) {
	tl := tally.New()
	if err := tl.AddAll(seed.PlayRecords()); err != nil {
		t.Fatalf("批量入账失败: %v", err)
	}
	snap := tl.Snapshot()
	sum := 0
	for _, c := range snap.Cells {
		sum += c.Plays
	}
	if snap.Total != sum {
		t.Fatalf("快照合计 = %d, 各格之和 = %d", snap.Total, sum)
	}
	if snap.Days != len(tl.Days()) {
		t.Fatalf("快照自然日数 = %d, 期望 %d", snap.Days, len(tl.Days()))
	}
}

func TestConcurrentRecordKeepsTotal(t *testing.T) {
	tl := tally.New()
	const workers, per = 16, 200
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				tl.Record(fmt.Sprintf("2026-09-%02d", 1+i%20), fmt.Sprintf("t-%d", w%4), 1)
			}
		}(w)
	}
	wg.Wait()
	if got := tl.Total(); got != workers*per {
		t.Fatalf("并发入账合计 = %d, 期望 %d", got, workers*per)
	}
}

func TestRecordOnPreparedDay(t *testing.T) {
	tl := tally.NewFor(seed.Days())
	tl.Record(seed.Days()[0], "romance", 900)
	if got := tl.Plays(seed.Days()[0], "romance"); got != 900 {
		t.Fatalf("预建自然日入账 = %d, 期望 900", got)
	}
}

// TestRecordAcceptsDayOutsidePreparedWindow 覆盖预建窗口之外的自然日补报。
func TestRecordAcceptsDayOutsidePreparedWindow(t *testing.T) {
	tl := tally.NewFor(seed.Days())
	if err := tl.AddAll(seed.PlayRecords()); err != nil {
		t.Fatalf("批量入账失败: %v", err)
	}
	before := tl.Total()

	tl.Record("2026-08-20", "romance", 12000)
	if got := tl.Plays("2026-08-20", "romance"); got != 12000 {
		t.Fatalf("新自然日入账 = %d, 期望 12000", got)
	}
	if got := tl.Total(); got != before+12000 {
		t.Fatalf("补报后合计 = %d, 期望 %d", got, before+12000)
	}
	if got := len(tl.Days()); got != len(seed.Days())+1 {
		t.Fatalf("自然日数 = %d, 期望 %d", got, len(seed.Days())+1)
	}
}

func TestAddAcceptsDayOutsidePreparedWindow(t *testing.T) {
	tl := tally.NewFor(seed.Days())
	rec := model.PlayRecord{SeriesID: "MD-2026-002", Day: "2026-09-01", Tag: "conflict", Plays: 777}
	if err := tl.Add(rec); err != nil {
		t.Fatalf("补报失败: %v", err)
	}
	if got := tl.SeriesTotal("MD-2026-002"); got != 777 {
		t.Fatalf("剧目累计 = %d, 期望 777", got)
	}
	if got := tl.DayTotal("2026-09-01"); got != 777 {
		t.Fatalf("当日合计 = %d, 期望 777", got)
	}
}

func TestPrepareIsIdempotent(t *testing.T) {
	tl := tally.NewFor(seed.Days())
	tl.Record(seed.Days()[1], "daily", 40)
	tl.Prepare(seed.Days()...)
	if got := tl.Plays(seed.Days()[1], "daily"); got != 40 {
		t.Fatalf("重复预建清空了已有数据, 播放量 = %d", got)
	}
	if got := len(tl.Days()); got != len(seed.Days()) {
		t.Fatalf("自然日数 = %d, 期望 %d", got, len(seed.Days()))
	}
}
