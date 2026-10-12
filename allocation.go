package investor

import (
	"sort"
)

// The calculation of a closing, kept pure: no database, no clock, no Grant.
// Everything it needs arrives as values, so every rule the owner set is a
// function a test can call with the numbers of the example it came from.
//
// The rules, as decided:
//
//   - every declared cost is shared out over the revenue of the period: the
//     cost ratio is costs / gross, and every line of the statement carries
//     that share of its own gross;
//   - a cost of some products is borne by the revenue of those products, and a
//     contract over some products bears the part of each cost its products'
//     revenue is of the revenue that cost is shared over;
//   - a cost is counted by billing month (from the billing day to the same day
//     of the next month): whole when it ran the whole month, by days when it
//     started, ended or the period cut the month;
//   - the investor's amount is the net times their share, and nothing when the
//     net is zero or negative -- with no loss carried into the next period.

// billingMonthStart is the first day of the billing month the day falls in.
func billingMonthStart(day Day, billingDay int) Day {
	t := day.Time()
	start := Day(t.AddDate(0, 0, billingDay-t.Day()).Format(DayLayout))
	if t.Day() < billingDay {
		start = Day(start.Time().AddDate(0, -1, 0).Format(DayLayout))
	}
	return start
}

// overlapDays is how many days [a1,a2) and [b1,b2) share. An empty end means
// no end.
func overlapDays(a1, a2, b1, b2 Day) int {
	start := a1
	if b1 > start {
		start = b1
	}
	end := a2
	if end.IsZero() || (!b2.IsZero() && b2 < end) {
		end = b2
	}
	if end.IsZero() {
		return 0
	}
	if n := start.DaysUntil(end); n > 0 {
		return n
	}
	return 0
}

// roundDiv divides rounding half away from zero, in integers.
func roundDiv(num, den int64) int64 {
	if den == 0 {
		return 0
	}
	if den < 0 {
		num, den = -num, -den
	}
	if num >= 0 {
		return (num + den/2) / den
	}
	return -((-num + den/2) / den)
}

// CostInWindow is how much of one cost falls in the window [start, end), for a
// contract that closes on billingDay.
func CostInWindow(item CostItem, start, end Day, billingDay int) int64 {
	if item.Amount == 0 || !start.Before(end) {
		return 0
	}
	// The days the cost runs: [StartsOn, EndsOn], so the day after EndsOn is
	// the exclusive end.
	runEnd := Day("")
	if !item.EndsOn.IsZero() {
		runEnd = item.EndsOn.AddDays(1)
	}
	switch item.Recurrence {
	case Once:
		if !item.StartsOn.Before(start) && item.StartsOn.Before(end) {
			return item.Amount
		}
		return 0
	case Monthly, Yearly:
	default:
		return 0
	}

	var total int64
	for month := billingMonthStart(start, billingDay); month.Before(end); {
		next := Day(month.Time().AddDate(0, 1, 0).Format(DayLayout))
		days := month.DaysUntil(next)
		// The days of this billing month that are in the window and in which
		// the cost ran.
		inWindow := overlapDays(month, next, start, end)
		if inWindow > 0 {
			windowStart := month
			if start > windowStart {
				windowStart = start
			}
			windowEnd := next
			if end < windowEnd {
				windowEnd = end
			}
			running := overlapDays(windowStart, windowEnd, item.StartsOn, runEnd)
			if running > 0 {
				per := int64(days)
				if item.Recurrence == Yearly {
					per *= 12
				}
				total += roundDiv(item.Amount*int64(running), per)
			}
		}
		month = next
	}
	return total
}

// Scope is a set of products, or the whole application.
type Scope struct {
	All      bool
	Products map[string]bool
}

// covers reports whether the product is in the scope.
func (s Scope) covers(product string) bool { return s.All || s.Products[product] }

// within reports whether every product of s is in other.
func (s Scope) within(other Scope) bool {
	if other.All {
		return true
	}
	if s.All {
		return false
	}
	for p := range s.Products {
		if !other.Products[p] {
			return false
		}
	}
	return true
}

// ScopedCost is a cost's amount in a window and the products that bear it.
type ScopedCost struct {
	Amount int64
	Scope  Scope
}

// AllocateCosts is the part of the costs a contract bears.
//
// revenue is the gross of the window by product, over the whole application
// -- not only the contract's products, because a cost of the whole
// application is shared over all of its revenue. A cost whose products earned
// nothing in the window is borne whole by a contract that covers all of them,
// and by nobody else: it has no revenue to be shared over.
func AllocateCosts(costs []ScopedCost, revenue map[string]int64, contract Scope) int64 {
	var allocated int64
	for _, c := range costs {
		if c.Amount == 0 {
			continue
		}
		var bearing, borne int64
		for product, gross := range revenue {
			if gross <= 0 || !c.Scope.covers(product) {
				continue
			}
			bearing += gross
			if contract.covers(product) {
				borne += gross
			}
		}
		if bearing > 0 {
			allocated += roundDiv(c.Amount*borne, bearing)
			continue
		}
		if c.Scope.within(contract) {
			allocated += c.Amount
		}
	}
	return allocated
}

// Figures are the numbers one closing of one contract comes to.
type Figures struct {
	Gross        int64
	Fees         int64
	Costs        int64
	Net          int64
	CostRatioPPM int64
	ShareBps     int64
	Amount       int64
}

// ComputeFigures turns the contract's gross, fees and allocated costs into the
// net and the investor's amount. A net of zero or less gives no amount.
func ComputeFigures(gross, fees, costs, shareBps int64) Figures {
	f := Figures{Gross: gross, Fees: fees, Costs: costs, ShareBps: shareBps}
	f.Net = gross - fees - costs
	if gross > 0 {
		f.CostRatioPPM = roundDiv(costs*1_000_000, gross)
	}
	if f.Net > 0 {
		// Floor, never round up: the centavo the division leaves stays with the
		// application, so the sum of what is paid out never exceeds the net.
		f.Amount = f.Net * shareBps / 10000
	}
	return f
}

// StatementLine is one entry as the investor's statement shows it: its own
// gross and fee, its share of the period's costs, and what is left.
type StatementLine struct {
	Entry Entry
	Cost  int64
	Net   int64
}

// SpreadCosts gives each entry its share of the costs, in proportion to its
// gross, and makes the shares add up to exactly the total: the centavos the
// rounding leaves go, one each, to the lines with the largest remainders.
// Entries with no positive gross carry no cost.
func SpreadCosts(entries []Entry, costs int64) []StatementLine {
	lines := make([]StatementLine, len(entries))
	var gross int64
	for i, e := range entries {
		lines[i] = StatementLine{Entry: e}
		if e.Gross > 0 {
			gross += e.Gross
		}
	}
	if gross > 0 && costs != 0 {
		type rest struct {
			i   int
			rem int64
		}
		var given int64
		var rests []rest
		for i, e := range entries {
			if e.Gross <= 0 {
				continue
			}
			share := costs * e.Gross / gross
			lines[i].Cost = share
			given += share
			rests = append(rests, rest{i, (costs * e.Gross) % gross})
		}
		sort.SliceStable(rests, func(a, b int) bool { return rests[a].rem > rests[b].rem })
		for k := 0; given < costs && k < len(rests); k++ {
			lines[rests[k].i].Cost++
			given++
		}
	}
	for i := range lines {
		lines[i].Net = lines[i].Entry.Gross - lines[i].Entry.Fee - lines[i].Cost
	}
	return lines
}
