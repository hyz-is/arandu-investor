package feature_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	wallet "github.com/hyz-is/arandu-wallet"

	investor "github.com/hyz-is/arandu-investor"
)

// cpfCarla is a third valid CPF, the new owner of the registrations below.
const cpfCarla = "390.533.447-05"

// today is the calendar day of the application's clock, the one a transfer
// made now is dated to: a registration cannot be passed on a day before it
// existed.
func today() string {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	return time.Now().In(loc).Format(investor.DayLayout)
}

func carla(userID string) investor.TransferRequest {
	return investor.TransferRequest{
		UserID: userID, Document: cpfCarla, Name: "Carla Souza", Email: "carla@example.com",
		EffectiveOn: today(), Reason: "gift to the investor's daughter",
	}
}

func activityCount(t *testing.T, w world, event, causer string) int {
	t.Helper()
	var n int
	if err := w.sql.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM activity_log WHERE tenant_id = ? AND log_name = ? AND event = ? AND causer_id = ?`,
		tenant, investor.ActivityLogName, event, causer).Scan(&n); err != nil {
		t.Fatalf("reading the activity log: %v", err)
	}
	return n
}

func TestTheInvestorPassesToAnotherPersonWithItsContractsAndBooks(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)

	// The previous owner has a PIX key and no money in the wallet.
	if _, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", "ana@example.com"); err != nil {
		t.Fatalf("declaring the PIX key: %v", err)
	}

	moved, err := w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla"))
	if err != nil {
		t.Fatalf("transferring: %v", err)
	}
	if moved.ID != ana.ID || moved.UserID != "user-carla" || moved.Name != "Carla Souza" || moved.Email != "carla@example.com" {
		t.Fatalf("the registration after the transfer is %+v", moved)
	}
	if moved.Document != "39053344705" || !moved.IsActive() {
		t.Fatalf("the document is %q and the status %q", moved.Document, moved.Status)
	}

	// The new owner reads the registration and the contract; the previous one
	// is nobody's investor any more.
	mine, err := w.svc.Mine(ctx, user("user-carla"))
	if err != nil || mine.ID != ana.ID {
		t.Fatalf("the new owner's registration: %v, %v", mine, err)
	}
	if _, err := w.svc.Mine(ctx, user("user-ana")); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("the previous owner still has the registration: %v", err)
	}
	contracts, err := w.svc.InvestorContracts(ctx, admin(), ana.ID)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("the contracts after the transfer: %d, %v", len(contracts), err)
	}
	var entries int
	if err := w.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM investor_entries`).Scan(&entries); err != nil || entries != 1 {
		t.Fatalf("the entries after the transfer: %d, %v", entries, err)
	}

	// The previous owner's PIX key takes no new request.
	accounts, err := w.wallets.PayoutAccountsOf(ctx, admin(), "user-ana")
	if err != nil || len(accounts) != 1 || !accounts[0].Disabled {
		t.Fatalf("the previous owner's accounts: %+v, %v", accounts, err)
	}

	// What the registration was is kept, and the act is in the activity log.
	history, err := w.svc.Transfers(ctx, admin(), ana.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("the history: %d, %v", len(history), err)
	}
	h := history[0]
	if h.FromUserID != "user-ana" || h.FromDocument != "52998224725" || h.FromName != "Ana user-ana" ||
		h.ToUserID != "user-carla" || string(h.EffectiveOn) != today() || h.CreatedBy != "finance-1" || h.Reason == "" {
		t.Fatalf("the history row is %+v", h)
	}
	if activityCount(t, w, investor.EventInvestorTransferred, "finance-1") != 1 {
		t.Error("the transfer is not in the activity log under the admin")
	}
	var properties string
	if err := w.sql.QueryRowContext(ctx, `SELECT properties FROM activity_log WHERE event = ?`, investor.EventInvestorTransferred).Scan(&properties); err != nil {
		t.Fatalf("reading the entry: %v", err)
	}
	if strings.Contains(properties, "52998224725") || strings.Contains(properties, "39053344705") || !strings.Contains(properties, "529******25") {
		t.Errorf("the entry carries a document that is not masked: %s", properties)
	}

	// The next closing pays the new owner's wallet, and opens none for the old.
	if _, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20"); err != nil {
		t.Fatalf("closing after the transfer: %v", err)
	}
	if got := balance(t, w, "user-carla"); got <= 0 {
		t.Fatalf("the new owner's wallet holds %d", got)
	}
	if _, err := w.wallets.FindBySlug(ctx, admin(), "user-ana", investor.WalletSlug); err == nil {
		t.Error("the previous owner's wallet was opened by the closing")
	}
}

