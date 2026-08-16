// Package model 定义微短剧内容审核与分账结算平台的领域模型。
//
// 平台覆盖剧目与剧集登记、内容审核流转、分级标签、播放量统计与分账结算。
package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Genre 表示题材类型。
type Genre string

const (
	// GenreUrban 都市情感。
	GenreUrban Genre = "urban"
	// GenreCostume 古装。
	GenreCostume Genre = "costume"
	// GenreSuspense 悬疑。
	GenreSuspense Genre = "suspense"
	// GenreComedy 喜剧。
	GenreComedy Genre = "comedy"
	// GenreRealism 现实题材。
	GenreRealism Genre = "realism"
)

// AllGenres 返回全部题材类型。
func AllGenres() []Genre {
	return []Genre{GenreUrban, GenreCostume, GenreSuspense, GenreComedy, GenreRealism}
}

// DisplayName 返回题材中文名。
func (g Genre) DisplayName() string {
	switch g {
	case GenreUrban:
		return "都市情感"
	case GenreCostume:
		return "古装"
	case GenreSuspense:
		return "悬疑"
	case GenreComedy:
		return "喜剧"
	case GenreRealism:
		return "现实题材"
	default:
		return string(g)
	}
}

// ParseGenre 解析题材代码。
func ParseGenre(s string) (Genre, error) {
	v := Genre(strings.ToLower(strings.TrimSpace(s)))
	for _, g := range AllGenres() {
		if v == g {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownGenre, s)
}

// Rating 表示内容分级。
type Rating string

const (
	// RatingAll 全龄可看。
	RatingAll Rating = "all"
	// RatingTeen 13 岁以上。
	RatingTeen Rating = "teen"
	// RatingAdult 18 岁以上。
	RatingAdult Rating = "adult"
	// RatingBlocked 不予播出。
	RatingBlocked Rating = "blocked"
)

// AllRatings 返回全部分级。
func AllRatings() []Rating {
	return []Rating{RatingAll, RatingTeen, RatingAdult, RatingBlocked}
}

// DisplayName 返回分级中文名。
func (r Rating) DisplayName() string {
	switch r {
	case RatingAll:
		return "全龄可看"
	case RatingTeen:
		return "13 岁以上"
	case RatingAdult:
		return "18 岁以上"
	case RatingBlocked:
		return "不予播出"
	default:
		return string(r)
	}
}

// Publishable 报告该分级是否允许播出。
func (r Rating) Publishable() bool {
	return r != RatingBlocked
}

// ParseRating 解析分级代码。
func ParseRating(s string) (Rating, error) {
	v := Rating(strings.ToLower(strings.TrimSpace(s)))
	for _, x := range AllRatings() {
		if v == x {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownRating, s)
}

// Stage 表示审核阶段。
type Stage string

const (
	// StageSubmitted 已提交。
	StageSubmitted Stage = "submitted"
	// StageMachine 机器初审。
	StageMachine Stage = "machine"
	// StageHuman 人工复审。
	StageHuman Stage = "human"
	// StageApproved 审核通过。
	StageApproved Stage = "approved"
	// StageRejected 审核驳回。
	StageRejected Stage = "rejected"
	// StageWithdrawn 已撤回。
	StageWithdrawn Stage = "withdrawn"
)

// AllStages 返回全部审核阶段。
func AllStages() []Stage {
	return []Stage{StageSubmitted, StageMachine, StageHuman, StageApproved, StageRejected, StageWithdrawn}
}

// DisplayName 返回阶段中文名。
func (s Stage) DisplayName() string {
	switch s {
	case StageSubmitted:
		return "已提交"
	case StageMachine:
		return "机器初审"
	case StageHuman:
		return "人工复审"
	case StageApproved:
		return "审核通过"
	case StageRejected:
		return "审核驳回"
	case StageWithdrawn:
		return "已撤回"
	default:
		return string(s)
	}
}

// Terminal 报告该阶段是否为终态。
func (s Stage) Terminal() bool {
	return s == StageApproved || s == StageRejected || s == StageWithdrawn
}

// ParseStage 解析阶段代码。
func ParseStage(s string) (Stage, error) {
	v := Stage(strings.ToLower(strings.TrimSpace(s)))
	for _, x := range AllStages() {
		if v == x {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownStage, s)
}

// Party 表示一个分账参与方。
type Party struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	// ShareBP 是分账比例，单位万分之一（basis point）。
	ShareBP int `json:"share_bp"`
}

// Series 表示一部剧目。
type Series struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Genre    Genre     `json:"genre"`
	Studio   string    `json:"studio"`
	Episodes int       `json:"episodes"`
	Rating   Rating    `json:"rating"`
	Stage    Stage     `json:"stage"`
	FiledAt  time.Time `json:"filed_at"`
	// Parties 是分账参与方，比例合计应为 10000 个基点。
	Parties []Party `json:"parties"`
}

// Validate 校验剧目登记信息。
func (s Series) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: 剧目编号为空", ErrInvalidSeries)
	}
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf("%w: 剧目 %s 缺少剧名", ErrInvalidSeries, s.ID)
	}
	if _, err := ParseGenre(string(s.Genre)); err != nil {
		return fmt.Errorf("%w: 剧目 %s 题材非法", ErrInvalidSeries, s.ID)
	}
	if _, err := ParseRating(string(s.Rating)); err != nil {
		return fmt.Errorf("%w: 剧目 %s 分级非法", ErrInvalidSeries, s.ID)
	}
	if s.Episodes <= 0 {
		return fmt.Errorf("%w: 剧目 %s 集数必须为正", ErrInvalidSeries, s.ID)
	}
	if s.FiledAt.IsZero() {
		return fmt.Errorf("%w: 剧目 %s 缺少备案日期", ErrInvalidSeries, s.ID)
	}
	if len(s.Parties) == 0 {
		return fmt.Errorf("%w: 剧目 %s 缺少分账参与方", ErrInvalidSeries, s.ID)
	}
	total := 0
	for _, p := range s.Parties {
		if strings.TrimSpace(p.ID) == "" {
			return fmt.Errorf("%w: 剧目 %s 存在无编号的参与方", ErrInvalidSeries, s.ID)
		}
		if p.ShareBP <= 0 {
			return fmt.Errorf("%w: 剧目 %s 参与方 %s 分账比例必须为正", ErrInvalidSeries, s.ID, p.ID)
		}
		total += p.ShareBP
	}
	if total != 10000 {
		return fmt.Errorf("%w: 剧目 %s 分账比例合计 %d 个基点, 期望 10000",
			ErrInvalidSeries, s.ID, total)
	}
	return nil
}

