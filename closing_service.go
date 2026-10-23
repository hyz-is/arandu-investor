package investor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	wallet "github.com/hyz-is/arandu-wallet"
)

// WalletSlug is the slug of the investor's wallet in arandu-wallet: one per
// investor, held by their login user, in reais. Every invoice is deposited
// there and every withdrawal of one is paid out of it.
const WalletSlug = "investor"

// PayoutVia is the name this package asks for its investors' withdrawals
// under, and the one their wallets are opened with: arandu-wallet refuses a
// request for those wallets that does not carry it.
const PayoutVia = "investor"

// periodEndFor is the last billing day on or before asOf: the exclusive end of
// the period a closing on asOf closes. The billing day itself belongs to the
// period that starts on it.
func periodEndFor(asOf Day, billingDay int) Day {
	t := asOf.Time()
	end := Day(time.Date(t.Year(), t.Month(), billingDay, 0, 0, 0, 0, time.UTC).Format(DayLayout))
	if asOf.Before(end) {
		end = Day(end.Time().AddDate(0, -1, 0).Format(DayLayout))
	}
	return end
}

// Close closes the period of one contract that ended on the last billing day
// on or before asOf, and answers the invoice it produced.
//
// The period starts where the last invoice of the contract ended, or on the
// day the contract started. The entries counted are those of the contract's
// products not counted before -- which is how an entry dated in a closed period
// is counted in the next closing instead of reopening the old one. The costs
// are those of the period's window, shared out over the revenue.
//
// Closing the same period twice answers the invoice the first closing made.
func (s *InvestorService) Close(ctx context.Context, actor security.Subject, contractID string, asOf Day) (*Invoice, error) {
	if asOf.IsZero() {
		return nil, validation.Errors{"as_of": {"must be a day in the form " + DayLayout}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	named, err := Contracts(s.db).WhereKey(contractID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if named == nil {
		return nil, ErrNotFound
	}
	owner, err := Investors(s.db).WhereKey(named.InvestorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, EntriesManage, *owner); err != nil {
		return nil, err
	}
	invoice, err := s.closeContract(ctx, actor, g, owner, named.RootID, asOf)
	if err != nil {
		return nil, err
	}
	if err := s.recordClosing(ctx, g, actor, invoice); err != nil {
		return nil, err
	}
	return invoice, nil
}

// recordClosing writes the entry of a period closed into an invoice.
func (s *InvestorService) recordClosing(ctx context.Context, g security.Grant, actor security.Subject, invoice *Invoice) error {
	return s.record(ctx, g, actor, EventPeriodClosed, "investor_invoice", invoice.ID, map[string]any{
		"investor_id": invoice.InvestorID, "period_start": string(invoice.PeriodStart), "period_end": string(invoice.PeriodEnd),
		"net": invoice.Net, "amount": invoice.Amount, "status": string(invoice.Status),
	})
}

// CloseDue closes every contract whose billing day is asOf's day: the admin's
// "close the contracts that are due today". A contract that fails is reported
// beside the others, and does not stop them.
func (s *InvestorService) CloseDue(ctx context.Context, actor security.Subject, asOf Day) ([]*Invoice, map[string]error, error) {
	if asOf.IsZero() {
		return nil, nil, validation.Errors{"as_of": {"must be a day in the form " + DayLayout}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, nil, err
	}
	due, err := Contracts(s.db).Where("status", "=", string(ContractActive)).
		Where("billing_day", "=", asOf.Time().Day()).OrderBy("created_at").Get(ctx, g)
	if err != nil {
		return nil, nil, err
	}
	var closed []*Invoice
	failed := map[string]error{}
	for _, c := range due {
		owner, err := Investors(s.db).WhereKey(c.InvestorID).First(ctx, g)
		if err != nil || owner == nil {
			failed[c.ID] = fmt.Errorf("investor of contract %s: %w", c.Number, errors.Join(err, ErrNotFound))
			continue
		}
		if _, err := security.Authorize(ctx, s.policy, actor, EntriesManage, *owner); err != nil {
			failed[c.ID] = err
			continue
		}
		invoice, err := s.closeContract(ctx, actor, g, owner, c.RootID, asOf)
		if err != nil {
			failed[c.ID] = err
			continue
		}
		if err := s.recordClosing(ctx, g, actor, invoice); err != nil {
			failed[c.ID] = err
			continue
		}
		closed = append(closed, invoice)
	}
	return closed, failed, nil
}

// closeContract does the closing after the caller authorized and loaded the
// investor.
func (s *InvestorService) closeContract(ctx context.Context, actor security.Subject, g security.Grant, owner *Investor, rootID string, asOf Day) (*Invoice, error) {
	if !owner.IsActive() {
		return nil, validation.Errors{"investor": {"is suspended and takes part in no new closing"}}
	}
	if s.wallets == nil {
		return nil, fmt.Errorf("investor: no wallet service was configured (Config.Wallet)")
	}
	current, err := Contracts(s.db).Where("root_id", "=", rootID).
		Where("status", "=", string(ContractActive)).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, validation.Errors{"contract": {"has no version in force"}}
	}
	original, err := Contracts(s.db).WhereKey(rootID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, ErrNotFound
	}

	end := periodEndFor(asOf, current.BillingDay)
	if !current.EndsOn.IsZero() && current.EndsOn.AddDays(1).Before(end) {
		end = current.EndsOn.AddDays(1)
	}
	start := original.StartsOn
	last, err := Invoices(s.db).Where("root_id", "=", rootID).OrderByDesc("period_end").First(ctx, g)
	if err != nil {
		return nil, err
	}
	if last != nil {
		if last.PeriodEnd == end {
			return last, nil
		}
		start = last.PeriodEnd
	}
	if !start.Before(end) {
		return nil, validation.Errors{"period": {fmt.Sprintf("nothing to close: the next period of contract %s ends on %s", current.Number, periodEndFor(start.AddDays(32), current.BillingDay))}}
	}

	contract, err := s.scopeOfContract(ctx, g, current)
	if err != nil {
		return nil, err
	}

	// Every entry dated before the end, of the contract's products, from the
	// contract's start on, not counted in an earlier invoice of this contract.
	counted := map[string]bool{}
	links, err := InvoiceEntries(s.db).Where("root_id", "=", rootID).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		counted[l.EntryID] = true
	}
	candidates, err := Entries(s.db).Where("occurred_on", "<", string(end)).
		Where("occurred_on", ">=", string(original.StartsOn)).OrderBy("occurred_at").OrderBy("id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	var gross, fees int64
	for _, e := range candidates {
		if counted[e.ID] || !contract.covers(e.ProductID) {
			continue
		}
		entries = append(entries, *e)
		gross += e.Gross
		fees += e.Fee
	}

	// The revenue the costs are shared over: the whole application's, in the
	// window of this period.
	window, err := Entries(s.db).Where("occurred_on", ">=", string(start)).
		Where("occurred_on", "<", string(end)).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	revenue := map[string]int64{}
	for _, e := range window {
		if revenueKinds[e.Kind] || e.Kind == KindReversal {
			revenue[e.ProductID] += e.Gross
		}
	}
	views, err := s.loadCosts(ctx, g)
	if err != nil {
		return nil, err
	}
	scoped := make([]ScopedCost, 0, len(views))
	for _, v := range views {
		scoped = append(scoped, ScopedCost{
			Amount: CostInWindow(v.Item, start, end, current.BillingDay),
			Scope:  v.scopeOf(),
		})
	}
	figures := ComputeFigures(gross, fees, AllocateCosts(scoped, revenue, contract), current.ShareBps)

	invoice, err := Invoices(s.db).New()
	if err != nil {
		return nil, err
	}
	if invoice.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	invoice.TenantID = data.Tenant(g)
	invoice.InvestorID = owner.ID
	invoice.ContractID = current.ID
	invoice.RootID = rootID
	invoice.PeriodStart, invoice.PeriodEnd = start, end
	invoice.Gross, invoice.Fees, invoice.Costs, invoice.Net = figures.Gross, figures.Fees, figures.Costs, figures.Net
	invoice.CostRatioPPM = figures.CostRatioPPM
	invoice.ShareBps = figures.ShareBps
	invoice.Amount = figures.Amount
	invoice.Status = InvoiceNothingDue
	if figures.Amount > 0 {
		invoice.Status = InvoiceAvailable
	}
	invoice.ClosedBy = actor.ID
	invoice.ClosedAt, invoice.CreatedAt, invoice.UpdatedAt = now, now, now

	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		// An invoice still waiting when the next period closes is overdue: still
		// the investor's to withdraw, and still its own period's.
		waiting, err := Invoices(s.db).Where("root_id", "=", rootID).
			Where("status", "=", string(InvoiceAvailable)).Get(ctx, g)
		if err != nil {
			return err
		}
		for _, w := range waiting {
			w.Status = InvoiceOverdue
			w.UpdatedAt = now
			if _, err := w.Save(ctx, g); err != nil {
				return err
			}
		}
		if invoice.Amount > 0 {
			receipt, err := s.deposit(ctx, actor, owner, invoice)
			if err != nil {
				return err
			}
			invoice.WalletOperationID = receipt.Operation.ID
		}
		if _, err := invoice.Save(ctx, g); err != nil {
			return err
		}
		for _, e := range entries {
			link, err := InvoiceEntries(s.db).New()
			if err != nil {
				return err
			}
			if link.ID, err = data.NewID(); err != nil {
				return err
			}
			link.TenantID = data.Tenant(g)
			link.InvoiceID = invoice.ID
			link.RootID = rootID
			link.EntryID = e.ID
			link.CreatedAt, link.UpdatedAt = now, now
			if _, err := link.Save(ctx, g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			// A concurrent closing of the same period won: answer its invoice. A
			// collision anywhere else -- an entry already counted -- is a fault,
			// and is reported as one.
			won, lookup := Invoices(s.db).Where("root_id", "=", rootID).Where("period_end", "=", string(end)).First(ctx, g)
			if lookup == nil && won != nil {
				return won, nil
			}
		}
		return nil, err
	}
	return invoice, nil
}

// scopeOfContract is the set of products a contract version counts.
func (s *InvestorService) scopeOfContract(ctx context.Context, g security.Grant, c *Contract) (Scope, error) {
	if c.AllProducts {
		return Scope{All: true}, nil
	}
	links, err := ContractProducts(s.db).Where("contract_id", "=", c.ID).Get(ctx, g)
	if err != nil {
		return Scope{}, err
	}
	set := map[string]bool{}
	for _, l := range links {
		set[l.ProductID] = true
	}
	return Scope{Products: set}, nil
}

// deposit puts an invoice's amount in the investor's wallet, opening the
// wallet the first time. The idempotency key is the invoice: a retried closing
// deposits once.
//
// The subject is the one closing the period, so the application gives its
// finance people arandu-wallet's operator role beside this package's admin
// role. A deposit is moving money, and this package does not mint a subject
// that may.
func (s *InvestorService) deposit(ctx context.Context, actor security.Subject, owner *Investor, invoice *Invoice) (wallet.Receipt, error) {
	w, err := s.wallets.FindBySlug(ctx, actor, owner.UserID, WalletSlug)
	if errors.Is(err, wallet.ErrNotFound) {
		w, err = s.wallets.Open(ctx, actor, wallet.OpenRequest{
			HolderID:      owner.UserID,
			Slug:          WalletSlug,
			Name:          "Investidor Anjo",
			Description:   "Faturas do Investidor Anjo",
			Currency:      "BRL",
			DecimalPlaces: 2,
			// The money leaves only through a withdrawal request an operator
			// pays, and only one this package asked for: the wallet's own
			// route cannot take a part of an invoice past these rules.
			PayoutVia: PayoutVia,
		})
	}
	if err != nil {
		return wallet.Receipt{}, err
	}
	return s.wallets.Deposit(ctx, actor, wallet.DepositRequest{
		IdempotencyKey: "investor:invoice:" + invoice.ID,
		WalletID:       w.ID,
		Amount:         cents(invoice.Amount),
	})
}

// Invoices lists one investor's invoices, newest period first. The admin may
// ask about anybody; an investor about themselves.
func (s *InvestorService) Invoices(ctx context.Context, actor security.Subject, investorID string) ([]*Invoice, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return nil, err
	}
	owner, err := Investors(s.db).WhereKey(investorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ReportsView, *owner); err != nil {
		return nil, err
	}
	return Invoices(s.db).Where("investor_id", "=", owner.ID).
		OrderByDesc("period_end").OrderBy("root_id").Get(ctx, g)
}
