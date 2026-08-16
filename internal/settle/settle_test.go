package settle_test

import (
	"errors"
	"testing"
	"time"

	"microdrama/internal/model"
	"microdrama/internal/seed"
	"microdrama/internal/settle"
	"microdrama/internal/tally"
)

func parties(bps ...int) []model.Party {
	out := make([]model.Party, 0, len(bps))
	for i, bp := range bps {
		out = append(out, model.Party{
			ID:      string(rune('A' + i)),
			Name:    "参与方",
			Role:    "producer",
			ShareBP: bp,
		})
	}
	return out
}

// TestSharesSumToTotal 覆盖分账明细金额之和与待分账总额的一致性。
func TestSharesSumToTotal(t *testing.T) {
	cases := []struct {
		total int64
		bps   []int
	}{
		{1524719, []int{4500, 3000, 1500, 1000}},
		{1149291, []int{3333, 3333, 3334}},
		{1789286, []int{5000, 2500, 1500, 1000}},
		{881806, []int{6000, 2500, 1500}},
		{1344942, []int{4000, 3500, 2500}},
	}
	for _, c := range cases {
		shares, err := settle.Split(c.total, parties(c.bps...))
		if err != nil {
			t.Fatalf("拆分 %d 分失败: %v", c.total, err)
		}
		var sum int64
		for _, s := range shares {
			sum += s.AmountFen
		}
		if sum != c.total {
			t.Fatalf("待分账 %d 分, 各方合计 %d 分, 偏差 %d 分", c.total, sum, sum-c.total)
		}
	}
}

// TestSharesNoRoundingLeak 覆盖比例无法整除时的尾差处理。
func TestSharesNoRoundingLeak(t *testing.T) {
	for total := int64(1); total <= 3000; total++ {
		for _, bps := range [][]int{
			{3333, 3333, 3334},
			{4500, 3000, 1500, 1000},
			{5000, 2500, 1500, 1000},
			{1, 9999},
			{2500, 2500, 2500, 2500},
		} {
			shares, err := settle.Split(total, parties(bps...))
			if err != nil {
				t.Fatalf("拆分 %d 分失败: %v", total, err)
			}
			var sum int64
			for _, s := range shares {
				sum += s.AmountFen
				if s.AmountFen < 0 {
					t.Fatalf("待分账 %d 分时出现负数分账 %d", total, s.AmountFen)
				}
			}
			if sum != total {
				t.Fatalf("比例 %v 拆分 %d 分, 各方合计 %d 分, 偏差 %d 分",
					bps, total, sum, sum-total)
			}
		}
	}
}

func TestSplitOrdersSharesByPartyID(t *testing.T) {
	ps := []model.Party{
		{ID: "P-C", ShareBP: 2000},
		{ID: "P-A", ShareBP: 5000},
		{ID: "P-B", ShareBP: 3000},
	}
	shares, err := settle.Split(100000, ps)
	if err != nil {
		t.Fatalf("拆分失败: %v", err)
	}
	want := []string{"P-A", "P-B", "P-C"}
	for i, id := range want {
		if shares[i].PartyID != id {
			t.Fatalf("第 %d 项参与方 = %s, 期望 %s", i, shares[i].PartyID, id)
		}
	}
}

func TestSplitRejectsBadInput(t *testing.T) {
	if _, err := settle.Split(-1, parties(10000)); !errors.Is(err, model.ErrSettleMismatch) {
		t.Fatalf("负数总额应返回 ErrSettleMismatch, 实际 %v", err)
	}
	if _, err := settle.Split(100, nil); !errors.Is(err, model.ErrInvalidParty) {
		t.Fatalf("无参与方应返回 ErrInvalidParty, 实际 %v", err)
	}
	if _, err := settle.Split(100, parties(5000, 4000)); !errors.Is(err, model.ErrShareMismatch) {
		t.Fatalf("比例合计不足应返回 ErrShareMismatch, 实际 %v", err)
	}
	if _, err := settle.Split(100, parties(10000, 0)); !errors.Is(err, model.ErrInvalidParty) {
		t.Fatalf("零比例应返回 ErrInvalidParty, 实际 %v", err)
	}
}

func TestSplitZeroTotal(t *testing.T) {
	shares, err := settle.Split(0, parties(3333, 3333, 3334))
	if err != nil {
		t.Fatalf("零总额拆分失败: %v", err)
	}
	for _, s := range shares {
		if s.AmountFen != 0 {
			t.Fatalf("零总额下参与方 %s 分得 %d 分", s.PartyID, s.AmountFen)
		}
	}
}