func TestATransferIsRefusedWhileThePreviousOwnerIsOwedMoney(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)
	if _, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", "ana@example.com"); err != nil {
		t.Fatalf("declaring the PIX key: %v", err)
	}
	if _, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20"); err != nil {
		t.Fatalf("closing: %v", err)
	}

	var refused validation.Errors
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla")); !errors.As(err, &refused) || len(refused["user_id"]) == 0 {
		t.Fatalf("a transfer over money owed was not refused on the owner: %v", err)
	}
	// Nothing changed: the registration, the history and the PIX key.
	if mine, err := w.svc.Mine(ctx, user("user-ana")); err != nil || mine.UserID != "user-ana" {
		t.Fatalf("the refusal changed the registration: %v, %v", mine, err)
	}
	if history, err := w.svc.Transfers(ctx, admin(), ana.ID); err != nil || len(history) != 0 {
		t.Fatalf("the refusal left a history: %d, %v", len(history), err)
	}
	accounts, err := w.wallets.PayoutAccountsOf(ctx, admin(), "user-ana")
	if err != nil || len(accounts) != 1 || accounts[0].Disabled {
		t.Fatalf("the refusal disabled the account: %+v, %v", accounts, err)
	}
}

func TestATransferRefusesWhatCannotBeRegistered(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	registerPerson(t, w, "user-bruno", cpfBruno)

	refuse := map[string]struct {
		change func(*investor.TransferRequest)
		field  string
	}{
		"the same login user":           {func(r *investor.TransferRequest) { r.UserID = "user-ana" }, "user_id"},
		"another investor's login user": {func(r *investor.TransferRequest) { r.UserID = "user-bruno" }, "user_id"},
		"another investor's document":   {func(r *investor.TransferRequest) { r.Document = cpfBruno }, "document"},
		"a document that is not a CPF":  {func(r *investor.TransferRequest) { r.Document = "529.982.247-24" }, "document"},
		"a day still to come":           {func(r *investor.TransferRequest) { r.EffectiveOn = "2999-01-01" }, "effective_on"},
		"no day":                        {func(r *investor.TransferRequest) { r.EffectiveOn = "" }, "effective_on"},
		"no reason":                     {func(r *investor.TransferRequest) { r.Reason = "  " }, "reason"},
		"no name":                       {func(r *investor.TransferRequest) { r.Name = "" }, "name"},
		"no e-mail":                     {func(r *investor.TransferRequest) { r.Email = "" }, "email"},
		"no login user":                 {func(r *investor.TransferRequest) { r.UserID = "" }, "user_id"},
	}
	for name, c := range refuse {
		request := carla("user-carla")
		c.change(&request)
		var errs validation.Errors
		if _, err := w.svc.Transfer(ctx, admin(), ana.ID, request); !errors.As(err, &errs) || len(errs[c.field]) == 0 {
			t.Errorf("%s: want a refusal on %s, got %v", name, c.field, err)
		}
	}
	if history, err := w.svc.Transfers(ctx, admin(), ana.ID); err != nil || len(history) != 0 {
		t.Fatalf("a refused transfer left %d rows of history: %v", len(history), err)
	}
	if _, err := w.svc.Transfer(ctx, admin(), "no-such-investor", carla("user-carla")); !errors.Is(err, investor.ErrNotFound) {
		t.Errorf("a transfer of an unknown investor: %v", err)
	}
}

