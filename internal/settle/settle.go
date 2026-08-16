// Package settle 依据播放量与分账比例拆分收益，并校验分账结果的一致性。
package settle

import (
	"fmt"
	"sort"

	"microdrama/internal/model"
)

// Share 是一个参与方的分账明细。
type Share struct {
	PartyID string `json:"party_id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	ShareBP int    `json:"share_bp"`
	// AmountFen 是分得金额，单位分。
	AmountFen int64 `json:"amount_fen"`
}

// Statement 是一部剧目的分账结算单。
type Statement struct {
	SeriesID string `json:"series_id"`
	// Plays 是计入结算的播放量。
	Plays int `json:"plays"`
	// RateFenPerKilo 是每千次播放的结算单价，单位分。
	RateFenPerKilo int64 `json:"rate_fen_per_kilo"`
	// TotalFen 是待分账总额，单位分。
	TotalFen int64   `json:"total_fen"`
	Shares   []Share `json:"shares"`
	// AllocatedFen 是各方分得金额之和，单位分。
	AllocatedFen int64 `json:"allocated_fen"`
}

// Balanced 报告结算单是否闭合：各方分得金额之和等于待分账总额。
func (s Statement) Balanced() bool {
	return s.AllocatedFen == s.TotalFen
}

// DiffFen 返回各方分得金额之和相对待分账总额的偏差，单位分。
func (s Statement) DiffFen() int64 {
	return s.AllocatedFen - s.TotalFen
}

// Revenue 依据播放量与每千次播放单价计算待分账总额，单位分。
func Revenue(plays int, rateFenPerKilo int64) int64 {
	if plays <= 0 || rateFenPerKilo <= 0 {
		return 0
	}
	return int64(plays) * rateFenPerKilo / 1000
}

// orderParties 按参与方编号排序，保证分账顺序稳定。
func orderParties(parties []model.Party) []model.Party {
	out := make([]model.Party, len(parties))
	copy(out, parties)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Split 把待分账总额按各方比例拆分。
//
// 分账明细按参与方编号排序输出，各方分得金额非负，
// 且各方分得金额之和必须精确等于待分账总额。
func Split(totalFen int64, parties []model.Party) ([]Share, error) {
	if totalFen < 0 {
		return nil, fmt.Errorf("%w: 待分账总额不能为负", model.ErrSettleMismatch)
	}
	if len(parties) == 0 {
		return nil, fmt.Errorf("%w: 缺少分账参与方", model.ErrInvalidParty)
	}
	sum := 0
	for _, p := range parties {
		if p.ShareBP <= 0 {
			return nil, fmt.Errorf("%w: 参与方 %s 分账比例必须为正", model.ErrInvalidParty, p.ID)
		}
		sum += p.ShareBP
	}
	if sum != 10000 {
		return nil, fmt.Errorf("%w: 合计 %d 个基点, 期望 10000", model.ErrShareMismatch, sum)
	}

	ordered := orderParties(parties)
	out := make([]Share, 0, len(ordered))
	var allocated int64
	for i, p := range ordered {
		var amount int64
		if i == len(ordered)-1 {
			amount = totalFen - allocated
		} else {
			amount = totalFen * int64(p.ShareBP) / 10000
			allocated += amount
		}
		out = append(out, Share{
			PartyID:   p.ID,
			Name:      p.Name,
			Role:      p.Role,
			ShareBP:   p.ShareBP,
			AmountFen: amount,
		})
	}
	return out, nil
}

// Compute 生成一部剧目的分账结算单。
func Compute(series model.Series, plays int, rateFenPerKilo int64) (Statement, error) {
	if err := series.Validate(); err != nil {
		return Statement{}, err
	}
	total := Revenue(plays, rateFenPerKilo)
	shares, err := Split(total, series.Parties)
	if err != nil {
		return Statement{}, err
	}
	st := Statement{
		SeriesID:       series.ID,
		Plays:          plays,
		RateFenPerKilo: rateFenPerKilo,
		TotalFen:       total,
		Shares:         shares,
	}
	for _, s := range shares {
		st.AllocatedFen += s.AmountFen
	}
	return st, nil
}

// Verify 校验一张结算单是否闭合。
func Verify(st Statement) error {
	if !st.Balanced() {
		return fmt.Errorf("%w: 剧目 %s 待分账 %d 分, 各方合计 %d 分, 偏差 %d 分",
			model.ErrSettleMismatch, st.SeriesID, st.TotalFen, st.AllocatedFen, st.DiffFen())
	}
	return nil
}

// Book 是一批结算单的汇总。
type Book struct {
	Statements []Statement `json:"statements"`
	// TotalFen 是全部剧目待分账总额之和，单位分。
	TotalFen int64 `json:"total_fen"`
	// AllocatedFen 是全部分账明细金额之和，单位分。
	AllocatedFen int64 `json:"allocated_fen"`
	// Unbalanced 是不闭合的结算单数量。
	Unbalanced int `json:"unbalanced"`
	// ByParty 是各参与方跨剧目的分得金额合计，单位分。
	ByParty map[string]int64 `json:"by_party"`
}

// Balanced 报告整本账是否闭合。
func (b Book) Balanced() bool {
	return b.Unbalanced == 0 && b.AllocatedFen == b.TotalFen
}

// BuildBook 汇总一批结算单。
func BuildBook(statements []Statement) Book {
	b := Book{ByParty: map[string]int64{}}
	b.Statements = make([]Statement, len(statements))
	copy(b.Statements, statements)
	sort.SliceStable(b.Statements, func(i, j int) bool {
		return b.Statements[i].SeriesID < b.Statements[j].SeriesID
	})
	for _, st := range b.Statements {
		b.TotalFen += st.TotalFen
		b.AllocatedFen += st.AllocatedFen
		if !st.Balanced() {
			b.Unbalanced++
		}
		for _, s := range st.Shares {
			b.ByParty[s.PartyID] += s.AmountFen
		}
	}
	return b
}

// Yuan 把以分为单位的金额格式化为元。
func Yuan(fen int64) string {
	neg := ""
	if fen < 0 {
		neg = "-"
		fen = -fen
	}
	return fmt.Sprintf("%s%d.%02d", neg, fen/100, fen%100)
}
