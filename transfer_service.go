package investor

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/translation"
	wallet "github.com/hyz-is/arandu-wallet"
)

// TransferRequest is who an investor passes to. The investor keeps its
// contracts, its entries, its invoices and its history; what changes is whose
// registration it is.
//
// There is no TenantID and no investor's identifier of the new owner's choice:
// the registration is named by the call, the tenant comes from the Grant.
type TransferRequest struct {
	// UserID is the login user of the new owner.
	UserID string
	// Kind is Person or Company. Empty keeps the registration's kind.
	Kind PersonKind
	// Document is the new owner's CPF or CNPJ, formatted or not.
	Document string
	// Name is the new owner's name, or the company's trade name.
	Name string
	// LegalName and LegalRepresentative are required for a company.
	LegalName           string
	LegalRepresentative string
	// Email is where the new owner is reached.
	Email string
	// EffectiveOn is the calendar day, YYYY-MM-DD, the new owner takes over. It
	// is a day already begun: a transfer is recorded when it is made and dated
	// to when it counts, never ahead of it.
	EffectiveOn string
	// Reason is why the registration changes hands, up to 1000 characters.
	Reason string
}

// Transfer passes an investor to another person: the registration (the login
// user, the document, the names and the e-mail) is the new owner's from then
// on, and the contracts, the entries, the invoices and the closings stay as
// they were. What the registration was is kept in an InvestorTransfer, and the
// act is recorded in the activity log with the owner before and the owner
// after, the documents masked.
//
// The previous owner's payout accounts in arandu-wallet are disabled, so the
// new owner declares their own. A transfer is refused, and changes nothing,
// while the previous owner's investor wallet still holds money or has a
// withdrawal in progress: that money is theirs to be paid first. The wallet of
// the previous owner is left as it is, with its history; the next closing
// opens the new owner's.
//
// The previous owner stops being the investor at once, so their own area of
// the application is gone. Taking the application's role away from them is the
// application's act, made beside this one.
func (s *InvestorService) Transfer(ctx context.Context, actor security.Subject, id string, in TransferRequest) (*Investor, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsTransfer, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := Investors(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsTransfer, *record); err != nil {
		return nil, err
	}

	kind := in.Kind
	if kind == "" {
		kind = record.Kind
	}
	userID := strings.TrimSpace(in.UserID)
	create := CreateRequest{
		UserID: userID, Kind: kind, Document: in.Document, Name: in.Name,
		LegalName: in.LegalName, LegalRepresentative: in.LegalRepresentative, Email: in.Email,
	}
	errs := create.Validate()
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || len([]rune(reason)) > maxTextLen {
		errs.Add("reason", message("reason_required", translation.Replace{"max": "1000"}))
	}
	day, err := ParseDay(in.EffectiveOn)
	switch {
	case err != nil || day.IsZero():
		errs.Add("effective_on", message("day", dayLayout))
	case DayOf(s.now(), s.location()).Before(day):
		errs.Add("effective_on", message("transfer_future_day"))
	}
	if errs.Any() {
		return nil, errs
	}
	document, _ := normalizeDocument(kind, in.Document)

	var (
		previous Investor
		history  *InvestorTransfer
		disabled int
		now      = s.now()
	)
	// Everything that decides the transfer is read again inside the
	// transaction, after the registration's row is locked: a closing deposits
	// into the owner it loaded, and a second transfer reads the owner it found,
	// so what they saw before the lock may be what the other one just changed.
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		locked, err := s.lockInvestor(ctx, g, id)
		if err != nil {
			return err
		}
		record = locked
		previous = *locked
		if userID == record.UserID {
			return validation.Errors{"user_id": {message("transfer_same_user")}}
		}
		if errs, err := s.transferConflicts(ctx, g, record, userID, document); err != nil || errs.Any() {
			if err != nil {
				return err
			}
			return errs
		}
		if errs, err := s.transferDayErrors(ctx, g, record, day); err != nil || errs.Any() {
			if err != nil {
				return err
			}
			return errs
		}
		if err := s.ensureOwedNothing(ctx, actor, previous.UserID); err != nil {
			return err
		}
		if disabled, err = s.releasePayoutAccounts(ctx, actor, previous.UserID); err != nil {
			return err
		}
		if err := s.settleInvoices(ctx, actor, g, record, previous.UserID, now); err != nil {
			return err
		}

		record.UserID, record.Kind, record.Document = userID, kind, document
		record.Name, record.Email = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email)
		record.LegalName, record.LegalRepresentative = "", ""
		if kind == Company {
			record.LegalName, record.LegalRepresentative = strings.TrimSpace(in.LegalName), strings.TrimSpace(in.LegalRepresentative)
		}
		record.UpdatedAt = now
		if _, err := record.Save(ctx, g); err != nil {
			return err
		}

		if history, err = InvestorTransfers(s.db).New(); err != nil {
			return err
		}
		if history.ID, err = data.NewID(); err != nil {
			return err
		}
		history.TenantID = data.Tenant(g)
		history.InvestorID = record.ID
		history.FromUserID, history.FromKind, history.FromDocument = previous.UserID, previous.Kind, previous.Document
		history.FromName, history.FromLegalName = previous.Name, previous.LegalName
		history.FromLegalRepresentative, history.FromEmail = previous.LegalRepresentative, previous.Email
		history.ToUserID, history.ToName = userID, strings.TrimSpace(in.Name)
		history.EffectiveOn, history.Reason, history.CreatedBy = day, reason, actor.ID
		history.CreatedAt, history.UpdatedAt = now, now
		if _, err := history.Save(ctx, g); err != nil {
			return err
		}

		// What was true when the lock was taken is checked again as the last
		// act, with the wallet as it stands: money that arrived since is
		// money the previous owner is owed, and the transfer is undone.
		if err := s.ensureOwedNothing(ctx, actor, previous.UserID); err != nil {
			return err
		}

		// The act is recorded in the transaction, so a transfer the log does
		// not show is a transfer that did not happen.
		return s.record(ctx, g, actor, EventInvestorTransferred, "investor", record.ID, map[string]any{
			"transfer_id":     history.ID,
			"effective_on":    string(day),
			"reason":          reason,
			"from_user_id":    previous.UserID,
			"from_name":       previous.DisplayName(),
			"from_document":   maskDocument(previous.Document),
			"to_user_id":      record.UserID,
			"to_name":         record.DisplayName(),
			"to_document":     maskDocument(record.Document),
			"payout_accounts": disabled,
		})
	})
	if err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			// A concurrent registration took the login user or the document
			// between the check and the write: say which one it was.
			if errs, lookup := s.transferConflicts(ctx, g, &previous, userID, document); lookup == nil && errs.Any() {
				return nil, errs
			}
			return nil, validation.Errors{"document": {message("transfer_document_taken")}}
		}
		return nil, err
	}
	return record, nil
}

