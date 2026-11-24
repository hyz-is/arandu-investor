package investor

import (
	"context"
	"errors"
	"strings"

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
	if userID == record.UserID {
		errs.Add("user_id", message("transfer_same_user"))
	}
	if errs.Any() {
		return nil, errs
	}
	document, _ := normalizeDocument(kind, in.Document)

	if errs, err := s.transferConflicts(ctx, g, record, userID, document); err != nil || errs.Any() {
		if err != nil {
			return nil, err
		}
		return nil, errs
	}

	now := s.now()
	history, err := InvestorTransfers(s.db).New()
	if err != nil {
		return nil, err
	}
	if history.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	history.TenantID = data.Tenant(g)
	history.InvestorID = record.ID
	history.FromUserID, history.FromKind, history.FromDocument = record.UserID, record.Kind, record.Document
	history.FromName, history.FromLegalName = record.Name, record.LegalName
	history.FromLegalRepresentative, history.FromEmail = record.LegalRepresentative, record.Email
	history.ToUserID, history.ToName = userID, strings.TrimSpace(in.Name)
	history.EffectiveOn, history.Reason, history.CreatedBy = day, reason, actor.ID
	history.CreatedAt, history.UpdatedAt = now, now

	previous := *record
	disabled := 0
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		var err error
		if disabled, err = s.releasePayoutAccounts(ctx, actor, previous.UserID); err != nil {
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
		_, err = history.Save(ctx, g)
		return err
	})
	if err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			return nil, validation.Errors{"document": {message("transfer_document_taken")}}
		}
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventInvestorTransferred, "investor", record.ID, map[string]any{
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
	}); err != nil {
		return nil, err
	}
	return record, nil
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

// releasePayoutAccounts disables the accounts the previous owner declared to be
// paid into and returns how many it disabled. It runs before anything is
// written and refuses, without disabling any, while the previous owner's
// investor wallet holds money or a withdrawal of theirs is still open.
func (s *InvestorService) releasePayoutAccounts(ctx context.Context, actor security.Subject, holderID string) (int, error) {
	if s.wallets == nil {
		return 0, nil
	}
	held, err := s.wallets.FindBySlug(ctx, actor, holderID, WalletSlug)
	switch {
	case errors.Is(err, wallet.ErrNotFound):
	case err != nil:
		return 0, err
	case held.Balance != 0 || held.Held != 0:
		return 0, validation.Errors{"user_id": {message("transfer_funds")}}
	}
	requests, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{HolderID: holderID, Limit: 200})
	if err != nil {
		return 0, err
	}
	for _, r := range requests {
		if r.Status == wallet.WithdrawalRequested || r.Status == wallet.WithdrawalProcessing {
			return 0, validation.Errors{"user_id": {message("transfer_withdrawals")}}
		}
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
