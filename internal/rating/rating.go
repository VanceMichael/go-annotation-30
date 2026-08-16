// Package rating 依据剧集内容标签判定剧目分级，并给出判定依据。
package rating

import (
	"fmt"
	"sort"
	"strings"

	"microdrama/internal/model"
)

// Weight 是各分级的严格程度权重，取值越大越严格。
func Weight(r model.Rating) int {
	switch r {
	case model.RatingAll:
		return 0
	case model.RatingTeen:
		return 1
	case model.RatingAdult:
		return 2
	case model.RatingBlocked:
		return 3
	default:
		return -1
	}
}

// Stricter 返回两个分级中更严格的一个。
func Stricter(a, b model.Rating) model.Rating {
	if Weight(a) >= Weight(b) {
		return a
	}
	return b
}

// Evidence 记录一条判定依据。
type Evidence struct {
	EpisodeNo int          `json:"episode_no"`
	Tag       string       `json:"tag"`
	Rating    model.Rating `json:"rating"`
}

// Decision 是一部剧目的分级判定结果。
type Decision struct {
	SeriesID string       `json:"series_id"`
	Rating   model.Rating `json:"rating"`
	// Publishable 报告该分级是否允许播出。
	Publishable bool       `json:"publishable"`
	Evidence    []Evidence `json:"evidence"`
	// TagCounts 是各标签出现次数。
	TagCounts map[string]int `json:"tag_counts"`
}

// TagRating 返回单个标签对应的最低分级要求。
func TagRating(tag string) model.Rating {
	return model.RatingFor([]string{tag})
}

// Assess 依据全部剧集标签判定剧目分级。
func Assess(seriesID string, episodes []model.Episode) (Decision, error) {
	if strings.TrimSpace(seriesID) == "" {
		return Decision{}, fmt.Errorf("%w: 缺少剧目编号", model.ErrInvalidSeries)
	}
	if len(episodes) == 0 {
		return Decision{}, fmt.Errorf("%w: 剧目 %s 无剧集可判定", model.ErrEpisodeUnknown, seriesID)
	}
	d := Decision{
		SeriesID:  seriesID,
		Rating:    model.RatingAll,
		TagCounts: make(map[string]int),
	}
	for _, e := range episodes {
		for _, tag := range e.Tags {
			d.TagCounts[tag]++
			tr := TagRating(tag)
			if Weight(tr) > Weight(model.RatingAll) {
				d.Evidence = append(d.Evidence, Evidence{EpisodeNo: e.No, Tag: tag, Rating: tr})
			}
			d.Rating = Stricter(d.Rating, tr)
		}
	}
	sort.SliceStable(d.Evidence, func(i, j int) bool {
		if d.Evidence[i].EpisodeNo != d.Evidence[j].EpisodeNo {
			return d.Evidence[i].EpisodeNo < d.Evidence[j].EpisodeNo
		}
		return d.Evidence[i].Tag < d.Evidence[j].Tag
	})
	d.Publishable = d.Rating.Publishable()
	return d, nil
}

// BlockingTags 返回导致不予播出的标签，按字典序排列。
func BlockingTags(episodes []model.Episode) []string {
	seen := make(map[string]bool)
	for _, e := range episodes {
		for _, tag := range e.Tags {
			if TagRating(tag) == model.RatingBlocked {
				seen[tag] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// Distribution 统计一批判定结果的分级分布。
func Distribution(decisions []Decision) map[model.Rating]int {
	out := make(map[model.Rating]int, len(model.AllRatings()))
	for _, r := range model.AllRatings() {
		out[r] = 0
	}
	for _, d := range decisions {
		out[d.Rating]++
	}
	return out
}

// Summary 汇总一批判定结果。
type Summary struct {
	Series      int            `json:"series"`
	Publishable int            `json:"publishable"`
	Blocked     int            `json:"blocked"`
	ByRating    map[string]int `json:"by_rating"`
}

// Summarise 汇总一批判定结果。
func Summarise(decisions []Decision) Summary {
	s := Summary{Series: len(decisions), ByRating: make(map[string]int)}
	for _, r := range model.AllRatings() {
		s.ByRating[string(r)] = 0
	}
	for _, d := range decisions {
		s.ByRating[string(d.Rating)]++
		if d.Publishable {
			s.Publishable++
		} else {
			s.Blocked++
		}
	}
	return s
}
