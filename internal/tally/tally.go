// Package tally 按自然日与内容标签汇总播放量上报。
package tally

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"microdrama/internal/model"
)

// Tally 是按「自然日 -> 标签 -> 播放量」组织的两级统计表。
type Tally struct {
	mu   sync.Mutex
	days map[string]map[string]int
	// series 记录每部剧目的累计播放量。
	series map[string]int
	// rows 记录已入账的上报条数。
	rows int
}

// New 构造一个空统计表，后续按需扩展。
func New() *Tally {
	return &Tally{
		days:   make(map[string]map[string]int),
		series: make(map[string]int),
	}
}

// NewFor 构造统计表并预先建立给定自然日的标签分表。
//
// 预建只是为了让报表输出包含零播放量的自然日，
// 不在预建范围内的自然日仍然可以直接入账。
func NewFor(days []string) *Tally {
	t := New()
	t.Prepare(days...)
	return t
}

// Prepare 预先建立给定自然日的标签分表。
func (t *Tally) Prepare(days ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range days {
		if _, ok := t.days[d]; !ok {
			t.days[d] = make(map[string]int)
		}
	}
}

// Record 把某一天某个标签的播放量累加进统计表。
//
// 调用方不需要预先声明自然日或标签，统计表按需扩展；
// 同一自然日与标签的重复上报按累加处理。
func (t *Tally) Record(day, tag string, plays int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byTag, ok := t.days[day]
	if !ok {
		byTag = make(map[string]int)
		t.days[day] = byTag
	}
	byTag[tag] += plays
}

// Add 入账一条播放量上报。
func (t *Tally) Add(rec model.PlayRecord) error {
	if strings.TrimSpace(rec.Day) == "" {
		return fmt.Errorf("%w: 上报缺少自然日", model.ErrTallyMissing)
	}
	if strings.TrimSpace(rec.Tag) == "" {
		return fmt.Errorf("%w: 上报缺少内容标签", model.ErrTallyMissing)
	}
	if rec.Plays < 0 {
		return fmt.Errorf("%w: 播放量不能为负", model.ErrTallyMissing)
	}
	t.Record(rec.Day, rec.Tag, rec.Plays)
	t.mu.Lock()
	t.series[rec.SeriesID] += rec.Plays
	t.rows++
	t.mu.Unlock()
	return nil
}

// AddAll 批量入账。
func (t *Tally) AddAll(records []model.PlayRecord) error {
	for _, rec := range records {
		if err := t.Add(rec); err != nil {
			return err
		}
	}
	return nil
}

// Days 返回已入账的自然日，按字典序排列。
func (t *Tally) Days() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.days))
	for day := range t.days {
		out = append(out, day)
	}
	sort.Strings(out)
	return out
}

// Tags 返回某一天出现过的标签，按字典序排列。
func (t *Tally) Tags(day string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, 8)
	for tag := range t.days[day] {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// Plays 返回某一天某个标签的播放量。
func (t *Tally) Plays(day, tag string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.days[day][tag]
}

// DayTotal 返回某一天的播放量合计。
func (t *Tally) DayTotal(day string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	total := 0
	for _, v := range t.days[day] {
		total += v
	}
	return total
}

// TagTotal 返回某个标签的跨日播放量合计。
func (t *Tally) TagTotal(tag string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	total := 0
	for _, byTag := range t.days {
		total += byTag[tag]
	}
	return total
}

// SeriesTotal 返回某部剧目的累计播放量。
func (t *Tally) SeriesTotal(seriesID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.series[seriesID]
}

// Total 返回全部播放量合计。
func (t *Tally) Total() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	total := 0
	for _, byTag := range t.days {
		for _, v := range byTag {
			total += v
		}
	}
	return total
}

// Rows 返回已入账的上报条数。
func (t *Tally) Rows() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rows
}

// Cell 是导出用的一格统计。
type Cell struct {
	Day   string `json:"day"`
	Tag   string `json:"tag"`
	Plays int    `json:"plays"`
}

// Cells 导出全部统计格，按日期与标签稳定排序。
func (t *Tally) Cells() []Cell {
	t.mu.Lock()
	rows := make([]Cell, 0, 32)
	for day, byTag := range t.days {
		for tag, plays := range byTag {
			rows = append(rows, Cell{Day: day, Tag: tag, Plays: plays})
		}
	}
	t.mu.Unlock()
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Day != rows[j].Day {
			return rows[i].Day < rows[j].Day
		}
		return rows[i].Tag < rows[j].Tag
	})
	return rows
}

// Snapshot 是统计表的一次快照。
type Snapshot struct {
	Days  int    `json:"days"`
	Tags  int    `json:"tags"`
	Rows  int    `json:"rows"`
	Total int    `json:"total_plays"`
	Cells []Cell `json:"cells"`
}

// Snapshot 导出统计表快照。
func (t *Tally) Snapshot() Snapshot {
	cells := t.Cells()
	tags := make(map[string]bool)
	total := 0
	for _, c := range cells {
		tags[c.Tag] = true
		total += c.Plays
	}
	return Snapshot{
		Days:  len(t.Days()),
		Tags:  len(tags),
		Rows:  t.Rows(),
		Total: total,
		Cells: cells,
	}
}

// TopTags 返回播放量最高的若干标签。
func (t *Tally) TopTags(n int) []Cell {
	agg := make(map[string]int)
	for _, c := range t.Cells() {
		agg[c.Tag] += c.Plays
	}
	rows := make([]Cell, 0, len(agg))
	for tag, plays := range agg {
		rows = append(rows, Cell{Tag: tag, Plays: plays})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Plays != rows[j].Plays {
			return rows[i].Plays > rows[j].Plays
		}
		return rows[i].Tag < rows[j].Tag
	})
	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}