func TestOnlyTheAdminTransfersAnInvestor(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)

	other := security.Subject{ID: "finance-1", Tenant: "globex", Roles: []string{investor.AdminRole},
		Actions: []security.Action{investor.InvestorsTransfer}, Verified: true}
	if _, err := w.svc.Transfer(ctx, other, ana.ID, carla("user-carla")); !errors.Is(err, investor.ErrNotFound) {
		t.Errorf("another tenant's admin reached the investor: %v", err)
	}
	if _, err := w.svc.Transfers(ctx, other, ana.ID); !errors.Is(err, investor.ErrNotFound) {
		t.Errorf("another tenant's admin read the history: %v", err)
	}
	for name, who := range map[string]security.Subject{
		"the investor":  user("user-ana"),
		"somebody else": user("user-bruno"),
		"a guest":       security.Guest(tenant),
	} {
		if _, err := w.svc.Transfer(ctx, who, ana.ID, carla("user-carla")); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s transferred an investor: %v", name, err)
		}
		if _, err := w.svc.Transfers(ctx, who, ana.ID); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s read the history of an investor: %v", name, err)
		}
	}
	// The admin role and the admin action do not carry the transfer: the
	// action of its own does.
	if _, err := w.svc.Transfer(ctx, adminWithoutTransfer(), ana.ID, carla("user-carla")); !errors.Is(err, security.ErrForbidden) {
		t.Errorf("the admin role transferred an investor: %v", err)
	}
	if _, err := w.svc.Transfer(ctx, financeByAction(), ana.ID, carla("user-carla")); !errors.Is(err, security.ErrForbidden) {
		t.Errorf("the admin action transferred an investor: %v", err)
	}
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla")); err != nil {
		t.Fatalf("the transfer action was refused: %v", err)
	}
}

func TestAnInvestorPassesMoreThanOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)

	if _, err := w.sql.ExecContext(ctx, `UPDATE investors SET created_at = '2020-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	first := carla("user-carla")
	first.EffectiveOn = string(investor.Day(today()).AddDays(-1))
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, first); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	back := investor.TransferRequest{
		UserID: "user-ana", Document: cpfAna, Name: "Ana Souza", Email: "ana@example.com",
		EffectiveOn: today(), Reason: "given back",
	}
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, back); err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	history, err := w.svc.Transfers(ctx, admin(), ana.ID)
	if err != nil || len(history) != 2 || history[0].FromUserID != "user-ana" || history[1].FromUserID != "user-carla" {
		t.Fatalf("the history is %d rows, %v", len(history), err)
	}
}

// paidInvoice closes Ana's October period, has her withdraw it and has the
// operator pay it, so her wallet is empty and the invoice is paid.
func paidInvoice(t *testing.T, w world, ana *investor.Investor, c *investor.Contract) (*investor.Invoice, string) {
	t.Helper()
	ctx := context.Background()
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	account, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", "ana@example.com")
	if err != nil {
		t.Fatalf("declaring the PIX key: %v", err)
	}
	view, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	if _, err := w.svc.StartPaying(ctx, admin(), view.Withdrawal.ID); err != nil {
		t.Fatalf("taking the withdrawal: %v", err)
	}
	_, err = w.svc.PayInvestorWithdrawal(ctx, admin(), wallet.PaymentInput{
		RequestID: view.Withdrawal.ID, EndToEndID: "E1", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF"))
	if err != nil {
		t.Fatalf("paying: %v", err)
	}
	return invoice, view.Withdrawal.ID
}

// An invoice paid to the previous owner is still paid for the new one: the
// request that paid it is the previous owner's, arandu-wallet shows it to them
// alone, and it must not be read as no request at all.
func TestAnInvoicePaidToThePreviousOwnerIsNotWithdrawableByTheNewOne(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	paid, requestID := paidInvoice(t, w, ana, c)

	// Paying recorded the invoice as paid, before any transfer.
	stored, err := w.svc.Invoices(ctx, admin(), ana.ID)
	if err != nil || len(stored) != 1 || stored[0].Status != investor.InvoiceWithdrawn {
		t.Fatalf("the paid invoice is %+v, %v; want withdrawn", stored, err)
	}

	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla")); err != nil {
		t.Fatalf("transferring: %v", err)
	}
	views, err := w.svc.InvoiceViews(ctx, user("user-carla"), ana.ID)
	if err != nil || len(views) != 1 {
		t.Fatalf("the new owner's invoices: %d, %v", len(views), err)
	}
	if v := views[0]; v.Invoice.Status != investor.InvoiceWithdrawn || !v.Withdrawn() || v.Withdrawable() {
		t.Fatalf("the new owner reads the paid invoice as %s, withdrawn %v, withdrawable %v", v.Invoice.Status, v.Withdrawn(), v.Withdrawable())
	}

	record(t, w, "sale-2", investor.KindSale, "loccrm", "2026-11-05", 1000000, 0)
	next, err := w.svc.Close(ctx, admin(), c.ID, "2026-11-20")
	if err != nil {
		t.Fatalf("closing the next period: %v", err)
	}
	account, err := w.svc.AddPixAccount(ctx, user("user-carla"), "email", "carla@example.com")
	if err != nil {
		t.Fatalf("the new owner's PIX key: %v", err)
	}
	if _, err := w.svc.WithdrawInvoice(ctx, user("user-carla"), paid.ID, account.ID); !errors.Is(err, investor.ErrNotWithdrawable) {
		t.Fatalf("the new owner withdrew the invoice already paid: %v", err)
	}
	if _, err := w.svc.WithdrawInvoice(ctx, user("user-carla"), next.ID, account.ID); err != nil {
		t.Fatalf("the new owner could not withdraw their own invoice: %v", err)
	}

	// The proof of the first payment is where it was.
	after, err := w.svc.Invoices(ctx, admin(), ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, inv := range after {
		if inv.ID == paid.ID && inv.WithdrawalRequestID != requestID {
			t.Fatalf("the paid invoice points at %q, want %q", inv.WithdrawalRequestID, requestID)
		}
	}
	if _, _, err := w.svc.InvoiceReceipt(ctx, admin(), paid.ID); err != nil {
		t.Fatalf("the receipt of the payment was lost: %v", err)
	}
}

// An invoice that names a request nobody asking can see is not offered for a
// new one, whatever its status says.
func TestAnInvoiceWhoseRequestCannotBeSeenIsNotOffered(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if _, err := w.sql.ExecContext(ctx, `UPDATE investor_invoices SET withdrawal_request_id = 'somebody-elses' WHERE id = ?`, invoice.ID); err != nil {
		t.Fatal(err)
	}
	account, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", "ana@example.com")
	if err != nil {
		t.Fatalf("the PIX key: %v", err)
	}
	if _, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID); !errors.Is(err, investor.ErrNotWithdrawable) {
		t.Fatalf("an invoice with a request nobody can see was withdrawn: %v", err)
	}
}

func TestATransferIsNotDatedBeforeTheRegistrationOrTheLastTransfer(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)

	early := carla("user-carla")
	early.EffectiveOn = "2020-01-01"
	var errs validation.Errors
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, early); !errors.As(err, &errs) || len(errs["effective_on"]) == 0 {
		t.Fatalf("a day before the registration was accepted: %v", err)
	}

	// The registration is taken to have been made long ago, so that the only
	// thing left to refuse a day is the last transfer.
	if _, err := w.sql.ExecContext(ctx, `UPDATE investors SET created_at = '2020-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla")); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	back := investor.TransferRequest{
		UserID: "user-ana", Document: cpfAna, Name: "Ana Souza", Email: "ana@example.com",
		EffectiveOn: string(investor.Day(today()).AddDays(-1)), Reason: "given back",
	}
	errs = nil
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, back); !errors.As(err, &errs) || len(errs["effective_on"]) == 0 {
		t.Fatalf("a day before the last transfer was accepted: %v", err)
	}
	if history, err := w.svc.Transfers(ctx, admin(), ana.ID); err != nil || len(history) != 1 {
		t.Fatalf("the refusals left %d rows of history: %v", len(history), err)
	}
}