// Episode 表示一集内容。
type Episode struct {
	SeriesID string `json:"series_id"`
	No       int    `json:"no"`
	Title    string `json:"title"`
	// DurationSec 是时长，单位秒。
	DurationSec int `json:"duration_sec"`
	// Tags 是内容标签，用于分级与统计。
	Tags []string `json:"tags"`
}

// Validate 校验剧集信息。
func (e Episode) Validate() error {
	if strings.TrimSpace(e.SeriesID) == "" {
		return fmt.Errorf("%w: 缺少剧目编号", ErrInvalidEpisode)
	}
	if e.No <= 0 {
		return fmt.Errorf("%w: 剧目 %s 集号必须为正", ErrInvalidEpisode, e.SeriesID)
	}
	if e.DurationSec <= 0 {
		return fmt.Errorf("%w: 剧目 %s 第 %d 集时长必须为正", ErrInvalidEpisode, e.SeriesID, e.No)
	}
	return nil
}

// PlayRecord 表示一条播放量上报。
type PlayRecord struct {
	SeriesID string    `json:"series_id"`
	Day      string    `json:"day"`
	Tag      string    `json:"tag"`
	Plays    int       `json:"plays"`
	At       time.Time `json:"at"`
}

// SortSeries 按备案日期与编号排序，保证输出稳定。
func SortSeries(items []Series) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].FiledAt.Equal(items[j].FiledAt) {
			return items[i].FiledAt.Before(items[j].FiledAt)
		}
		return items[i].ID < items[j].ID
	})
}

// RatingFor 依据内容标签判定分级。
func RatingFor(tags []string) Rating {
	has := func(t string) bool {
		for _, x := range tags {
			if x == t {
				return true
			}
		}
		return false
	}
	switch {
	case has("violence-graphic") || has("illegal-content"):
		return RatingBlocked
	case has("violence-mild") || has("substance"):
		return RatingAdult
	case has("romance") || has("conflict"):
		return RatingTeen
	default:
		return RatingAll
	}
}
