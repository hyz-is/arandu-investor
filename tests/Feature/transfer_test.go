package feature_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	investor "github.com/hyz-is/arandu-investor"
)

// cpfCarla is a third valid CPF, the new owner of the registrations below.
const cpfCarla = "390.533.447-05"

func carla(userID string) investor.TransferRequest {
	return investor.TransferRequest{
		UserID: userID, Document: cpfCarla, Name: "Carla Souza", Email: "carla@example.com",
		EffectiveOn: "2026-10-21", Reason: "gift to the investor's daughter",
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
		h.ToUserID != "user-carla" || h.EffectiveOn != "2026-10-21" || h.CreatedBy != "finance-1" || h.Reason == "" {
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

	other := security.Subject{ID: "finance-1", Tenant: "globex", Roles: []string{investor.AdminRole}, Verified: true}
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
	if _, err := w.svc.Transfer(ctx, financeByAction(), ana.ID, carla("user-carla")); err != nil {
		t.Fatalf("the admin action was refused: %v", err)
	}
}

func TestAnInvestorPassesMoreThanOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)

	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, carla("user-carla")); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	back := investor.TransferRequest{
		UserID: "user-ana", Document: cpfAna, Name: "Ana Souza", Email: "ana@example.com",
		EffectiveOn: "2026-10-22", Reason: "given back",
	}
	if _, err := w.svc.Transfer(ctx, admin(), ana.ID, back); err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	history, err := w.svc.Transfers(ctx, admin(), ana.ID)
	if err != nil || len(history) != 2 || history[0].FromUserID != "user-ana" || history[1].FromUserID != "user-carla" {
		t.Fatalf("the history is %d rows, %v", len(history), err)
	}
}
