package unit_test

import (
	"context"
	"errors"
	"testing"

	investor "github.com/hyz-is/arandu-investor"
)

// withhold asks the default withholder, with its 2026 table, about one payment
// credited on a day of November 2026.
func withhold(t *testing.T, in investor.WithholdingInput) investor.Withholding {
	t.Helper()
	if in.Day == "" {
		in.Day = "2026-11-24"
	}
	out, err := investor.DefaultWithholder().Withhold(context.Background(), in)
	if err != nil {
		t.Fatalf("withholding: %v", err)
	}
	return out
}

// The figures below are worked by hand from the Receita Federal's table of
// 2026 (Lei 15.191/2025) and the reduction of Lei 15.270/2025, in centavos.
func TestTheIncomeTaxOfOnePaymentFollowsTheMonthlyTableAndTheReduction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                       string
		gross                      int64
		dependents                 int
		inss, irrf, base, reducing int64
	}{
		// Up to R$ 5.000,00 the reduction zeroes the tax: with the simplified
		// discount of R$ 607,20 the base is R$ 4.392,80 and the tax R$ 312,89,
		// the very figure of the reduction.
		{"R$ 3.000, under the exemption", 300000, 0, 33000, 0, 239280, 0},
		{"R$ 5.000, the edge of the exemption", 500000, 0, 55000, 0, 439280, 31289},
		// R$ 6.000: the contribution of R$ 660,00 beats the simplified
		// discount, so the base is R$ 5.340,00 at 27,5% less R$ 908,73 =
		// R$ 559,77, reduced by 978,62 - 0,133145 x 6.000 = R$ 179,75.
		{"R$ 6.000, inside the reduction", 600000, 0, 66000, 38002, 534000, 17975},
		// The same with two dependents of R$ 189,59 each.
		{"R$ 6.000 with two dependents", 600000, 2, 66000, 27575, 496082, 17975},
		// Over R$ 7.350,00 there is no reduction, and the contribution stops
		// at the ceiling: 11% of R$ 8.475,55 is R$ 932,31.
		{"R$ 10.000, over the ceiling", 1000000, 0, 93231, 158488, 906769, 0},
	}
	for _, c := range cases {
		got := withhold(t, investor.WithholdingInput{Gross: c.gross, Dependents: c.dependents})
		if got.INSS.Amount != c.inss || got.IRRF.Amount != c.irrf || got.IRRF.Base != c.base || got.IRRF.Reduction != c.reducing {
			t.Errorf("%s: INSS %d IRRF %d base %d reduction %d; want %d %d %d %d", c.name,
				got.INSS.Amount, got.IRRF.Amount, got.IRRF.Base, got.IRRF.Reduction, c.inss, c.irrf, c.base, c.reducing)
		}
		if got.Table != "2026-01" || got.INSS.RateBps != 1100 {
			t.Errorf("%s: table %q rate %d", c.name, got.Table, got.INSS.RateBps)
		}
	}
}

func TestTheBracketsOfTheMonthlyTableEachApply(t *testing.T) {
	t.Parallel()

	// Each limit belongs to the bracket it ends, and the next centavo to the
	// following one.
	table := investor.Withholding2026
	for _, c := range []struct {
		base int64
		rate int64
	}{
		{242880, 0}, {242881, 750}, {282665, 750}, {282666, 1500}, {375105, 1500}, {375106, 2250},
		{466468, 2250}, {466469, 2750}, {90000000, 2750},
	} {
		if b := bracketOf(table, c.base); b.RateBps != c.rate {
			t.Errorf("base %d is in the %d bracket, want %d", c.base, b.RateBps, c.rate)
		}
	}
}

// bracketOf is the bracket a base falls in, read off the table the way a person
// reads it: the first whose limit holds the base.
func bracketOf(table investor.WithholdingTable, base int64) investor.IRRFBracket {
	for _, b := range table.IRRFBrackets {
		if b.Upto == 0 || base <= b.Upto {
			return b
		}
	}
	return table.IRRFBrackets[len(table.IRRFBrackets)-1]
}

// The contribution of the month stops at the ceiling counting what the month
// already held, and the tax of the month is the one a single payment of the
// whole would have paid.
func TestTheCeilingOfTheINSSCountsTheMonthsEarlierPayments(t *testing.T) {
	t.Parallel()

	first := withhold(t, investor.WithholdingInput{Gross: 800000})
	if first.INSS.Amount != 88000 || first.INSS.Base != 800000 || first.IRRF.Amount != 104927 {
		t.Fatalf("the first payment: INSS %d over %d, IRRF %d; want 88000 over 800000 and 104927", first.INSS.Amount, first.INSS.Base, first.IRRF.Amount)
	}
	second := withhold(t, investor.WithholdingInput{Gross: 200000, Accrued: investor.WithholdingAccrued{
		Gross: 800000, INSSBase: first.INSS.Base, INSS: first.INSS.Amount, IRRF: first.IRRF.Amount,
	}})
	// Only R$ 475,55 of the second is under the ceiling.
	if second.INSS.Base != 47555 || second.INSS.Amount != 5231 {
		t.Fatalf("the second payment holds %d over %d; want 5231 over 47555", second.INSS.Amount, second.INSS.Base)
	}
	whole := withhold(t, investor.WithholdingInput{Gross: 1000000})
	if first.INSS.Amount+second.INSS.Amount != whole.INSS.Amount || first.IRRF.Amount+second.IRRF.Amount != whole.IRRF.Amount {
		t.Fatalf("two payments hold %d and %d; one of the whole holds %d and %d",
			first.INSS.Amount+second.INSS.Amount, first.IRRF.Amount+second.IRRF.Amount, whole.INSS.Amount, whole.IRRF.Amount)
	}
	// Once the ceiling is reached, nothing more is held for the contribution.
	third := withhold(t, investor.WithholdingInput{Gross: 100000, Accrued: investor.WithholdingAccrued{
		Gross: 1000000, INSSBase: 847555, INSS: 93231, IRRF: whole.IRRF.Amount,
	}})
	if third.INSS.Amount != 0 || third.INSS.Base != 0 {
		t.Fatalf("a payment over the ceiling holds %d of INSS over %d", third.INSS.Amount, third.INSS.Base)
	}
	// Income tax still applies at the top rate: 27,5% of the R$ 1.000,00.
	if third.IRRF.Amount != 27500 {
		t.Fatalf("a payment over the ceiling holds %d of income tax, want 27500", third.IRRF.Amount)
	}
}

