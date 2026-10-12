package feature_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"
	wallet "github.com/hyz-is/arandu-wallet"

	investor "github.com/hyz-is/arandu-investor"
)

func TestAnInvoiceIsWithdrawnWholeThroughTheWallet(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 3000000, 30000)
	monthlyCost(t, w, "servidores", 600000)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}

	// The investor cannot take the money out of the ledger by hand.
	wl, err := w.wallets.FindBySlug(ctx, admin(), "user-ana", investor.WalletSlug)
	if err != nil {
		t.Fatalf("Ana's wallet: %v", err)
	}
	if _, err := w.wallets.Withdraw(ctx, user("user-ana"), wallet.WithdrawRequest{IdempotencyKey: "x", WalletID: wl.ID, Amount: "1.00"}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("the investor withdrew from the ledger directly: %v", err)
	}

	account, err := w.wallets.AddPayoutAccount(ctx, user("user-ana"), wallet.PayoutAccountRequest{
		HolderID: "user-ana", Kind: "pix", Label: "CPF", Details: wallet.Meta{"key": "52998224725", "key_type": "cpf"},
		HolderName: "Ana", HolderDocument: "52998224725",
	})
	if err != nil {
		t.Fatalf("declaring the account: %v", err)
	}

	view, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking for the invoice: %v", err)
	}
	if view.Withdrawal == nil || int64(view.Withdrawal.Amount) != invoice.Amount || !view.Pending() {
		t.Fatalf("the request is %+v; want the whole invoice, waiting", view.Withdrawal)
	}
	if _, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID); !errors.Is(err, investor.ErrNotWithdrawable) {
		t.Fatalf("a second request for a waiting invoice answered %v", err)
	}

	// The operator pays it, with the receipt.
	if _, err := w.wallets.PayWithdrawal(ctx, admin(), wallet.PaymentInput{
		RequestID: view.Withdrawal.ID, EndToEndID: "E00000000202610201200", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF pix")); err != nil {
		t.Fatalf("paying: %v", err)
	}
	views, err := w.svc.InvoiceViews(ctx, user("user-ana"), ana.ID)
	if err != nil || len(views) != 1 {
		t.Fatalf("Ana's invoices: %d, %v", len(views), err)
	}
	if !views[0].Withdrawn() || views[0].Invoice.Status != investor.InvoiceWithdrawn || views[0].Withdrawable() {
		t.Fatalf("the paid invoice reads %s, withdrawn %v", views[0].Invoice.Status, views[0].Withdrawn())
	}
	if got := balance(t, w, "user-ana"); got != 0 {
		t.Fatalf("the wallet holds %d after the invoice was paid out, want 0", got)
	}
}

func TestARejectedWithdrawalLeavesTheInvoiceToBeAskedForAgain(t *testing.T) {
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
	account, err := w.wallets.AddPayoutAccount(ctx, user("user-ana"), wallet.PayoutAccountRequest{
		HolderID: "user-ana", Kind: "pix", Label: "e-mail", Details: wallet.Meta{"key": "ana@example.com"},
		HolderName: "Ana", HolderDocument: "52998224725",
	})
	if err != nil {
		t.Fatalf("declaring the account: %v", err)
	}
	first, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if _, err := w.wallets.RejectWithdrawal(ctx, admin(), first.Withdrawal.ID, "a chave não é do titular"); err != nil {
		t.Fatalf("rejecting: %v", err)
	}
	second, err := w.svc.WithdrawInvoice(ctx, user("user-ana"), invoice.ID, account.ID)
	if err != nil {
		t.Fatalf("asking again after a rejection: %v", err)
	}
	if second.Withdrawal.ID == first.Withdrawal.ID || int64(second.Withdrawal.Amount) != 10000 {
		t.Fatalf("the second request is %s for %d", second.Withdrawal.ID, second.Withdrawal.Amount)
	}

	// Somebody else cannot ask for Ana's invoice.
	if _, err := w.svc.WithdrawInvoice(ctx, user("user-bruno"), invoice.ID, account.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else asked for Ana's invoice: %v", err)
	}
}
