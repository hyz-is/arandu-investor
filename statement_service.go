package investor

import (
	"context"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
)

// StatementFilter narrows a statement. Every field is optional.
type StatementFilter struct {
	From, To  Day // [From, To], by the day the entry happened
	Kind      EntryKind
	ProductID string
}

// Statement is one contract's entries as the investor reads them.
type Statement struct {
	Contract Contract
	Lines    []StatementLine
	// Totals of the lines shown.
	Gross, Fees, Costs, Net int64
	// Open marks the lines whose cost share is an estimate: they belong to the
	// period that has not closed yet, whose costs are the projection's.
	Open map[string]bool
}

// Projection is the open period of a contract: what it is expected to cost,
// what came in so far, and where that leaves the investor if nothing else
// happens before it closes.
type Projection struct {
	Contract              Contract
	PeriodStart           Day
	PeriodEnd             Day
	Costs                 []ProjectedCost
	Gross, Fees           int64
	ProjectedCosts        int64
	EstimatedNet          int64
	EstimatedAmount       int64
	EstimatedCostRatioPPM int64
}

// ProjectedCost is one declared cost and how much of it falls in the open
// period, before it is shared out.
type ProjectedCost struct {
	Item     CostItem
	Products []string
	Amount   int64
}

// contractFor loads a contract version and its investor, and asks the policy
// about the investor's row: the admin may read any, an investor their own.
func (s *InvestorService) contractFor(ctx context.Context, actor security.Subject, g security.Grant, contractID string, action security.Action) (*Contract, error) {
	record, err := Contracts(s.db).WhereKey(contractID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	owner, err := Investors(s.db).WhereKey(record.InvestorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, action, *owner); err != nil {
		return nil, err
	}
	// The version in force speaks for the contract; an old version named is
	// read as the contract it belongs to.
	current, err := Contracts(s.db).Where("root_id", "=", record.RootID).
		Where("status", "=", string(ContractActive)).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if current != nil {
		return current, nil
	}
	return record, nil
}

// StatementOf answers a contract's statement: each entry of its products with
// its share of the costs. A line of a closed period carries the cost ratio of
// the invoice that counted it; a line of the open period carries the
// projection's, and is marked as an estimate.
func (s *InvestorService) StatementOf(ctx context.Context, actor security.Subject, contractID string, f StatementFilter) (*Statement, error) {
	if !f.From.IsZero() && !f.To.IsZero() && f.To.Before(f.From) {
		return nil, validation.Errors{"to": {"must not be before from"}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Investor{})
	if err != nil {
		return nil, err
	}
	contract, err := s.contractFor(ctx, actor, g, contractID, DashboardView)
	if err != nil {
		return nil, err
	}
	scope, err := s.scopeOfContract(ctx, g, contract)
	if err != nil {
		return nil, err
	}
	original, err := Contracts(s.db).WhereKey(contract.RootID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, ErrNotFound
	}

	q := Entries(s.db).Where("occurred_on", ">=", string(original.StartsOn))
	if !f.From.IsZero() {
		q = q.Where("occurred_on", ">=", string(f.From))
	}
	if !f.To.IsZero() {
		q = q.Where("occurred_on", "<=", string(f.To))
	}
	if f.Kind != "" {
		q = q.Where("kind", "=", string(f.Kind))
	}
	if f.ProductID != "" {
		q = q.Where("product_id", "=", f.ProductID)
	}
	rows, err := q.OrderBy("occurred_at").OrderBy("id").Get(ctx, g)
	if err != nil {
		return nil, err
	}

	// Which invoice counted each entry, and that invoice's cost ratio.
	links, err := InvoiceEntries(s.db).Where("root_id", "=", contract.RootID).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	invoiceOf := map[string]string{}
	for _, l := range links {
		invoiceOf[l.EntryID] = l.InvoiceID
	}
	invoices, err := Invoices(s.db).Where("root_id", "=", contract.RootID).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	ratio := map[string]int64{}
	for _, inv := range invoices {
		ratio[inv.ID] = inv.CostRatioPPM
	}
	projection, err := s.project(ctx, g, contract, original)
	if err != nil {
		return nil, err
	}

	st := &Statement{Contract: *contract, Open: map[string]bool{}}
	for _, e := range rows {
		if !scope.covers(e.ProductID) {
			continue
		}
		ppm := projection.EstimatedCostRatioPPM
		if inv, ok := invoiceOf[e.ID]; ok {
			ppm = ratio[inv]
		} else {
			st.Open[e.ID] = true
		}
		line := StatementLine{Entry: *e}
		if e.Gross > 0 {
			line.Cost = roundDiv(e.Gross*ppm, 1_000_000)
		}
		line.Net = e.Gross - e.Fee - line.Cost
		st.Lines = append(st.Lines, line)
		st.Gross += e.Gross
		st.Fees += e.Fee
		st.Costs += line.Cost
		st.Net += line.Net
	}
	return st, nil
}

// ProjectionOf answers the open period of a contract.
func (s *InvestorService) ProjectionOf(ctx context.Context, actor security.Subject, contractID string) (*Projection, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Investor{})
	if err != nil {
		return nil, err
	}
	contract, err := s.contractFor(ctx, actor, g, contractID, DashboardView)
	if err != nil {
		return nil, err
	}
	original, err := Contracts(s.db).WhereKey(contract.RootID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, ErrNotFound
	}
	return s.project(ctx, g, contract, original)
}

// project computes the open period after the caller authorized: from the end of
// the last invoice (or the contract's start) to the next billing day after
// today.
func (s *InvestorService) project(ctx context.Context, g security.Grant, contract, original *Contract) (*Projection, error) {
	today := DayOf(s.now(), s.location())
	start := original.StartsOn
	last, err := Invoices(s.db).Where("root_id", "=", contract.RootID).OrderByDesc("period_end").First(ctx, g)
	if err != nil {
		return nil, err
	}
	if last != nil {
		start = last.PeriodEnd
	}
	end := periodEndFor(today, contract.BillingDay)
	for !start.Before(end) {
		end = Day(end.Time().AddDate(0, 1, 0).Format(DayLayout))
	}
	if end.Before(today) || end == today {
		end = Day(end.Time().AddDate(0, 1, 0).Format(DayLayout))
	}

	scope, err := s.scopeOfContract(ctx, g, contract)
	if err != nil {
		return nil, err
	}
	window, err := Entries(s.db).Where("occurred_on", ">=", string(start)).
		Where("occurred_on", "<", string(end)).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	p := &Projection{Contract: *contract, PeriodStart: start, PeriodEnd: end}
	revenue := map[string]int64{}
	for _, e := range window {
		if revenueKinds[e.Kind] || e.Kind == KindReversal {
			revenue[e.ProductID] += e.Gross
		}
		if scope.covers(e.ProductID) {
			p.Gross += e.Gross
			p.Fees += e.Fee
		}
	}
	views, err := s.loadCosts(ctx, g)
	if err != nil {
		return nil, err
	}
	scoped := make([]ScopedCost, 0, len(views))
	for _, v := range views {
		amount := CostInWindow(v.Item, start, end, contract.BillingDay)
		if amount == 0 {
			continue
		}
		p.Costs = append(p.Costs, ProjectedCost{Item: v.Item, Products: v.Products, Amount: amount})
		scoped = append(scoped, ScopedCost{Amount: amount, Scope: v.scopeOf()})
	}
	f := ComputeFigures(p.Gross, p.Fees, AllocateCosts(scoped, revenue, scope), contract.ShareBps)
	p.ProjectedCosts = f.Costs
	p.EstimatedNet = f.Net
	p.EstimatedAmount = f.Amount
	p.EstimatedCostRatioPPM = f.CostRatioPPM
	return p, nil
}
