package settle

import (
	"sort"
	"sync"
)

// Meter 统计结算过程中的调用与金额。所有方法可并发调用。
type Meter struct {
	mu sync.Mutex
	// calls 是累计入账次数。
	calls int64
	// amountFen 是累计入账金额，单位分。
	amountFen int64
	// byParty 是各参与方累计入账金额，单位分。
	byParty map[string]int64
	// bySeries 是各剧目累计入账次数。
	bySeries map[string]int64
}

// NewMeter 构造一个空计量器。
func NewMeter() *Meter {
	return &Meter{
		byParty:  make(map[string]int64),
		bySeries: make(map[string]int64),
	}
}

// Bump 入账一次分账明细。
func (m *Meter) Bump(seriesID, partyID string, amountFen int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.amountFen += amountFen
	m.byParty[partyID] += amountFen
	m.bySeries[seriesID]++
}

// BumpStatement 入账一张结算单的全部明细。
func (m *Meter) BumpStatement(st Statement) {
	for _, s := range st.Shares {
		m.Bump(st.SeriesID, s.PartyID, s.AmountFen)
	}
}

// Calls 返回累计入账次数。
func (m *Meter) Calls() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// AmountFen 返回累计入账金额，单位分。
func (m *Meter) AmountFen() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.amountFen
}

// PartyAmountFen 返回某参与方累计入账金额，单位分。
func (m *Meter) PartyAmountFen(partyID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byParty[partyID]
}

// SeriesCalls 返回某剧目累计入账次数。
func (m *Meter) SeriesCalls(seriesID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bySeries[seriesID]
}

// Parties 返回已入账的参与方编号，按字典序排列。
func (m *Meter) Parties() []string {
	m.mu.Lock()
	out := make([]string, 0, len(m.byParty))
	for id := range m.byParty {
		out = append(out, id)
	}
	m.mu.Unlock()
	sort.Strings(out)
	return out
}

// MeterSnapshot 是计量器的一次快照。
type MeterSnapshot struct {
	Calls     int64            `json:"calls"`
	AmountFen int64            `json:"amount_fen"`
	ByParty   map[string]int64 `json:"by_party"`
	BySeries  map[string]int64 `json:"by_series"`
	// PartyAmountFen 是各参与方入账金额之和，用于与 AmountFen 交叉核对。
	PartyAmountFen int64 `json:"party_amount_fen"`
	// SeriesCalls 是各剧目入账次数之和，用于与 Calls 交叉核对。
	SeriesCalls int64 `json:"series_calls"`
}

// Consistent 报告快照的交叉核对是否成立。
func (s MeterSnapshot) Consistent() bool {
	return s.Calls == s.SeriesCalls && s.AmountFen == s.PartyAmountFen
}

// Snapshot 导出计量器快照。
func (m *Meter) Snapshot() MeterSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := MeterSnapshot{
		Calls:     m.calls,
		AmountFen: m.amountFen,
		ByParty:   make(map[string]int64, len(m.byParty)),
		BySeries:  make(map[string]int64, len(m.bySeries)),
	}
	for id, v := range m.byParty {
		snap.ByParty[id] = v
		snap.PartyAmountFen += v
	}
	for id, v := range m.bySeries {
		snap.BySeries[id] = v
		snap.SeriesCalls += v
	}
	return snap
}
