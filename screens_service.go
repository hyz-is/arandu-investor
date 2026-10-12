package investor

import (
	"context"
	"io"
	"slices"
	"strings"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/filesystem"
	wallet "github.com/hyz-is/arandu-wallet"
)

// What the screens need beyond the use cases: the wallet behind the panel, the
// admin's books a page at a time, and the operator's side of an investor's
// withdrawal. Every method asks the policy before it reads, like every other.

// PayoutAccounts are the accounts the investor can be paid into, from
// arandu-wallet, the usable ones first.
func (s *InvestorService) PayoutAccounts(ctx context.Context, actor security.Subject, investorID string) ([]*wallet.PayoutAccount, error) {
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
	return s.wallets.PayoutAccountsOf(ctx, actor, owner.UserID)
}

// PixKeyTypes are the kinds of PIX key an investor can be paid into.
var PixKeyTypes = []string{"cpf", "cnpj", "email", "phone", "random"}

// AddPixAccount declares a PIX key the investor who is asking is paid into,
// in the name and the document of their registration: an investor is paid
// into an account of their own, never a third party's.
func (s *InvestorService) AddPixAccount(ctx context.Context, actor security.Subject, keyType, key string) (*wallet.PayoutAccount, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return nil, err
	}
	me, err := Investors(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if me == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ReportsView, *me); err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	errs := validation.Errors{}
	validation.Required(errs, "pix_key", key)
	validation.MaxLen(errs, "pix_key", key, maxNameLen)
	if !slices.Contains(PixKeyTypes, keyType) {
		errs.Add("pix_key_type", "must be one of "+strings.Join(PixKeyTypes, ", "))
	}
	if errs.Any() {
		return nil, errs
	}
	name := me.Name
	if me.Kind == Company && me.LegalName != "" {
		name = me.LegalName
	}
	return s.wallets.AddPayoutAccount(ctx, actor, wallet.PayoutAccountRequest{
		HolderID:       me.UserID,
		Kind:           "pix",
		Label:          "PIX " + key,
		Details:        wallet.Meta{"key_type": keyType, "key": key},
		HolderName:     name,
		HolderDocument: me.Document,
	})
}

// EntriesQuery narrows the admin's books. Page counts from one.
type EntriesQuery struct {
	Search  string
	Kind    EntryKind
	Sort    string
	Dir     string
	Page    int
	PerPage int
}

// sortableEntry is the ordering allowlist of the books.
var sortableEntry = map[string]string{
	"": "occurred_at", "occurred_on": "occurred_at", "gross": "gross", "product": "product_id", "kind": "kind",
}

// EntriesPage is a page of the admin's books and how many rows the whole
// listing has.
func (s *InvestorService) EntriesPage(ctx context.Context, actor security.Subject, q EntriesQuery) ([]*Entry, int, error) {
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, 0, err
	}
	column, ok := sortableEntry[q.Sort]
	if !ok {
		return nil, 0, validation.Errors{"sort": {"is not a column the books can be ordered by"}}
	}
	if q.PerPage <= 0 || q.PerPage > maxLimit {
		q.PerPage = defaultLimit
	}
	if q.Page < 1 {
		q.Page = 1
	}
	filtered := func() *EntryQuery {
		b := Entries(s.db)
		if q.Kind != "" {
			b = b.Where("kind", "=", string(q.Kind))
		}
		if term := strings.TrimSpace(q.Search); term != "" {
			like := "%" + term + "%"
			b = b.Where(func(w *EntryQuery) {
				w.Where("description", "like", like).OrWhere("external_ref", "like", like).OrWhere("product_id", "like", like)
			})
		}
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	page := filtered()
	if q.Dir == "asc" {
		page = page.OrderBy(column)
	} else {
		page = page.OrderByDesc(column)
	}
	rows, err := page.OrderByDesc("id").Offset((q.Page-1)*q.PerPage).Limit(q.PerPage).Get(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	return rows, int(total), nil
}

// InvestorWithdrawal is one investor's withdrawal request, with the invoice
// and the investor it pays.
type InvestorWithdrawal struct {
	Request  *wallet.WithdrawalRequest
	Invoice  *Invoice
	Investor *Investor
}

// Withdrawals is the operator's queue of investors' withdrawals: the requests
// arandu-wallet holds whose invoice is one of this tenant's.
func (s *InvestorService) Withdrawals(ctx context.Context, actor security.Subject, status wallet.WithdrawalStatus) ([]InvestorWithdrawal, error) {
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	invoices, err := Invoices(s.db).Where("withdrawal_request_id", "!=", "").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	byRequest := map[string]*Invoice{}
	for _, inv := range invoices {
		byRequest[inv.WithdrawalRequestID] = inv
	}
	requests, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{Status: status, Limit: 200})
	if err != nil {
		return nil, err
	}
	owners := map[string]*Investor{}
	var out []InvestorWithdrawal
	for _, r := range requests {
		inv := byRequest[r.ID]
		if inv == nil {
			continue
		}
		owner, ok := owners[inv.InvestorID]
		if !ok {
			if owner, err = Investors(s.db).WhereKey(inv.InvestorID).First(ctx, g); err != nil {
				return nil, err
			}
			owners[inv.InvestorID] = owner
		}
		out = append(out, InvestorWithdrawal{Request: r, Invoice: inv, Investor: owner})
	}
	return out, nil
}