// lockInvestor takes the row of one investor for the rest of the transaction
// and reads it again, as it stands once the lock is held. The write is what
// locks: it touches the registration's updated_at, on every engine, and a
// concurrent closing or transfer of the same investor waits behind it. It
// must run inside a transaction.
func (s *InvestorService) lockInvestor(ctx context.Context, g security.Grant, id string) (*Investor, error) {
	touched, err := Investors(s.db).WhereKey(id).Update(ctx, g, map[string]any{"updated_at": s.now()})
	if err != nil {
		return nil, err
	}
	if touched == 0 {
		return nil, ErrNotFound
	}
	record, err := Investors(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	return record, nil
}

// transferDayErrors refuses a day before the registration existed and a day
// before the one of the last transfer, so the history reads in order.
func (s *InvestorService) transferDayErrors(ctx context.Context, g security.Grant, record *Investor, day Day) (validation.Errors, error) {
	errs := validation.Errors{}
	if day.Before(DayOf(record.CreatedAt, s.location())) {
		errs.Add("effective_on", message("transfer_day_before_created"))
	}
	last, err := InvestorTransfers(s.db).Where("investor_id", "=", record.ID).
		OrderByDesc("effective_on").OrderByDesc("created_at").OrderByDesc("id").First(ctx, g)
	if err != nil {
		return nil, err
	}
	if last != nil && day.Before(last.EffectiveOn) {
		errs.Add("effective_on", message("transfer_day_before_last", translation.Replace{"day": string(last.EffectiveOn)}))
	}
	return errs, nil
}

// Transfers returns the changes of who the investor is, the earliest day they
// took effect first, for the admin. The registration itself is the current owner; these are the ones
// before.
func (s *InvestorService) Transfers(ctx context.Context, actor security.Subject, id string) ([]*InvestorTransfer, error) {
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}
	record, err := Investors(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, *record); err != nil {
		return nil, err
	}
	return InvestorTransfers(s.db).Where("investor_id", "=", record.ID).
		OrderBy("effective_on").OrderBy("created_at").OrderBy("id").Get(ctx, g)
}

