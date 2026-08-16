// Package seed 提供内置演示数据，保证各命令与测试在无外部依赖时可复现。
package seed

import (
	"fmt"
	"time"

	"microdrama/internal/catalog"
	"microdrama/internal/model"
	"microdrama/internal/tally"
)

// Year 是当前结算年度。
const Year = 2026

// RateFenPerKilo 是每千次播放的结算单价，单位分。
const RateFenPerKilo int64 = 1187

// Days 是内置播放量上报覆盖的自然日。
func Days() []string {
	return []string{"2026-08-10", "2026-08-11", "2026-08-12", "2026-08-13", "2026-08-14"}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Series 返回内置剧目。
func Series() []model.Series {
	return []model.Series{
		{
			ID: "MD-2026-001", Title: "潮汐来信", Genre: model.GenreUrban,
			Studio: "星潮文化", Episodes: 24, Rating: model.RatingAll,
			Stage: model.StageSubmitted, FiledAt: day(2026, time.March, 9),
			Parties: []model.Party{
				{ID: "P-A01", Name: "星潮文化", Role: "producer", ShareBP: 4500},
				{ID: "P-B01", Name: "短剧云平台", Role: "platform", ShareBP: 3000},
				{ID: "P-C01", Name: "编剧工作室", Role: "writer", ShareBP: 1500},
				{ID: "P-D01", Name: "主演团队", Role: "cast", ShareBP: 1000},
			},
		},
		{
			ID: "MD-2026-002", Title: "长安夜行", Genre: model.GenreCostume,
			Studio: "云栖影业", Episodes: 30, Rating: model.RatingAll,
			Stage: model.StageSubmitted, FiledAt: day(2026, time.March, 21),
			Parties: []model.Party{
				{ID: "P-A02", Name: "云栖影业", Role: "producer", ShareBP: 3333},
				{ID: "P-B02", Name: "短剧云平台", Role: "platform", ShareBP: 3333},
				{ID: "P-C02", Name: "服化道联合体", Role: "vendor", ShareBP: 3334},
			},
		},
		{
			ID: "MD-2026-003", Title: "暗流三十七度", Genre: model.GenreSuspense,
			Studio: "长夜制作", Episodes: 20, Rating: model.RatingAll,
			Stage: model.StageSubmitted, FiledAt: day(2026, time.April, 6),
			Parties: []model.Party{
				{ID: "P-A03", Name: "长夜制作", Role: "producer", ShareBP: 5000},
				{ID: "P-B03", Name: "短剧云平台", Role: "platform", ShareBP: 2500},
				{ID: "P-C03", Name: "剪辑团队", Role: "editor", ShareBP: 1500},
				{ID: "P-D03", Name: "配乐团队", Role: "score", ShareBP: 1000},
			},
		},
		{
			ID: "MD-2026-004", Title: "顶楼与地下", Genre: model.GenreRealism,
			Studio: "平川纪实", Episodes: 18, Rating: model.RatingAll,
			Stage: model.StageSubmitted, FiledAt: day(2026, time.May, 18),
			Parties: []model.Party{
				{ID: "P-A04", Name: "平川纪实", Role: "producer", ShareBP: 6000},
				{ID: "P-B04", Name: "短剧云平台", Role: "platform", ShareBP: 2500},
				{ID: "P-C04", Name: "摄影团队", Role: "camera", ShareBP: 1500},
			},
		},
		{
			ID: "MD-2026-005", Title: "加班奇遇", Genre: model.GenreComedy,
			Studio: "开工文娱", Episodes: 26, Rating: model.RatingAll,
			Stage: model.StageSubmitted, FiledAt: day(2026, time.June, 1),
			Parties: []model.Party{
				{ID: "P-A05", Name: "开工文娱", Role: "producer", ShareBP: 4000},
				{ID: "P-B05", Name: "短剧云平台", Role: "platform", ShareBP: 3500},
				{ID: "P-C05", Name: "喜剧编剧组", Role: "writer", ShareBP: 2500},
			},
		},
	}
}

// episodeSpec 描述一批剧集的生成规则。
type episodeSpec struct {
	seriesID string
	count    int
	baseSec  int
	stepSec  int
	// tagCycle 是按集号轮换的标签组。
	tagCycle [][]string
}

func specs() []episodeSpec {
	return []episodeSpec{
		{
			seriesID: "MD-2026-001", count: 24, baseSec: 92, stepSec: 3,
			tagCycle: [][]string{
				{"daily"},
				{"romance"},
				{"romance", "conflict"},
				{"daily", "romance"},
			},
		},
		{
			seriesID: "MD-2026-002", count: 30, baseSec: 105, stepSec: 2,
			tagCycle: [][]string{
				{"daily"},
				{"conflict"},
				{"romance", "conflict"},
			},
		},
		{
			seriesID: "MD-2026-003", count: 20, baseSec: 118, stepSec: 4,
			tagCycle: [][]string{
				{"conflict"},
				{"violence-mild"},
				{"conflict", "violence-mild"},
				{"substance"},
			},
		},
		{
			seriesID: "MD-2026-004", count: 18, baseSec: 130, stepSec: 5,
			tagCycle: [][]string{
				{"daily"},
				{"conflict"},
			},
		},
		{
			seriesID: "MD-2026-005", count: 26, baseSec: 88, stepSec: 2,
			tagCycle: [][]string{
				{"daily"},
				{"daily", "romance"},
			},
		},
	}
}

// Episodes 返回内置剧集。
func Episodes() []model.Episode {
	out := make([]model.Episode, 0, 128)
	for _, sp := range specs() {
		for no := 1; no <= sp.count; no++ {
			tags := sp.tagCycle[(no-1)%len(sp.tagCycle)]
			copied := make([]string, len(tags))
			copy(copied, tags)
			out = append(out, model.Episode{
				SeriesID:    sp.seriesID,
				No:          no,
				Title:       fmt.Sprintf("第 %d 集", no),
				DurationSec: sp.baseSec + (no-1)*sp.stepSec%17,
				Tags:        copied,
			})
		}
	}
	return out
}

// playTotals 是各剧目在统计窗口内的累计播放量。
func playTotals() []struct {
	SeriesID string
	Total    int
} {
	return []struct {
		SeriesID string
		Total    int
	}{
		{"MD-2026-001", 1284515},
		{"MD-2026-002", 968232},
		{"MD-2026-003", 1507402},
		{"MD-2026-004", 742887},
		{"MD-2026-005", 1133060},
	}
}

// seriesTags 返回参与播放量拆分的标签，顺序固定。
func seriesTags(seriesID string) []string {
	switch seriesID {
	case "MD-2026-001":
		return []string{"daily", "romance", "conflict"}
	case "MD-2026-002":
		return []string{"daily", "conflict", "romance"}
	case "MD-2026-003":
		return []string{"conflict", "violence-mild", "substance"}
	case "MD-2026-004":
		return []string{"daily", "conflict"}
	default:
		return []string{"daily", "romance"}
	}
}

// weights 是逐日逐标签的拆分权重，保证拆分结果稳定可复现。
func weights(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = 7 + (i*13)%29
	}
	return out
}