func TestRevenueUsesKiloPlays(t *testing.T) {
	if got := settle.Revenue(1000, 1187); got != 1187 {
		t.Fatalf("1000 次播放收益 = %d 分, 期望 1187", got)
	}
	if got := settle.Revenue(0, 1187); got != 0 {
		t.Fatalf("零播放收益 = %d, 期望 0", got)
	}
	if got := settle.Revenue(1000, 0); got != 0 {
		t.Fatalf("零单价收益 = %d, 期望 0", got)
	}
}

func TestComputeBalancesEverySeededSeries(t *testing.T) {
	reg, tl, err := seed.Load()
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}
	statements := make([]settle.Statement, 0, 8)
	for _, s := range reg.Series() {
		st, cerr := settle.Compute(s, tl.SeriesTotal(s.ID), seed.RateFenPerKilo)
		if cerr != nil {
			t.Fatalf("剧目 %s 结算失败: %v", s.ID, cerr)
		}
		if verr := settle.Verify(st); verr != nil {
			t.Fatalf("剧目 %s 结算单不闭合: %v", s.ID, verr)
		}
		statements = append(statements, st)
	}
	book := settle.BuildBook(statements)
	if !book.Balanced() {
		t.Fatalf("整本账不闭合: 待分账 %d 分, 各方合计 %d 分, 不闭合 %d 张",
			book.TotalFen, book.AllocatedFen, book.Unbalanced)
	}
}

func TestBuildBookAggregatesByParty(t *testing.T) {
	reg, tl, err := seed.Load()
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}
	statements := make([]settle.Statement, 0, 8)
	for _, s := range reg.Series() {
		st, cerr := settle.Compute(s, tl.SeriesTotal(s.ID), seed.RateFenPerKilo)
		if cerr != nil {
			t.Fatalf("结算失败: %v", cerr)
		}
		statements = append(statements, st)
	}
	book := settle.BuildBook(statements)
	var sum int64
	for _, v := range book.ByParty {
		sum += v
	}
	if sum != book.AllocatedFen {
		t.Fatalf("按参与方汇总 %d 分, 明细合计 %d 分", sum, book.AllocatedFen)
	}
	for i := 1; i < len(book.Statements); i++ {
		if book.Statements[i-1].SeriesID > book.Statements[i].SeriesID {
			t.Fatal("结算单未按剧目编号排序")
		}
	}
}

func TestVerifyReportsMismatch(t *testing.T) {
	st := settle.Statement{SeriesID: "MD-T-001", TotalFen: 100, AllocatedFen: 101}
	err := settle.Verify(st)
	if !errors.Is(err, model.ErrSettleMismatch) {
		t.Fatalf("不闭合结算单应返回 ErrSettleMismatch, 实际 %v", err)
	}
}

func TestComputeRejectsInvalidSeries(t *testing.T) {
	bad := model.Series{ID: "MD-T-001"}
	if _, err := settle.Compute(bad, 1000, 1187); err == nil {
		t.Fatal("非法剧目应结算失败")
	}
}

func TestComputeAcrossRatesStaysBalanced(t *testing.T) {
	s := model.Series{
		ID: "MD-T-002", Title: "费率遍历", Genre: model.GenreComedy,
		Studio: "测试厂牌", Episodes: 6, Rating: model.RatingAll,
		Stage: model.StageSubmitted, FiledAt: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		Parties: []model.Party{
			{ID: "P-1", ShareBP: 3333}, {ID: "P-2", ShareBP: 3333}, {ID: "P-3", ShareBP: 3334},
		},
	}
	tl := tally.New()
	tl.Record("2026-08-10", "daily", 1)
	for rate := int64(1); rate <= 400; rate++ {
		for plays := 997; plays <= 1013; plays++ {
			st, err := settle.Compute(s, plays, rate)
			if err != nil {
				t.Fatalf("结算失败: %v", err)
			}
			if !st.Balanced() {
				t.Fatalf("单价 %d、播放 %d 时结算单不闭合: 待分账 %d 分, 各方合计 %d 分",
					rate, plays, st.TotalFen, st.AllocatedFen)
			}
		}
	}
}

func TestYuanFormatting(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 105: "1.05", 152472: "1524.72", -105: "-1.05"}
	for fen, want := range cases {
		if got := settle.Yuan(fen); got != want {
			t.Fatalf("Yuan(%d) = %s, 期望 %s", fen, got, want)
		}
	}
}
