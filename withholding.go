package investor

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Withholding at source on what is credited to an investor who is a person.
//
// A partner who brings work and no money is paid as an autonomous worker
// (contribuinte individual): the company that pays holds back the social
// security contribution (INSS) and the income tax (IRRF) and pays the rest.
// The retention is a discount on the investor's part of the net. It is never
// a cost of the base, so the other investors' figures do not move by it.
//
// Everything here is pure: no database, no clock, no Grant. The service gathers
// what the month already holds and asks a Withholder; the calculation is a
// function a test can call with the numbers of an example.

// WithholdingAccrued is what the same investor was already credited, and held
// back, in the same calendar month by invoices of this package. The tax of a
// month is computed over the month, so a second payment inside it is taxed as
// part of the sum and only the difference is held back now.
type WithholdingAccrued struct {
	// Gross is the sum of the parts credited before, before any retention.
	Gross int64
	// INSSBase is the part of Gross the contribution was held over.
	INSSBase int64
	// INSS and IRRF are what was held back before.
	INSS, IRRF int64
}

// WithholdingInput is what a Withholder is asked about: one amount credited to
// one investor.
type WithholdingInput struct {
	// Day is the day the amount is credited. It picks the table in force.
	Day Day
	// Gross is the investor's part credited now, in centavos, before any
	// retention. It is the part after the loss carried from earlier periods.
	Gross int64
	// Dependents is how many dependents the investor declared, each one a
	// deduction of the income tax base.
	Dependents int
	// Accrued is what the month already holds.
	Accrued WithholdingAccrued
}

// WithholdingLine is one tax held back, with how it came out.
type WithholdingLine struct {
	// Base is what the rate was applied to, in centavos. For the income tax it
	// is the month's base after the deductions, accumulated over every payment
	// of the month; for the contribution, the part of this payment under the
	// ceiling.
	Base int64
	// RateBps is the rate in basis points: 1100 is 11%. For the income tax it
	// is the rate of the bracket the month's base fell in.
	RateBps int64
	// Reduction is the amount the income tax was reduced by under Lei
	// 15.270/2025. Zero for the contribution.
	Reduction int64
	// Amount is what is held back by this payment, in centavos, never
	// negative.
	Amount int64
}

// Withholding is what a Withholder answers: the contribution and the income
// tax of one payment.
type Withholding struct {
	// Table names the table of rates in force, so an invoice says which one it
	// was computed with.
	Table string
	INSS  WithholdingLine
	IRRF  WithholdingLine
}

// Total is everything held back.
func (w Withholding) Total() int64 { return w.INSS.Amount + w.IRRF.Amount }

// Withholder computes the retention at source of one payment to a person.
// Config.Withholder takes the application's own when its rules are not the
// ones DefaultWithholder carries. An implementation must answer amounts that
// add up to no more than the gross it was given.
type Withholder interface {
	Withhold(ctx context.Context, in WithholdingInput) (Withholding, error)
}

// ErrNoWithholdingTable is answered when no table is in force on the day a
// payment is credited.
var ErrNoWithholdingTable = errors.New("investor: no withholding table is in force on that day")

// IRRFBracket is one line of the monthly income tax table: the base up to
// Upto (inclusive; zero means no limit), the rate over the whole base, and the
// amount deducted from the product.
type IRRFBracket struct {
	Upto      int64
	RateBps   int64
	Deduction int64
}

// WithholdingTable is the set of figures a retention is computed with from the
// day From on, until a later table replaces it. All amounts are centavos.
type WithholdingTable struct {
	// Name is how the table is called on an invoice: "2026-01".
	Name string
	// From is the first day the table is in force.
	From Day

	// INSSRateBps is the contribution held from a contribuinte individual who
	// serves a company: 1100 is 11%. INSSCeiling is the contribution ceiling
	// of the month (the salary-of-contribution ceiling): what is paid above it
	// is not charged, and the month's payments count together toward it.
	INSSRateBps int64
	INSSCeiling int64

	// IRRFBrackets are the lines of the monthly table, from the smallest
	// base. The last has no limit (Upto zero).
	IRRFBrackets []IRRFBracket
	// DependentDeduction is deducted from the base for each dependent.
	DependentDeduction int64
	// SimplifiedDiscount is the monthly simplified discount, deducted in place
	// of the contribution and the dependents when it is the larger deduction.
	// Zero never applies it.
	SimplifiedDiscount int64

	// The reduction of Lei 15.270/2025, over the month's taxable income (the
	// payment before any deduction): up to ReductionUpTo of income, the tax is
	// reduced by at most ReductionFull, which zeroes it; from there to
	// ReductionEnd the reduction is ReductionBase minus ReductionFactorPPM
	// parts per million of the income; above ReductionEnd there is none. All
	// zero turns the reduction off.
	ReductionUpTo      int64
	ReductionFull      int64
	ReductionBase      int64
	ReductionFactorPPM int64
	ReductionEnd       int64
}

