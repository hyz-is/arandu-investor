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

// The kinds of payout account an investor declares, as arandu-wallet keeps
// them.
const (
	PayoutPix  = "pix"
	PayoutBank = "bank"
)

// BankAccountTypes are the kinds of bank account an investor can be paid
// into: checking and savings.
var BankAccountTypes = []string{"checking", "savings"}

// BankAccountRequest is a bank account an investor declares to be paid into.
// The holder is not here: an investor is paid into an account in the name and
// the document of their registration, whatever a form says.
type BankAccountRequest struct {
	// Bank is the bank's code or name: "341", "Itaú".
	Bank string
	// Branch is the agency, with its digit when it has one.
	Branch string
	// Number is the account number, with its digit.
	Number string
	// Type is one of BankAccountTypes.
	Type string
}

// Validate reports the errors per field.
func (r BankAccountRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "bank", strings.TrimSpace(r.Bank))
	validation.MaxLen(e, "bank", r.Bank, maxNameLen)
	validation.Required(e, "branch", strings.TrimSpace(r.Branch))
	validation.MaxLen(e, "branch", r.Branch, 12)
	validation.Required(e, "account_number", strings.TrimSpace(r.Number))
	validation.MaxLen(e, "account_number", r.Number, 24)
	if !slices.Contains(BankAccountTypes, r.Type) {
		e.Add("account_type", "must be one of "+strings.Join(BankAccountTypes, ", "))
	}
	return e
}

var _ validation.Validatable = BankAccountRequest{}

// holderName is the name an investor's accounts are held in: the company's
// registered name, or the person's.
func holderName(me *Investor) string {
	if me.Kind == Company && me.LegalName != "" {
		return me.LegalName
	}
	return me.Name
}

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
	account, err := s.wallets.AddPayoutAccount(ctx, actor, wallet.PayoutAccountRequest{
		HolderID:       me.UserID,
		Kind:           PayoutPix,
		Label:          "PIX " + key,
		Details:        wallet.Meta{"key_type": keyType, "key": key},
		HolderName:     holderName(me),
		HolderDocument: me.Document,
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventPayoutAccountAdded, "investor", me.ID, map[string]any{"account_id": account.ID, "kind": account.Kind, "label": account.Label}); err != nil {
		return nil, err
	}
	return account, nil
}

// AddBankAccount declares a bank account the investor who is asking is paid
// into, in the name and the document of their registration, as AddPixAccount
// does for a PIX key.
func (s *InvestorService) AddBankAccount(ctx context.Context, actor security.Subject, in BankAccountRequest) (*wallet.PayoutAccount, error) {
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
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	bank, branch, number := strings.TrimSpace(in.Bank), strings.TrimSpace(in.Branch), strings.TrimSpace(in.Number)
	account, err := s.wallets.AddPayoutAccount(ctx, actor, wallet.PayoutAccountRequest{
		HolderID: me.UserID,
		Kind:     PayoutBank,
		Label:    bank + " · " + branch + " · " + number,
		Details: wallet.Meta{
			"bank": bank, "branch": branch, "account_number": number, "account_type": in.Type,
		},
		HolderName:     holderName(me),
		HolderDocument: me.Document,
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventPayoutAccountAdded, "investor", me.ID, map[string]any{"account_id": account.ID, "kind": account.Kind, "label": account.Label}); err != nil {
		return nil, err
	}
	return account, nil
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
	req, err := s.wallets.StartProcessing(ctx, actor, requestID)
	if err != nil {
		return nil, err
	}
	if err := s.recordWithdrawal(ctx, g, actor, EventWithdrawalTaken, req, nil); err != nil {
		return nil, err
	}
	return req, nil
}

// recordWithdrawal writes the entry of an operator's act on a withdrawal.
func (s *InvestorService) recordWithdrawal(ctx context.Context, g security.Grant, actor security.Subject, event string, req *wallet.WithdrawalRequest, extra map[string]any) error {
	properties := map[string]any{"amount": int64(req.Amount), "status": string(req.Status)}
	for k, v := range extra {
		properties[k] = v
	}
	return s.record(ctx, g, actor, event, "wallet_withdrawal_request", req.ID, properties)
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
	req, err := s.wallets.PayWithdrawal(ctx, actor, in, receipt)
	if err != nil {
		return nil, err
	}
	if err := s.recordWithdrawal(ctx, g, actor, EventWithdrawalPaid, req, map[string]any{
		"end_to_end_id": req.EndToEndID, "receipt_name": req.ReceiptName, "receipt_sha256": req.ReceiptSHA256,
	}); err != nil {
		return nil, err
	}
	return req, nil
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
	req, err := s.wallets.RejectWithdrawal(ctx, actor, requestID, reason)
	if err != nil {
		return nil, err
	}
	if err := s.recordWithdrawal(ctx, g, actor, EventWithdrawalRejected, req, map[string]any{"reason": req.RejectedReason}); err != nil {
		return nil, err
	}
	return req, nil
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