func TestNothingIsHeldFromNothingAndNeverMoreThanWasPaid(t *testing.T) {
	t.Parallel()

	zero := withhold(t, investor.WithholdingInput{Gross: 0})
	if zero.Total() != 0 || zero.Table != "2026-01" {
		t.Fatalf("a payment of nothing holds %d (table %q)", zero.Total(), zero.Table)
	}
	for _, gross := range []int64{1, 99, 1000, 10000, 123456, 3000000, 90000000} {
		got := withhold(t, investor.WithholdingInput{Gross: gross})
		if got.Total() > gross || got.INSS.Amount < 0 || got.IRRF.Amount < 0 {
			t.Errorf("a payment of %d holds %d (INSS %d, IRRF %d)", gross, got.Total(), got.INSS.Amount, got.IRRF.Amount)
		}
	}
}

func TestAWithholderWithoutATableInForceRefuses(t *testing.T) {
	t.Parallel()

	w := investor.DefaultWithholder()
	_, err := w.Withhold(context.Background(), investor.WithholdingInput{Day: "2025-12-31", Gross: 100000})
	if !errors.Is(err, investor.ErrNoWithholdingTable) {
		t.Fatalf("a day before the first table answered %v", err)
	}
	// A table stays in force until a later one replaces it.
	if _, err := w.Withhold(context.Background(), investor.WithholdingInput{Day: "2031-06-01", Gross: 100000}); err != nil {
		t.Fatalf("a later year with no new table: %v", err)
	}
}

func TestTablesAreKeptByTheDayTheyTakeEffectAndRefusedWhenTheyCannotApply(t *testing.T) {
	t.Parallel()

	next := investor.Withholding2026
	next.Name, next.From, next.INSSRateBps = "2027-01", "2027-01-01", 1000
	w, err := investor.NewBrazilWithholder(next, investor.Withholding2026)
	if err != nil {
		t.Fatal(err)
	}
	in := investor.WithholdingInput{Day: "2026-12-31", Gross: 100000}
	if out, _ := w.Withhold(context.Background(), in); out.Table != "2026-01" || out.INSS.Amount != 11000 {
		t.Fatalf("the last day of 2026 used %q and held %d", out.Table, out.INSS.Amount)
	}
	in.Day = "2027-01-01"
	if out, _ := w.Withhold(context.Background(), in); out.Table != "2027-01" || out.INSS.Amount != 10000 {
		t.Fatalf("the first day of 2027 used %q and held %d", out.Table, out.INSS.Amount)
	}

	bad := investor.Withholding2026
	bad.IRRFBrackets = nil
	for name, tables := range map[string][]investor.WithholdingTable{
		"none":             nil,
		"no brackets":      {bad},
		"the same day":     {investor.Withholding2026, investor.Withholding2026},
		"no name":          {{From: "2026-01-01", IRRFBrackets: investor.Withholding2026.IRRFBrackets}},
		"a limit on last":  {withLastLimit()},
		"brackets falling": {withFallingBrackets()},
	} {
		if _, err := investor.NewBrazilWithholder(tables...); err == nil {
			t.Errorf("%s: the tables were accepted", name)
		}
	}
}

func withLastLimit() investor.WithholdingTable {
	t := investor.Withholding2026
	t.IRRFBrackets = []investor.IRRFBracket{{Upto: 100, RateBps: 0}, {Upto: 200, RateBps: 100}}
	return t
}

func withFallingBrackets() investor.WithholdingTable {
	t := investor.Withholding2026
	t.IRRFBrackets = []investor.IRRFBracket{{Upto: 200, RateBps: 0}, {Upto: 100, RateBps: 100}, {RateBps: 200}}
	return t
}

func TestTheNextPayDayIsTheFirstOneOnOrAfterTheDay(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		payDay int
		from   investor.Day
		want   investor.Day
	}{
		{10, "2026-11-24", "2026-12-10"},
		{24, "2026-11-24", "2026-11-24"},
		{25, "2026-11-24", "2026-11-25"},
		{5, "2026-12-20", "2027-01-05"},
		{28, "2027-02-01", "2027-02-28"},
		{0, "2026-11-24", ""},
		{29, "2026-11-24", ""},
		{10, "", ""},
	} {
		if got := investor.NextPayDay(c.payDay, c.from); got != c.want {
			t.Errorf("pay day %d from %q: %q, want %q", c.payDay, c.from, got, c.want)
		}
	}
}