// transferConflicts says whether the registration can be handed over: the new
// login user and the new document are nobody else's, and the previous owner's
// money has been paid out. Both unique indexes would refuse the first two too,
// and the answer here is a message on the field instead of a database error.
func (s *InvestorService) transferConflicts(ctx context.Context, g security.Grant, record *Investor, userID, document string) (validation.Errors, error) {
	errs := validation.Errors{}
	taken, err := Investors(s.db).Where("user_id", "=", userID).Where("id", "!=", record.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if taken != nil {
		errs.Add("user_id", message("transfer_user_taken"))
	}
	taken, err = Investors(s.db).Where("document", "=", document).Where("id", "!=", record.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if taken != nil {
		errs.Add("document", message("transfer_document_taken"))
	}
	return errs, nil
}

// ensureOwedNothing refuses, answering a validation error on the login user,
// while the previous owner's investor wallet holds money or one of their
// withdrawals is still open: that money is theirs to be paid first.
func (s *InvestorService) ensureOwedNothing(ctx context.Context, actor security.Subject, holderID string) error {
	if s.wallets == nil {
		return nil
	}
	held, err := s.wallets.FindBySlug(ctx, actor, holderID, WalletSlug)
	switch {
	case errors.Is(err, wallet.ErrNotFound):
	case err != nil:
		return err
	case held.Balance != 0 || held.Held != 0:
		return validation.Errors{"user_id": {message("transfer_funds")}}
	}
	// Only the requests that are still open matter, and they are asked for by
	// status: the list is newest first and bounded, so a holder with many paid
	// requests must not push an open one out of it.
	for _, status := range []wallet.WithdrawalStatus{wallet.WithdrawalRequested, wallet.WithdrawalProcessing} {
		open, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{HolderID: holderID, Status: status, Limit: 200})
		if err != nil {
			return err
		}
		if len(open) > 0 {
			return validation.Errors{"user_id": {message("transfer_withdrawals")}}
		}
	}
	return nil
}

// releasePayoutAccounts disables the accounts the previous owner declared to be
// paid into and returns how many it disabled.
func (s *InvestorService) releasePayoutAccounts(ctx context.Context, actor security.Subject, holderID string) (int, error) {
	if s.wallets == nil {
		return 0, nil
	}
	accounts, err := s.wallets.PayoutAccountsOf(ctx, actor, holderID)
	if err != nil {
		return 0, err
	}
	disabled := 0
	for _, a := range accounts {
		if a.Disabled {
			continue
		}
		if _, err := s.wallets.DisablePayoutAccount(ctx, actor, a.ID); err != nil {
			return 0, err
		}
		disabled++
	}
	return disabled, nil
}

// settleInvoices closes what the previous owner's withdrawals left open on the
// invoices, so the new owner reads them as they were. An invoice whose request
// was paid becomes withdrawn, for good: arandu-wallet shows a request only to
// its holder, and the new owner is not the holder. An invoice whose request
// was rejected or cancelled forgets it and can be asked for again by the new
// owner. One whose request cannot be found is left as it is, and is not
// offered for a new request.
func (s *InvestorService) settleInvoices(ctx context.Context, actor security.Subject, g security.Grant, record *Investor, holderID string, now time.Time) error {
	if s.wallets == nil {
		return nil
	}
	invoices, err := Invoices(s.db).Where("investor_id", "=", record.ID).
		Where("withdrawal_request_id", "!=", "").Get(ctx, g)
	if err != nil {
		return err
	}
	if len(invoices) == 0 {
		return nil
	}
	byStatus := map[string]wallet.WithdrawalStatus{}
	for _, status := range []wallet.WithdrawalStatus{wallet.WithdrawalPaid, wallet.WithdrawalRejected, wallet.WithdrawalCancelled} {
		requests, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{HolderID: holderID, Status: status, Limit: 200})
		if err != nil {
			return err
		}
		if len(requests) >= 200 {
			// More than a page of them: not all can be matched, and guessing
			// which invoice was paid is how money is paid twice.
			return validation.Errors{"user_id": {message("transfer_withdrawals")}}
		}
		for _, r := range requests {
			byStatus[r.ID] = r.Status
		}
	}
	for _, inv := range invoices {
		switch byStatus[inv.WithdrawalRequestID] {
		case wallet.WithdrawalPaid:
			inv.Status = InvoiceWithdrawn
		case wallet.WithdrawalRejected, wallet.WithdrawalCancelled:
			inv.WithdrawalRequestID = ""
		default:
			continue
		}
		inv.UpdatedAt = now
		if _, err := inv.Save(ctx, g); err != nil {
			return err
		}
	}
	return nil
}

// maskDocument keeps the first three and the last two digits of a CPF or a
// CNPJ: enough to tell two owners apart in the activity log, not enough to
// use it.
func maskDocument(document string) string {
	runes := []rune(document)
	if len(runes) <= 6 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:3]) + strings.Repeat("*", len(runes)-5) + string(runes[len(runes)-2:])
}
