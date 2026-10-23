package feature_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	hview "github.com/arandu-io/hesape/view"
	wallet "github.com/hyz-is/arandu-wallet"

	investor "github.com/hyz-is/arandu-investor"
)

// financeByAction is the finance team as an application whose permissions
// arandu-permission administers produces it: the admin held as an action, and
// arandu-wallet's own role, which the wallet still asks for.
func financeByAction() security.Subject {
	return security.Subject{ID: "finance-2", Tenant: tenant, Roles: []string{wallet.OperatorRole},
		Actions: []security.Action{investor.AdminAction}, Verified: true}
}

func TestTheAdminActionOpensWhatTheAdminRoleOpens(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 1000000, 0)

	if !investor.IsAdmin(financeByAction()) || investor.IsAdmin(user("user-ana")) {
		t.Fatal("IsAdmin does not tell the admin action from an investor")
	}
	invoice, err := w.svc.Close(ctx, financeByAction(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing with the admin action: %v", err)
	}
	if _, err := w.svc.Find(ctx, financeByAction(), ana.ID); err != nil {
		t.Fatalf("reading the investor with the admin action: %v", err)
	}
	if invoice.Amount <= 0 {
		t.Fatalf("the invoice is %d", invoice.Amount)
	}
}

func TestAnInvestorDeclaresABankAccountAndIsPaidIntoIt(t *testing.T) {
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

	var refused validation.Errors
	if _, err := w.svc.AddBankAccount(ctx, user("user-ana"), investor.BankAccountRequest{Bank: "341"}); !errors.As(err, &refused) {
		t.Fatalf("an incomplete bank account was taken: %v", err)
	}
	if _, err := w.svc.AddBankAccount(ctx, user("user-bruno"), investor.BankAccountRequest{
		Bank: "341", Branch: "0001", Number: "12345-6", Type: "checking",
	}); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("somebody who is no investor declared an account: %v", err)
	}
	account, err := w.svc.AddBankAccount(ctx, user("user-ana"), investor.BankAccountRequest{
		Bank: "341 Itaú", Branch: "0001", Number: "12345-6", Type: "checking",
	})
	if err != nil {
		t.Fatalf("declaring the bank account: %v", err)
	}
	if account.Kind != investor.PayoutBank || account.HolderDocument != ana.Document || account.Details["account_number"] != "12345-6" {
		t.Fatalf("the account is %+v", account)
	}

	view, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking for the invoice: %v", err)
	}
	if _, err := w.svc.StartPaying(ctx, financeByAction(), view.Withdrawal.ID); err != nil {
		t.Fatalf("taking it: %v", err)
	}
	if _, err := w.svc.PayInvestorWithdrawal(ctx, financeByAction(), wallet.PaymentInput{
		RequestID: view.Withdrawal.ID, EndToEndID: "TED-1", ReceiptName: "ted.png", ReceiptType: "image/png",
	}, strings.NewReader("png")); err != nil {
		t.Fatalf("paying it: %v", err)
	}
	_, file, err := w.svc.InvoiceReceipt(ctx, user("user-ana"), invoice.ID)
	if err != nil {
		t.Fatalf("the investor opening the receipt: %v", err)
	}
	body, _ := io.ReadAll(file.Body)
	file.Body.Close()
	if string(body) != "png" {
		t.Fatalf("the receipt reads %q", body)
	}

	// Every act is in the activity log, in the tenant, under who did it.
	want := map[string]string{
		investor.EventPeriodClosed:        "finance-1",
		investor.EventPayoutAccountAdded:  "user-ana",
		investor.EventWithdrawalRequested: "user-ana",
		investor.EventWithdrawalTaken:     "finance-2",
		investor.EventWithdrawalPaid:      "finance-2",
	}
	for event, causer := range want {
		var n int
		if err := w.sql.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM activity_log WHERE tenant_id = ? AND log_name = ? AND event = ? AND causer_id = ?`,
			tenant, investor.ActivityLogName, event, causer).Scan(&n); err != nil {
			t.Fatalf("reading the activity log: %v", err)
		}
		if n == 0 {
			t.Errorf("no %s entry by %s in the activity log", event, causer)
		}
	}
}

func TestThePortuguesePathsServeTheRoutesInPortuguese(t *testing.T) {
	t.Parallel()

	m := mount(t, investor.Config{Prefix: "/painel/investidor", AdminPrefix: "/admin/investidores", Paths: investor.PortuguesePaths})
	u := investor.NewURLs("/painel/investidor", "/admin/investidores", investor.PortuguesePaths)
	if got := u.Me(); got != "/painel/investidor" {
		t.Fatalf("Me is %q", got)
	}
	if got := u.Withdraw("f1"); got != "/painel/investidor/faturas/f1/saque" {
		t.Fatalf("Withdraw is %q", got)
	}
	if got := u.Receipt("f1"); got != "/painel/investidor/faturas/f1/comprovante" {
		t.Fatalf("Receipt is %q", got)
	}
	if got := u.PayoutAccounts(); got != "/painel/investidor/conta-de-recebimento" {
		t.Fatalf("PayoutAccounts is %q", got)
	}
	if got := u.Pay("w1"); got != "/admin/investidores/saques/w1/pagamento" {
		t.Fatalf("Pay is %q", got)
	}

	cookies := m.signIn(t, administrator())
	for _, target := range []string{
		"/admin/investidores/saques",
		"/admin/investidores/investidores",
	} {
		rec := m.answer(t, http.MethodGet, target, "", cookies...)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s answered %d, want the policy's %d", target, rec.Code, http.StatusForbidden)
		}
	}
	for _, target := range []string{"/painel/investidor/me", "/investor/admin/withdrawals", "/painel/investidor/admin/withdrawals"} {
		rec := m.answer(t, http.MethodGet, target, "", cookies...)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s answered %d, want %d: the English address is not mounted", target, rec.Code, http.StatusNotFound)
		}
	}
}

func TestTheAdminMiddlewareRunsOnTheAdminRoutesOnly(t *testing.T) {
	t.Parallel()

	var ran []string
	var mu sync.Mutex
	gate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			ran = append(ran, r.URL.Path)
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
	m := mount(t, investor.Config{AdminMiddleware: []func(http.Handler) http.Handler{gate}})
	cookies := m.signIn(t, administrator())
	if rec := m.answer(t, http.MethodGet, "/investor/admin/withdrawals", "", cookies...); rec.Code != http.StatusForbidden {
		t.Errorf("the admin's route answered %d, want the policy's 403", rec.Code)
	}
	// The investor's route reaches the database this router has none of; what
	// is asked here is only whether the gate ran on the way.
	func() {
		defer func() { _ = recover() }()
		req := httptest.NewRequest(http.MethodGet, "/investor/me", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		m.router.ServeHTTP(httptest.NewRecorder(), req)
	}()
	// A visitor with no session is sent to sign in before the gate.
	if rec := m.answer(t, http.MethodGet, "/investor/admin/withdrawals", ""); rec.Code != http.StatusSeeOther {
		t.Errorf("a visitor was answered %d", rec.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "/investor/admin/withdrawals" {
		t.Errorf("the admin middleware ran on %v, want the admin's route once", ran)
	}
}

func TestAConfigurationWhosePathsCannotBeRoutesIsRefused(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]investor.Config{
		"wildcard":        {Paths: investor.Paths{Invoices: "/{x}"}},
		"no leading /":    {Paths: investor.Paths{Invoices: "faturas"}},
		"root not Me":     {Paths: investor.Paths{Costs: "/"}},
		"trailing /":      {Paths: investor.Paths{Costs: "/custos/"}},
		"admin prefix":    {AdminPrefix: "admin"},
		"colliding paths": {Paths: investor.Paths{Statement: "/projection"}},
		"nil middleware":  {Middleware: []func(http.Handler) http.Handler{nil}},
	} {
		cfg.Files, cfg.Wallet = testDisk(t), &wallet.WalletService{}
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: the configuration was accepted", name)
		}
	}
}

func TestAPageNeverWritesItsTokenOut(t *testing.T) {
	t.Parallel()
	page := investor.DashboardPageData{Screen: investor.Screen{Page: hview.Page{Title: "Painel", Token: "csrf-secret"}}}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(body), "csrf-secret") || !strings.Contains(string(body), "Painel") {
		t.Fatalf("the page marshals as %s", body)
	}
	if strings.Contains(page.Screen.LogValue().String(), "csrf-secret") {
		t.Fatal("the page logs its token")
	}
}