// The login user and the document are told apart when a registration is
// refused for either.
func TestATransferSaysWhichOfTheTwoIsTaken(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	registerPerson(t, w, "user-bruno", cpfBruno)

	byUser := carla("user-bruno")
	var errs validation.Errors
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, byUser); !errors.As(err, &errs) || len(errs["user_id"]) == 0 || len(errs["document"]) != 0 {
		t.Fatalf("a taken login user answered %v", err)
	}
	byDocument := carla("user-carla")
	byDocument.Document = cpfBruno
	errs = nil
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, byDocument); !errors.As(err, &errs) || len(errs["document"]) == 0 || len(errs["user_id"]) != 0 {
		t.Fatalf("a taken document answered %v", err)
	}
}

// A payout key is not written in the clear in the activity log.
func TestThePayoutKeyIsMaskedInTheActivityLog(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	registerPerson(t, w, "user-ana", cpfAna)
	if _, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", "ana.souza@example.com"); err != nil {
		t.Fatalf("the PIX key: %v", err)
	}
	if _, err := w.svc.AddBankAccount(ctx, user("user-ana"), investor.BankAccountRequest{Bank: "341", Branch: "0001", Number: "123456-7", Type: "checking"}); err != nil {
		t.Fatalf("the bank account: %v", err)
	}
	rows, err := w.sql.QueryContext(ctx, `SELECT properties FROM activity_log WHERE event = ?`, investor.EventPayoutAccountAdded)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var properties string
		if err := rows.Scan(&properties); err != nil {
			t.Fatal(err)
		}
		n++
		if strings.Contains(properties, "ana.souza@example.com") || strings.Contains(properties, "123456-7") {
			t.Errorf("the entry carries the key or the number in the clear: %s", properties)
		}
	}
	if n != 2 {
		t.Fatalf("%d entries, want 2", n)
	}
}

// A closing and a transfer of the same investor, started together, leave the
// money with whoever the registration is at the end: nothing is deposited into
// the previous owner's wallet once they have been passed over.
func TestAClosingAndATransferAtTheSameTimeLeaveNoMoneyWithThePreviousOwner(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)

	var wg sync.WaitGroup
	var closeErr, transferErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, closeErr = w.svc.Close(ctx, admin(), c.ID, "2026-10-20") }()
	go func() { defer wg.Done(); _, transferErr = w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla")) }()
	wg.Wait()

	switch {
	case transferErr == nil:
		// The transfer won, or the closing finished first and left nothing
		// owed: either way the previous owner holds nothing.
		if wl, err := w.wallets.FindBySlug(ctx, admin(), "user-ana", investor.WalletSlug); err == nil && wl.Balance != 0 {
			t.Fatalf("the previous owner's wallet holds %d after the transfer (closing: %v)", wl.Balance, closeErr)
		}
	default:
		// The closing won and the money is owed to the previous owner, or the
		// transfer lost for another reason: the registration did not move.
		mine, err := w.svc.Mine(ctx, user("user-ana"))
		if err != nil || mine.UserID != "user-ana" {
			t.Fatalf("the transfer failed (%v) and the registration moved: %v, %v", transferErr, mine, err)
		}
	}
}