// invoiceOfRequest finds the invoice a withdrawal request pays, after the
// caller authorized the probe, and asks the policy about its investor.
func (s *InvestorService) invoiceOfRequest(ctx context.Context, actor security.Subject, g security.Grant, requestID string, action security.Action) (*Invoice, error) {
	inv, err := Invoices(s.db).Where("withdrawal_request_id", "=", requestID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, ErrNotFound
	}
	owner, err := Investors(s.db).WhereKey(inv.InvestorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, action, *owner); err != nil {
		return nil, err
	}
	return inv, nil
}

// StartPaying is the operator taking an investor's withdrawal to pay it.
func (s *InvestorService) StartPaying(ctx context.Context, actor security.Subject, requestID string) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	if _, err := Invoices(s.db).Where("withdrawal_request_id", "=", requestID).First(ctx, g); err != nil {
		return nil, err
	}
	if _, err := s.invoiceOfRequest(ctx, actor, g, requestID, EntriesManage); err != nil {
		return nil, err
	}
	return s.wallets.StartProcessing(ctx, actor, requestID)
}

// PayInvestorWithdrawal records that the operator paid an investor's
// withdrawal, with the receipt.
func (s *InvestorService) PayInvestorWithdrawal(ctx context.Context, actor security.Subject, in wallet.PaymentInput, receipt io.Reader) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	if _, err := Invoices(s.db).Where("withdrawal_request_id", "=", in.RequestID).First(ctx, g); err != nil {
		return nil, err
	}
	if _, err := s.invoiceOfRequest(ctx, actor, g, in.RequestID, EntriesManage); err != nil {
		return nil, err
	}
	return s.wallets.PayWithdrawal(ctx, actor, in, receipt)
}

// RejectInvestorWithdrawal refuses an investor's withdrawal with the reason
// they will read. The invoice can be asked for again.
func (s *InvestorService) RejectInvestorWithdrawal(ctx context.Context, actor security.Subject, requestID, reason string) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, EntriesManage, Investor{})
	if err != nil {
		return nil, err
	}
	if _, err := Invoices(s.db).Where("withdrawal_request_id", "=", requestID).First(ctx, g); err != nil {
		return nil, err
	}
	if _, err := s.invoiceOfRequest(ctx, actor, g, requestID, EntriesManage); err != nil {
		return nil, err
	}
	return s.wallets.RejectWithdrawal(ctx, actor, requestID, reason)
}

// InvoiceReceipt opens the receipt of an invoice's paid withdrawal, for the
// investor and the admin. The caller closes the body.
func (s *InvestorService) InvoiceReceipt(ctx context.Context, actor security.Subject, invoiceID string) (*wallet.WithdrawalRequest, filesystem.File, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return nil, filesystem.File{}, err
	}
	inv, err := Invoices(s.db).WhereKey(invoiceID).First(ctx, g)
	if err != nil {
		return nil, filesystem.File{}, err
	}
	if inv == nil || inv.WithdrawalRequestID == "" {
		return nil, filesystem.File{}, ErrNotFound
	}
	if _, err := s.invoiceOfRequest(ctx, actor, g, inv.WithdrawalRequestID, ReportsView); err != nil {
		return nil, filesystem.File{}, err
	}
	return s.wallets.WithdrawalReceipt(ctx, actor, inv.WithdrawalRequestID)
}

// AdminCosts lists the declared costs for the admin who declares them.
func (s *InvestorService) AdminCosts(ctx context.Context, actor security.Subject) ([]CostView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, CostsManage, Investor{})
	if err != nil {
		return nil, err
	}
	if _, err := CostItems(s.db).Count(ctx, g); err != nil {
		return nil, err
	}
	return s.loadCosts(ctx, g)
}
