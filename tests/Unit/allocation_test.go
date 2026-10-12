package unit_test

import (
	"testing"

	investor "github.com/hyz-is/arandu-investor"
)

// monthly is a cost of R$ 3.000,00 a month, running from a day with no end.
func monthly(starts, ends string) investor.CostItem {
	return investor.CostItem{Amount: 300000, Recurrence: investor.Monthly,
		StartsOn: investor.Day(starts), EndsOn: investor.Day(ends)}
}

func TestACostIsCountedByBillingMonth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		item       investor.CostItem
		start, end string
		want       int64
	}{
		// From the 20th to the 20th is a whole month, whatever its length.
		{"a whole billing month", monthly("2026-01-01", ""), "2026-09-20", "2026-10-20", 300000},
		{"a whole February", monthly("2026-01-01", ""), "2027-01-20", "2027-02-20", 300000},
		{"two whole months", monthly("2026-01-01", ""), "2026-08-20", "2026-10-20", 600000},
		// The first period of a contract that starts on the 1st and closes on
		// the 20th: 19 of the 30 days of that billing month.
		{"the first period, cut by the contract's start", monthly("2026-01-01", ""), "2026-10-01", "2026-10-20", 190000},
		// A cost that starts on the 5th runs 15 of the 30 days.
		{"a cost that starts mid-month", monthly("2026-10-05", ""), "2026-09-20", "2026-10-20", 150000},
		// A cost that ends on the 29th (inclusive) runs 10 of the 30 days.
		{"a cost that ends mid-month", monthly("2026-01-01", "2026-09-29"), "2026-09-20", "2026-10-20", 100000},
		{"a cost that ended before the period", monthly("2026-01-01", "2026-08-31"), "2026-09-20", "2026-10-20", 0},
		{"a yearly cost, one month of it",
			investor.CostItem{Amount: 1200000, Recurrence: investor.Yearly, StartsOn: "2026-01-01"},
			"2026-09-20", "2026-10-20", 100000},
		{"a one-off cost inside the period",
			investor.CostItem{Amount: 50000, Recurrence: investor.Once, StartsOn: "2026-10-03"},
			"2026-09-20", "2026-10-20", 50000},
		{"a one-off cost on the closing day belongs to the next period",
			investor.CostItem{Amount: 50000, Recurrence: investor.Once, StartsOn: "2026-10-20"},
			"2026-09-20", "2026-10-20", 0},
	}
	for _, c := range cases {
		if got := investor.CostInWindow(c.item, investor.Day(c.start), investor.Day(c.end), 20); got != c.want {
			t.Errorf("%s: %d centavos, want %d", c.name, got, c.want)
		}
	}
}

func scope(products ...string) investor.Scope {
	if len(products) == 0 {
		return investor.Scope{All: true}
	}
	s := investor.Scope{Products: map[string]bool{}}
	for _, p := range products {
		s.Products[p] = true
	}
	return s
}

func TestACostIsBorneByTheRevenueOfItsProducts(t *testing.T) {
	t.Parallel()

	revenue := map[string]int64{"loccrm": 2000000, "tayi": 1000000}
	whole := investor.ScopedCost{Amount: 600000, Scope: scope()}
	tayiOnly := investor.ScopedCost{Amount: 300000, Scope: scope("tayi")}

	// The common case: a contract over the whole application bears every cost.
	if got := investor.AllocateCosts([]investor.ScopedCost{whole, tayiOnly}, revenue, scope()); got != 900000 {
		t.Errorf("a contract over everything bore %d, want every cost (900000)", got)
	}
	// A contract over LocCRM bears the part of the general cost LocCRM's
	// revenue is of the whole (2/3), and none of Tayi's own cost.
	if got := investor.AllocateCosts([]investor.ScopedCost{whole, tayiOnly}, revenue, scope("loccrm")); got != 400000 {
		t.Errorf("a contract over LocCRM bore %d, want 400000", got)
	}
	// Tayi's clusters reduce nobody else's share.
	if got := investor.AllocateCosts([]investor.ScopedCost{tayiOnly}, revenue, scope("loccrm")); got != 0 {
		t.Errorf("LocCRM bore %d of Tayi's own cost", got)
	}
	// A product that earned nothing has no revenue to share its cost over: a
	// contract that covers it bears it whole, and nobody else does.
	quiet := map[string]int64{"loccrm": 2000000}
	if got := investor.AllocateCosts([]investor.ScopedCost{tayiOnly}, quiet, scope()); got != 300000 {
		t.Errorf("a contract over everything bore %d of a cost whose product earned nothing, want 300000", got)
	}
	if got := investor.AllocateCosts([]investor.ScopedCost{tayiOnly}, quiet, scope("loccrm")); got != 0 {
		t.Errorf("a contract over LocCRM bore %d of a cost of a product it does not cover", got)
	}
}

func TestTheInvestorsAmountIsTheirShareOfTheNet(t *testing.T) {
	t.Parallel()

	// The owner's example: R$ 30.000 billed, R$ 300 of gateway fees, R$ 6.000
	// of costs (20% of the revenue), and an investor with 15%.
	f := investor.ComputeFigures(3000000, 30000, 600000, 1500)
	if f.Net != 2370000 || f.Amount != 355500 || f.CostRatioPPM != 200000 {
		t.Fatalf("net %d amount %d ratio %d ppm; want 2370000, 355500 and 200000", f.Net, f.Amount, f.CostRatioPPM)
	}

	// A period that lost money gives nothing, and carries nothing.
	if f := investor.ComputeFigures(500000, 10000, 600000, 1500); f.Net != -110000 || f.Amount != 0 {
		t.Fatalf("a negative period gave net %d amount %d, want -110000 and 0", f.Net, f.Amount)
	}

	// The centavo the division leaves stays with the application.
	if f := investor.ComputeFigures(1001, 0, 0, 1500); f.Amount != 150 {
		t.Fatalf("15%% of 1001 centavos paid %d, want 150 (rounded down)", f.Amount)
	}
}

func TestEveryLineCarriesItsShareOfTheCostsAndTheyAddUp(t *testing.T) {
	t.Parallel()

	// The owner's line: a payment of R$ 50 in a period whose costs are 20% of
	// the revenue carries R$ 10 of them.
	lines := investor.SpreadCosts([]investor.Entry{
		{Gross: 5000, Fee: 50}, {Gross: 20000}, {Gross: 25000},
	}, 10000)
	if lines[0].Cost != 1000 || lines[0].Net != 3950 {
		t.Fatalf("the R$ 50 line carries %d of cost and nets %d; want 1000 and 3950", lines[0].Cost, lines[0].Net)
	}

	// Amounts that do not divide evenly still add up to the exact total.
	uneven := investor.SpreadCosts([]investor.Entry{
		{Gross: 3333}, {Gross: 3333}, {Gross: 3334}, {Gross: -500}, {Fee: 99},
	}, 1001)
	var total int64
	for _, l := range uneven {
		total += l.Cost
	}
	if total != 1001 {
		t.Fatalf("the lines carry %d of the costs, want exactly 1001", total)
	}
	if uneven[3].Cost != 0 || uneven[4].Cost != 0 {
		t.Fatalf("a refund or a pure fee carried part of the costs: %d, %d", uneven[3].Cost, uneven[4].Cost)
	}
}