// Withholding2026 is the table of 2026, from the official figures:
//
//   - the contribution ceiling is the salary-of-contribution ceiling of
//     Portaria Interministerial MPS/MF nº 13, of 9 January 2026, art. 2 (R$
//     8.475,55), and the rate of the contribuinte individual who serves a
//     company is 11% of what is paid in the month, up to that ceiling, held by
//     the company (Lei 10.666/2003, art. 4; Lei 8.212/1991, art. 30);
//   - the monthly income tax table, the dependent deduction (R$ 189,59) and the
//     simplified discount (R$ 607,20) are the Receita Federal's tables of 2026
//     (Lei 15.191/2025), at
//     https://www.gov.br/receitafederal/pt-br/assuntos/meu-imposto-de-renda/tabelas/2026;
//   - the reduction is Lei 15.270/2025: tax reduced by up to R$ 312,89 on
//     income up to R$ 5.000,00, and by R$ 978,62 minus 0,133145 of the income
//     from R$ 5.000,01 to R$ 7.350,00, as the Receita Federal's page of 2026
//     states it.
//
// The figures are an accountant's to confirm before the first real payment.
var Withholding2026 = WithholdingTable{
	Name:        "2026-01",
	From:        "2026-01-01",
	INSSRateBps: 1100,
	INSSCeiling: 847555,
	IRRFBrackets: []IRRFBracket{
		{Upto: 242880, RateBps: 0, Deduction: 0},
		{Upto: 282665, RateBps: 750, Deduction: 18216},
		{Upto: 375105, RateBps: 1500, Deduction: 39416},
		{Upto: 466468, RateBps: 2250, Deduction: 67549},
		{Upto: 0, RateBps: 2750, Deduction: 90873},
	},
	DependentDeduction: 18959,
	SimplifiedDiscount: 60720,
	ReductionUpTo:      500000,
	ReductionFull:      31289,
	ReductionBase:      97862,
	ReductionFactorPPM: 133145,
	ReductionEnd:       735000,
}

// validate reports a table that could not be applied.
func (t WithholdingTable) validate() error {
	switch {
	case t.Name == "":
		return errors.New("investor: a withholding table needs a Name")
	case t.From.IsZero():
		return fmt.Errorf("investor: withholding table %s needs a From day", t.Name)
	case t.INSSRateBps < 0 || t.INSSRateBps > 10000:
		return fmt.Errorf("investor: withholding table %s has an INSS rate of %d basis points", t.Name, t.INSSRateBps)
	case t.INSSCeiling < 0:
		return fmt.Errorf("investor: withholding table %s has a negative INSS ceiling", t.Name)
	case len(t.IRRFBrackets) == 0:
		return fmt.Errorf("investor: withholding table %s has no income tax bracket", t.Name)
	}
	var previous int64
	for i, b := range t.IRRFBrackets {
		last := i == len(t.IRRFBrackets)-1
		switch {
		case b.RateBps < 0 || b.RateBps > 10000 || b.Deduction < 0:
			return fmt.Errorf("investor: withholding table %s has a bracket with a rate or a deduction out of range", t.Name)
		case last && b.Upto != 0:
			return fmt.Errorf("investor: the last bracket of withholding table %s has to have no limit", t.Name)
		case !last && b.Upto <= previous:
			return fmt.Errorf("investor: the brackets of withholding table %s have to rise", t.Name)
		}
		previous = b.Upto
	}
	return nil
}

// BrazilWithholder is the Withholder this package carries: the INSS of the
// contribuinte individual and the IRRF of the monthly table, from tables kept
// by the day they take effect.
type BrazilWithholder struct {
	tables []WithholdingTable
}

// NewBrazilWithholder builds a withholder over the tables given, which are
// sorted by their From day. A table that could not be applied, two from the
// same day, or none at all is refused.
func NewBrazilWithholder(tables ...WithholdingTable) (BrazilWithholder, error) {
	if len(tables) == 0 {
		return BrazilWithholder{}, errors.New("investor: a withholder needs at least one withholding table")
	}
	sorted := append([]WithholdingTable(nil), tables...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].From.Before(sorted[j].From) })
	for i, t := range sorted {
		if err := t.validate(); err != nil {
			return BrazilWithholder{}, err
		}
		if i > 0 && sorted[i-1].From == t.From {
			return BrazilWithholder{}, fmt.Errorf("investor: withholding tables %s and %s take effect on the same day", sorted[i-1].Name, t.Name)
		}
	}
	return BrazilWithholder{tables: sorted}, nil
}

