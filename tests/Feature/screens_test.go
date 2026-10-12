package feature_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	wallet "github.com/hyz-is/arandu-wallet"

	investor "github.com/hyz-is/arandu-investor"
)

func TestTheOperatorPaysAnInvestorsWithdrawalFromTheQueue(t *testing.T) {
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

	// The investor declares a PIX key, which takes the name and the document
	// of the registration whatever the form says.
	account, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", " ana@example.com ")
	if err != nil {
		t.Fatalf("declaring the PIX key: %v", err)
	}
	if account.HolderDocument != ana.Document || account.HolderName != ana.Name || account.Details["key"] != "ana@example.com" {
		t.Fatalf("the account is %+v; want it in Ana's name and document", account)
	}
	accounts, err := w.svc.PayoutAccounts(ctx, user("user-ana"), ana.ID)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("Ana's accounts: %d, %v", len(accounts), err)
	}
	if _, err := w.svc.PayoutAccounts(ctx, user("user-bruno"), ana.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else read Ana's accounts: %v", err)
	}

	view, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking for the invoice: %v", err)
	}

	queue, err := w.svc.Withdrawals(ctx, admin(), wallet.WithdrawalRequested)
	if err != nil || len(queue) != 1 {
		t.Fatalf("the queue holds %d, %v; want Ana's request", len(queue), err)
	}
	if queue[0].Invoice.ID != invoice.ID || queue[0].Investor.ID != ana.ID {
		t.Fatalf("the queue row is %+v", queue[0])
	}
	if _, err := w.svc.Withdrawals(ctx, user("user-ana"), ""); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("the investor read the operator's queue: %v", err)
	}
	if _, err := w.svc.StartPaying(ctx, user("user-ana"), view.Withdrawal.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("the investor took their own withdrawal to pay: %v", err)
	}

	if _, err := w.svc.StartPaying(ctx, admin(), view.Withdrawal.ID); err != nil {
		t.Fatalf("taking the withdrawal: %v", err)
	}
	paid, err := w.svc.PayInvestorWithdrawal(ctx, admin(), wallet.PaymentInput{
		RequestID: view.Withdrawal.ID, EndToEndID: "E00000000202610201200", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF pix"))
	if err != nil || paid.Status != wallet.WithdrawalPaid {
		t.Fatalf("paying: %v, %v", paid, err)
	}

	// Both the investor and the admin open the receipt; nobody else does.
	for _, who := range []security.Subject{user("user-ana"), admin()} {
		req, file, err := w.svc.InvoiceReceipt(ctx, who, invoice.ID)
		if err != nil {
			t.Fatalf("%s opening the receipt: %v", who.ID, err)
		}
		body, _ := io.ReadAll(file.Body)
		file.Body.Close()
		if string(body) != "%PDF pix" || req.ReceiptName != "pix.pdf" {
			t.Fatalf("%s read %q named %q", who.ID, body, req.ReceiptName)
		}
	}
	if _, _, err := w.svc.InvoiceReceipt(ctx, user("user-bruno"), invoice.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else opened Ana's receipt: %v", err)
	}
}

func TestTheOperatorRejectsAWithdrawalWithAReason(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1000)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 100000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	account, err := w.svc.AddPixAccount(ctx, user("user-ana"), "cpf", cpfAna)
	if err != nil {
		t.Fatalf("declaring the PIX key: %v", err)
	}
	view, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	rejected, err := w.svc.RejectInvestorWithdrawal(ctx, admin(), view.Withdrawal.ID, "a chave não é do titular")
	if err != nil || rejected.Status != wallet.WithdrawalRejected {
		t.Fatalf("rejecting: %v, %v", rejected, err)
	}
	views, err := w.svc.InvoiceViews(ctx, user("user-ana"), ana.ID)
	if err != nil || len(views) != 1 || !views[0].Withdrawable() || views[0].Withdrawal.RejectedReason != "a chave não é do titular" {
		t.Fatalf("after the rejection the invoice reads %+v, %v", views, err)
	}
	if _, _, err := w.svc.InvoiceReceipt(ctx, user("user-ana"), invoice.ID); err == nil {
		t.Fatal("a rejected withdrawal opened a receipt")
	}
}

func TestAPixKeyIsRefusedWhenItIsNotOneOfTheKinds(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	registerPerson(t, w, "user-ana", cpfAna)

	var errs validation.Errors
	if _, err := w.svc.AddPixAccount(ctx, user("user-ana"), "iban", "x"); !errors.As(err, &errs) || len(errs["pix_key_type"]) == 0 {
		t.Fatalf("an unknown key type answered %v", err)
	}
	if _, err := w.svc.AddPixAccount(ctx, user("user-ana"), "email", "  "); !errors.As(err, &errs) || len(errs["pix_key"]) == 0 {
		t.Fatalf("an empty key answered %v", err)
	}
	if _, err := w.svc.AddPixAccount(ctx, user("user-bruno"), "email", "b@example.com"); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("somebody who is not an investor declared a key: %v", err)
	}
}

func TestTheBooksArePagedSearchedAndOrderedOnTheServer(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-01", 1000, 0)
	record(t, w, "sale-2", investor.KindSale, "loccrm", "2026-10-02", 3000, 0)
	record(t, w, "renew-1", investor.KindRenewal, "placaflow", "2026-10-03", 2000, 0)

	rows, total, err := w.svc.EntriesPage(ctx, admin(), investor.EntriesQuery{Sort: "gross", Dir: "asc", PerPage: 2, Page: 1})
	if err != nil || total != 3 || len(rows) != 2 || rows[0].Gross != 1000 || rows[1].Gross != 2000 {
		t.Fatalf("page one by gross is %v of %d, %v", grossOf(rows), total, err)
	}
	rows, _, err = w.svc.EntriesPage(ctx, admin(), investor.EntriesQuery{Sort: "gross", Dir: "asc", PerPage: 2, Page: 2})
	if err != nil || len(rows) != 1 || rows[0].Gross != 3000 {
		t.Fatalf("page two by gross is %v, %v", grossOf(rows), err)
	}
	rows, total, err = w.svc.EntriesPage(ctx, admin(), investor.EntriesQuery{Search: "placa"})
	if err != nil || total != 1 || rows[0].ExternalRef != "renew-1" {
		t.Fatalf("searching the product found %d, %v", total, err)
	}
	rows, total, err = w.svc.EntriesPage(ctx, admin(), investor.EntriesQuery{Kind: investor.KindSale})
	if err != nil || total != 2 || rows[0].ExternalRef != "sale-2" {
		t.Fatalf("filtering sales found %d, newest %v, %v", total, rows, err)
	}

	var errs validation.Errors
	if _, _, err := w.svc.EntriesPage(ctx, admin(), investor.EntriesQuery{Sort: "tenant_id"}); !errors.As(err, &errs) {
		t.Fatalf("ordering by a column outside the allowlist answered %v", err)
	}
	if _, _, err := w.svc.EntriesPage(ctx, user("user-ana"), investor.EntriesQuery{}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody who is not the admin read the books: %v", err)
	}
	if _, err := w.svc.AdminCosts(ctx, user("user-ana")); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody who is not the admin read the admin's costs: %v", err)
	}
	if _, err := w.svc.AdminCosts(ctx, admin()); err != nil {
		t.Fatalf("the admin reading the costs: %v", err)
	}
}

func grossOf(rows []*investor.Entry) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Gross)
	}
	return out
}
