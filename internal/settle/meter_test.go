package settle_test

import (
	"fmt"
	"sync"
	"testing"

	"microdrama/internal/model"
	"microdrama/internal/settle"
)

// TestBumpCountsEveryCall 覆盖并发入账下的计量准确性。
func TestBumpCountsEveryCall(t *testing.T) {
	const workers, per = 64, 5000
	const amount = int64(7)

	m := settle.NewMeter()
	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			series := fmt.Sprintf("MD-T-%02d", w%5)
			party := fmt.Sprintf("P-%02d", w%4)
			for i := 0; i < per; i++ {
				m.Bump(series, party, amount)
			}
		}(w)
	}
	close(start)
	wg.Wait()

	wantCalls := int64(workers * per)
	if got := m.Calls(); got != wantCalls {
		t.Fatalf("入账次数 = %d, 期望 %d, 丢失 %d 次", got, wantCalls, wantCalls-got)
	}
	wantAmount := wantCalls * amount
	if got := m.AmountFen(); got != wantAmount {
		t.Fatalf("入账金额 = %d 分, 期望 %d 分, 丢失 %d 分", got, wantAmount, wantAmount-got)
	}
	snap := m.Snapshot()
	if !snap.Consistent() {
		t.Fatalf("交叉核对失败: 次数 %d vs 分剧目 %d, 金额 %d vs 分参与方 %d",
			snap.Calls, snap.SeriesCalls, snap.AmountFen, snap.PartyAmountFen)
	}
}

func TestBumpSequentialIsExact(t *testing.T) {
	m := settle.NewMeter()
	for i := 0; i < 1000; i++ {
		m.Bump("MD-T-001", "P-01", 3)
	}
	if got := m.Calls(); got != 1000 {
		t.Fatalf("入账次数 = %d, 期望 1000", got)
	}
	if got := m.AmountFen(); got != 3000 {
		t.Fatalf("入账金额 = %d, 期望 3000", got)
	}
	if got := m.PartyAmountFen("P-01"); got != 3000 {
		t.Fatalf("参与方入账金额 = %d, 期望 3000", got)
	}
	if got := m.SeriesCalls("MD-T-001"); got != 1000 {
		t.Fatalf("剧目入账次数 = %d, 期望 1000", got)
	}
}

func TestBumpStatementCoversEveryShare(t *testing.T) {
	ps := []model.Party{
		{ID: "P-A", ShareBP: 4500}, {ID: "P-B", ShareBP: 3000},
		{ID: "P-C", ShareBP: 1500}, {ID: "P-D", ShareBP: 1000},
	}
	shares, err := settle.Split(1524719, ps)
	if err != nil {
		t.Fatalf("拆分失败: %v", err)
	}
	st := settle.Statement{SeriesID: "MD-T-001", TotalFen: 1524719, Shares: shares}
	for _, s := range shares {
		st.AllocatedFen += s.AmountFen
	}

	m := settle.NewMeter()
	m.BumpStatement(st)
	if got := m.Calls(); got != int64(len(shares)) {
		t.Fatalf("入账次数 = %d, 期望 %d", got, len(shares))
	}
	if got := m.AmountFen(); got != st.AllocatedFen {
		t.Fatalf("入账金额 = %d, 期望 %d", got, st.AllocatedFen)
	}
	if got := len(m.Parties()); got != len(shares) {
		t.Fatalf("参与方数 = %d, 期望 %d", got, len(shares))
	}
}

func TestConcurrentBumpStatementsKeepTotals(t *testing.T) {
	ps := []model.Party{{ID: "P-A", ShareBP: 6000}, {ID: "P-B", ShareBP: 4000}}
	shares, err := settle.Split(881806, ps)
	if err != nil {
		t.Fatalf("拆分失败: %v", err)
	}
	st := settle.Statement{SeriesID: "MD-T-002", TotalFen: 881806, Shares: shares}
	for _, s := range shares {
		st.AllocatedFen += s.AmountFen
	}

	const workers = 32
	m := settle.NewMeter()
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			m.BumpStatement(st)
		}()
	}
	wg.Wait()

	if got := m.Calls(); got != int64(workers*len(shares)) {
		t.Fatalf("入账次数 = %d, 期望 %d", got, workers*len(shares))
	}
	if got := m.AmountFen(); got != int64(workers)*st.AllocatedFen {
		t.Fatalf("入账金额 = %d, 期望 %d", got, int64(workers)*st.AllocatedFen)
	}
}

func TestMeterPartiesSorted(t *testing.T) {
	m := settle.NewMeter()
	for _, id := range []string{"P-C", "P-A", "P-B"} {
		m.Bump("MD-T-001", id, 1)
	}
	got := m.Parties()
	want := []string{"P-A", "P-B", "P-C"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("参与方列表 = %v, 期望 %v", got, want)
		}
	}
}

func TestEmptyMeterSnapshotConsistent(t *testing.T) {
	if !settle.NewMeter().Snapshot().Consistent() {
		t.Fatal("空计量器快照应通过交叉核对")
	}
}