// DefaultWithholder is the withholder an application gets by leaving
// Config.Withholder nil: the tables this release carries, which is the one of
// 2026. A table stays in force until a later one replaces it, so the first
// closing of a new year is computed with the last table known; the version
// that carries the new year's figures is the one to upgrade to in January.
func DefaultWithholder() BrazilWithholder {
	w, err := NewBrazilWithholder(Withholding2026)
	if err != nil {
		panic("investor: the shipped withholding tables do not apply: " + err.Error())
	}
	return w
}

// tableOn is the table in force on the day: the latest one that started on or
// before it.
func (w BrazilWithholder) tableOn(day Day) (WithholdingTable, bool) {
	var found WithholdingTable
	ok := false
	for _, t := range w.tables {
		if t.From.Before(day) || t.From == day {
			found, ok = t, true
		}
	}
	return found, ok
}

// Withhold computes the retention of one payment.
func (w BrazilWithholder) Withhold(_ context.Context, in WithholdingInput) (Withholding, error) {
	table, ok := w.tableOn(in.Day)
	if !ok {
		return Withholding{}, fmt.Errorf("%w: %s", ErrNoWithholdingTable, in.Day)
	}
	out := Withholding{Table: table.Name}
	if in.Gross <= 0 {
		return out, nil
	}
	dependents := int64(in.Dependents)
	if dependents < 0 {
		dependents = 0
	}

	// The contribution: the rate over what is left under the month's ceiling.
	// The month's total is rounded once and this payment holds the difference,
	// so the centavos of several payments never add up to more than the month.
	room := table.INSSCeiling - in.Accrued.INSSBase
	if room < 0 {
		room = 0
	}
	base := in.Gross
	if base > room {
		base = room
	}
	monthBase := in.Accrued.INSSBase + base
	monthINSS := roundDiv(monthBase*table.INSSRateBps, 10000)
	inss := monthINSS - in.Accrued.INSS
	if inss < 0 {
		inss = 0
	}
	out.INSS = WithholdingLine{Base: base, RateBps: table.INSSRateBps, Amount: inss}

	// The income tax, over the month: the income so far, less the larger of
	// the legal deductions and the simplified discount.
	income := in.Accrued.Gross + in.Gross
	deduction := monthINSS + dependents*table.DependentDeduction
	if table.SimplifiedDiscount > deduction {
		deduction = table.SimplifiedDiscount
	}
	taxBase := income - deduction
	if taxBase < 0 {
		taxBase = 0
	}
	bracket := table.bracketFor(taxBase)
	tax := roundDiv(taxBase*bracket.RateBps, 10000) - bracket.Deduction
	if tax < 0 {
		tax = 0
	}
	reduction := table.reductionFor(income, tax)
	irrf := tax - reduction - in.Accrued.IRRF
	if irrf < 0 {
		irrf = 0
	}
	out.IRRF = WithholdingLine{Base: taxBase, RateBps: bracket.RateBps, Reduction: reduction, Amount: irrf}

	// Never hold back more than was credited.
	if over := out.Total() - in.Gross; over > 0 {
		out.IRRF.Amount -= over
		if out.IRRF.Amount < 0 {
			out.INSS.Amount += out.IRRF.Amount
			out.IRRF.Amount = 0
		}
	}
	return out, nil
}

// bracketFor is the line of the table a base falls in.
func (t WithholdingTable) bracketFor(base int64) IRRFBracket {
	for _, b := range t.IRRFBrackets {
		if b.Upto == 0 || base <= b.Upto {
			return b
		}
	}
	return t.IRRFBrackets[len(t.IRRFBrackets)-1]
}

// reductionFor is how much of the tax the reduction of Lei 15.270/2025 takes
// off, never more than the tax itself.
func (t WithholdingTable) reductionFor(income, tax int64) int64 {
	var reduction int64
	switch {
	case t.ReductionUpTo == 0 && t.ReductionEnd == 0:
		return 0
	case income <= t.ReductionUpTo:
		reduction = t.ReductionFull
	case income <= t.ReductionEnd:
		reduction = t.ReductionBase - roundDiv(income*t.ReductionFactorPPM, 1_000_000)
	}
	if reduction < 0 {
		reduction = 0
	}
	if reduction > tax {
		reduction = tax
	}
	return reduction
}