// spread 把剧目累计播放量按固定权重拆成逐日逐标签的上报，最后一条承接余额。
func spread(seriesID string, total int, at time.Time) []model.PlayRecord {
	days := Days()
	tags := seriesTags(seriesID)
	n := len(days) * len(tags)
	w := weights(n)
	sum := 0
	for _, v := range w {
		sum += v
	}
	out := make([]model.PlayRecord, 0, n)
	assigned := 0
	idx := 0
	for di, d := range days {
		for _, tag := range tags {
			plays := total * w[idx] / sum
			if idx == n-1 {
				plays = total - assigned
			} else {
				assigned += plays
			}
			out = append(out, model.PlayRecord{
				SeriesID: seriesID,
				Day:      d,
				Tag:      tag,
				Plays:    plays,
				At:       at.AddDate(0, 0, di),
			})
			idx++
		}
	}
	return out
}

// PlayRecords 返回内置播放量上报。
func PlayRecords() []model.PlayRecord {
	base := time.Date(2026, time.August, 10, 2, 0, 0, 0, time.UTC)
	out := make([]model.PlayRecord, 0, 96)
	for _, pt := range playTotals() {
		out = append(out, spread(pt.SeriesID, pt.Total, base)...)
	}
	return out
}

// SeriesIDs 返回内置剧目编号，顺序固定。
func SeriesIDs() []string {
	out := make([]string, 0, 8)
	for _, s := range Series() {
		out = append(out, s.ID)
	}
	return out
}

// Load 构造并填充台账与播放量统计表。
func Load() (*catalog.Registry, *tally.Tally, error) {
	reg := catalog.New()
	for _, s := range Series() {
		if err := reg.AddSeries(s); err != nil {
			return nil, nil, err
		}
	}
	for _, e := range Episodes() {
		if err := reg.AddEpisode(e); err != nil {
			return nil, nil, err
		}
	}
	tl := tally.NewFor(Days())
	if err := tl.AddAll(PlayRecords()); err != nil {
		return nil, nil, err
	}
	return reg, tl, nil
}

// LoadCatalog 只构造台账，不入账播放量。
func LoadCatalog() (*catalog.Registry, error) {
	reg := catalog.New()
	for _, s := range Series() {
		if err := reg.AddSeries(s); err != nil {
			return nil, err
		}
	}
	for _, e := range Episodes() {
		if err := reg.AddEpisode(e); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
