package query

import "sync"

// budget is the byte budget shared by the queries of one request (FR-016 AC9,
// NFR-PERF-003 AC2, B-062). Results are retained until the response is written, so
// without it a request of MaxQueries queries could hold MaxQueries × queryMaxBytes.
//
// Each query reserves a share before it runs and settles the reservation when it ends,
// keeping what its frames use and handing back the rest. The sum of open reservations
// and settled usage never exceeds the total, as long as sources honour Limits.MaxBytes.
type budget struct {
	mu         sync.Mutex
	total      int64
	remaining  int64 // may go negative if a source overshoots its share
	inFlight   int
	notStarted int
}

func newBudget(total int64, queries int) *budget {
	return &budget{total: total, remaining: total, notStarted: queries}
}

// reserve returns the byte share for a query that is about to run: what is left divided
// by the queries not yet started, counting this one. Queries that start together get
// equal shares, and what a finished query hands back goes to the queries after it. It
// returns 0 when nothing is left; the caller must not run the query then (0 means
// "unlimited" to frame.Limits).
func (b *budget) reserve() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := max(b.notStarted, 1)
	b.notStarted--
	b.inFlight++
	if b.remaining <= 0 {
		return 0
	}
	grant := b.remaining / int64(k)
	if grant == 0 {
		grant = b.remaining
	}
	b.remaining -= grant
	return grant
}

// settle ends a reservation: the query keeps used bytes and returns grant-used.
func (b *budget) settle(grant, used int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight--
	b.remaining += grant - used
}
