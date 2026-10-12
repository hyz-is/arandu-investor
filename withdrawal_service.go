package investor

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	wallet "github.com/hyz-is/arandu-wallet"
)

// The investor's side of a withdrawal. The flow itself -- the hold, the
// operator, the receipt -- is arandu-wallet's; what is decided here is this
// package's rule about what may be withdrawn: one invoice, whole, of a period
// already closed, with no minimum.

// InvoiceView is an invoice with the state of its withdrawal, as arandu-wallet
// knows it.
type InvoiceView struct {
	Invoice    Invoice
	Withdrawal *wallet.WithdrawalRequest
}

// Withdrawn reports whether the invoice was paid out.
func (v InvoiceView) Withdrawn() bool {
	return v.Withdrawal != nil && v.Withdrawal.Status == wallet.WithdrawalPaid
}

// Pending reports whether a request for it is waiting for an operator.
func (v InvoiceView) Pending() bool { return v.Withdrawal != nil && v.Withdrawal.Open() }

// Withdrawable reports whether the investor may ask for it now.
func (v InvoiceView) Withdrawable() bool {
	return v.Invoice.Amount > 0 &&
		(v.Invoice.Status == InvoiceAvailable || v.Invoice.Status == InvoiceOverdue) &&
		!v.Withdrawn() && !v.Pending()
}

// ErrNotWithdrawable is an invoice that cannot be asked for: nothing due,
// already paid out, or a request for it still waiting.
var ErrNotWithdrawable = errors.New("investor: this invoice cannot be withdrawn now")

// WithdrawInvoice asks arandu-wallet to pay one invoice, whole, to one of the
// investor's payout accounts. The investor asks for their own; the admin may
// ask on an investor's behalf.
func (s *InvestorService) WithdrawInvoice(ctx context.Context, actor security.Subject, invoiceID, accountID string) (*InvoiceView, error) {
	if accountID == "" {
		return nil, validation.Errors{"account_id": {"choose the account to be paid into"}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return nil, err
	}
	invoice, err := Invoices(s.db).WhereKey(invoiceID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if invoice == nil {
		return nil, ErrNotFound
	}
	owner, err := Investors(s.db).WhereKey(invoice.InvestorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ReportsView, *owner); err != nil {
		return nil, err
	}
	view, err := s.viewOf(ctx, actor, invoice)
	if err != nil {
		return nil, err
	}
	if !view.Withdrawable() {
		return nil, fmt.Errorf("%w: status %s, amount %d", ErrNotWithdrawable, invoice.Status, invoice.Amount)
	}
	w, err := s.wallets.FindBySlug(ctx, actor, owner.UserID, WalletSlug)
	if err != nil {
		return nil, err
	}
	attempt := invoice.WithdrawalAttempts + 1
	req, err := s.wallets.RequestWithdrawal(ctx, actor, wallet.WithdrawalRequestInput{
		IdempotencyKey: "investor:invoice:" + invoice.ID + ":withdrawal:" + strconv.Itoa(attempt),
		WalletID:       w.ID,
		Amount:         cents(invoice.Amount),
		AccountID:      accountID,
		Reference:      "investor:invoice:" + invoice.ID,
	})
	if err != nil {
		return nil, err
	}
	invoice.WithdrawalRequestID = req.ID
	invoice.WithdrawalAttempts = attempt
	invoice.UpdatedAt = s.now()
	if _, err := invoice.Save(ctx, g); err != nil {
		return nil, err
	}
	return &InvoiceView{Invoice: *invoice, Withdrawal: req}, nil
}

// InvoiceViews lists one investor's invoices with the state of each one's
// withdrawal. An invoice whose request was paid reads as withdrawn.
func (s *InvestorService) InvoiceViews(ctx context.Context, actor security.Subject, investorID string) ([]InvoiceView, error) {
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
	invoices, err := Invoices(s.db).Where("investor_id", "=", owner.ID).
		OrderByDesc("period_end").OrderBy("root_id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	out := make([]InvoiceView, 0, len(invoices))
	for _, inv := range invoices {
		v, err := s.viewOf(ctx, actor, inv)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// viewOf reads the invoice's latest withdrawal request from arandu-wallet,
// asked as the same subject, so the wallet's own policy decides what it shows.
func (s *InvestorService) viewOf(ctx context.Context, actor security.Subject, inv *Invoice) (*InvoiceView, error) {
	v := &InvoiceView{Invoice: *inv}
	if inv.WithdrawalRequestID == "" {
		return v, nil
	}
	requests, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{Limit: 200})
	if err != nil {
		return nil, err
	}
	for _, r := range requests {
		if r.ID == inv.WithdrawalRequestID {
			v.Withdrawal = r
			if r.Status == wallet.WithdrawalPaid {
				v.Invoice.Status = InvoiceWithdrawn
			}
			break
		}
	}
	return v, nil
}
