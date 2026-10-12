package feature_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/validation"

	investor "github.com/hyz-is/arandu-investor"
)

// day is noon of a calendar day in Sao Paulo, far from any midnight.
func day(s string) time.Time {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	t, _ := time.ParseInLocation("2006-01-02 15:04", s+" 12:00", loc)
	return t
}

// contractOver registers a contract starting on 2026-09-20, closing on the
// 20th, with the share given, over the products given or everything.
func contractOver(t *testing.T, w world, owner *investor.Investor, share int64, products ...string) *investor.Contract {
	t.Helper()
	terms := investor.ContractRequest{
		Number: "C-" + owner.UserID, SignedOn: "2026-09-15", StartsOn: "2026-09-20",
		ShareBps: share, BillingDay: 20, AllProducts: len(products) == 0, Products: products,
		FileName: "contrato.pdf", FileType: "application/pdf",
	}
	c, err := w.svc.CreateContract(context.Background(), admin(), owner.ID, terms, strings.NewReader("%PDF"))
	if err != nil {
		t.Fatalf("registering the contract: %v", err)
	}
	return c
}

func record(t *testing.T, w world, ref string, kind investor.EntryKind, product, on string, gross, fee int64) *investor.Entry {
	t.Helper()
	e, err := w.svc.RecordEntry(context.Background(), admin(), investor.EntryRequest{
		ExternalRef: ref, Kind: kind, ProductID: product, OccurredAt: day(on),
		Gross: gross, Fee: fee, FeeDetail: "gateway", Description: ref,
	})
	if err != nil {
		t.Fatalf("recording %s: %v", ref, err)
	}
	return e
}

func monthlyCost(t *testing.T, w world, name string, amount int64, products ...string) {
	t.Helper()
	_, err := w.svc.CreateCost(context.Background(), admin(), investor.CostRequest{
		Name: name, Category: "infra", Amount: amount, Recurrence: investor.Monthly,
		StartsOn: "2026-01-01", AllProducts: len(products) == 0, Products: products,
	})
	if err != nil {
		t.Fatalf("declaring %s: %v", name, err)
	}
}

func balance(t *testing.T, w world, userID string) int64 {
	t.Helper()
	wl, err := w.wallets.FindBySlug(context.Background(), admin(), userID, investor.WalletSlug)
	if err != nil {
		t.Fatalf("reading %s's wallet: %v", userID, err)
	}
	return int64(wl.Balance)
}

func TestClosingThePeriodPaysTheInvestorTheirShareOfTheNet(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)

	// The owner's example: R$ 30.000 of sales with R$ 300 of gateway fees, and
	// R$ 6.000 a month of costs, in the period from 20/09 to 20/10.
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 3000000, 30000)
	monthlyCost(t, w, "servidores", 600000)

	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if invoice.PeriodStart != "2026-09-20" || invoice.PeriodEnd != "2026-10-20" {
		t.Fatalf("the period is %s to %s, want 2026-09-20 to 2026-10-20", invoice.PeriodStart, invoice.PeriodEnd)
	}
	if invoice.Gross != 3000000 || invoice.Fees != 30000 || invoice.Costs != 600000 || invoice.Net != 2370000 {
		t.Fatalf("gross %d fees %d costs %d net %d", invoice.Gross, invoice.Fees, invoice.Costs, invoice.Net)
	}
	if invoice.Amount != 355500 || invoice.Status != investor.InvoiceAvailable || invoice.CostRatioPPM != 200000 {
		t.Fatalf("amount %d status %s ratio %d", invoice.Amount, invoice.Status, invoice.CostRatioPPM)
	}
	if got := balance(t, w, "user-ana"); got != 355500 {
		t.Fatalf("Ana's wallet holds %d centavos, want 355500", got)
	}

	// Closing the same period again is the same invoice, and no second deposit.
	again, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-25")
	if err != nil || again.ID != invoice.ID {
		t.Fatalf("closing again answered %v, %v; want the same invoice", again, err)
	}
	if got := balance(t, w, "user-ana"); got != 355500 {
		t.Fatalf("closing twice left %d in the wallet, want one deposit", got)
	}
}

func TestALateEntryIsCountedInTheNextClosingAndTheWaitingInvoiceIsOverdue(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1000)

	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 100000, 0)
	first, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil || first.Amount != 10000 {
		t.Fatalf("first closing: %v, %v", first, err)
	}

	// A sale dated 10/10 arrives after the period closed; another lands in the
	// next period.
	record(t, w, "sale-late", investor.KindSale, "loccrm", "2026-10-10", 50000, 0)
	record(t, w, "sale-2", investor.KindSale, "loccrm", "2026-11-03", 200000, 0)

	second, err := w.svc.Close(ctx, admin(), c.ID, "2026-11-20")
	if err != nil {
		t.Fatalf("second closing: %v", err)
	}
	if second.PeriodStart != "2026-10-20" || second.Gross != 250000 || second.Amount != 25000 {
		t.Fatalf("second invoice: start %s gross %d amount %d; want 2026-10-20, 250000 (the late sale too), 25000",
			second.PeriodStart, second.Gross, second.Amount)
	}

	// The first invoice was never summed into the second: it is still its own,
	// now overdue.
	invoices, err := w.svc.Invoices(ctx, user("user-ana"), ana.ID)
	if err != nil || len(invoices) != 2 {
		t.Fatalf("Ana's invoices: %d, %v", len(invoices), err)
	}
	if invoices[1].ID != first.ID || invoices[1].Status != investor.InvoiceOverdue || invoices[1].Amount != 10000 {
		t.Fatalf("the first invoice is %s %s %d; want overdue with its own 10000", invoices[1].ID, invoices[1].Status, invoices[1].Amount)
	}
	if got := balance(t, w, "user-ana"); got != 35000 {
		t.Fatalf("the wallet holds %d, want both invoices (35000)", got)
	}
}

func TestAPeriodThatLostMoneyPaysNothingAndCarriesNothing(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1500)
	monthlyCost(t, w, "servidores", 600000)

	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 500000, 0)
	lost, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if lost.Net != -100000 || lost.Amount != 0 || lost.Status != investor.InvoiceNothingDue || lost.WalletOperationID != "" {
		t.Fatalf("a losing period: net %d amount %d status %s deposit %q", lost.Net, lost.Amount, lost.Status, lost.WalletOperationID)
	}

	// The next period is not charged the loss of this one.
	record(t, w, "sale-2", investor.KindSale, "loccrm", "2026-11-05", 1600000, 0)
	next, err := w.svc.Close(ctx, admin(), c.ID, "2026-11-20")
	if err != nil {
		t.Fatalf("closing the next period: %v", err)
	}
	if next.Net != 1000000 || next.Amount != 150000 {
		t.Fatalf("the next period: net %d amount %d; want 1000000 and 150000, with no loss carried", next.Net, next.Amount)
	}
}

func TestAContractOverSomeProductsBearsItsPartOfTheCosts(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 1000, "loccrm")

	record(t, w, "loccrm-1", investor.KindSale, "loccrm", "2026-10-05", 2000000, 0)
	record(t, w, "tayi-1", investor.KindSale, "tayi", "2026-10-06", 1000000, 0)
	monthlyCost(t, w, "servidores", 600000)
	monthlyCost(t, w, "clusters gpu", 300000, "tayi")

	invoice, err := w.svc.Close(context.Background(), admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	// LocCRM's revenue only; two thirds of the general cost; none of Tayi's.
	if invoice.Gross != 2000000 || invoice.Costs != 400000 || invoice.Amount != 160000 {
		t.Fatalf("gross %d costs %d amount %d; want 2000000, 400000, 160000", invoice.Gross, invoice.Costs, invoice.Amount)
	}
}

func TestEntriesAreRecordedOnceAndReversedNotEdited(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()

	first := record(t, w, "order-10293", investor.KindSale, "loccrm", "2026-10-05", 19900, 798)
	second := record(t, w, "order-10293", investor.KindSale, "loccrm", "2026-10-05", 99999, 0)
	if second.ID != first.ID || second.Gross != 19900 || first.Net != 19102 {
		t.Fatalf("the same event twice made %q and %q (gross %d, net %d)", first.ID, second.ID, second.Gross, first.Net)
	}

	reversal, err := w.svc.ReverseEntry(ctx, admin(), first.ID, "cobrança duplicada")
	if err != nil {
		t.Fatalf("reversing: %v", err)
	}
	if reversal.Gross != -19900 || reversal.Fee != -798 || reversal.ReversesID != first.ID {
		t.Fatalf("the reversal carries gross %d fee %d of %q", reversal.Gross, reversal.Fee, reversal.ReversesID)
	}
	again, err := w.svc.ReverseEntry(ctx, admin(), first.ID, "")
	if err != nil || again.ID != reversal.ID {
		t.Fatalf("reversing twice answered %v, %v; want the first reversal", again, err)
	}

	_, err = w.svc.RecordEntry(ctx, admin(), investor.EntryRequest{
		ExternalRef: "x", Kind: investor.KindSale, ProductID: "loccrm", OccurredAt: day("2026-10-05"), Gross: -1,
	})
	var errs validation.Errors
	if !errors.As(err, &errs) || !errs.Has("gross") {
		t.Fatalf("a sale with negative gross answered %v", err)
	}
}

func TestASuspendedInvestorTakesPartInNoClosingAndDueContractsCloseTogether(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	bruno := registerPerson(t, w, "user-bruno", cpfBruno)
	contractOver(t, w, ana, 1000)
	cb := contractOver(t, w, bruno, 2000)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 100000, 0)

	if _, err := w.svc.SetActive(ctx, admin(), bruno.ID, false); err != nil {
		t.Fatalf("suspending Bruno: %v", err)
	}
	if _, err := w.svc.Close(ctx, admin(), cb.ID, "2026-10-20"); err == nil {
		t.Fatal("a suspended investor's contract was closed")
	}

	closed, failed, err := w.svc.CloseDue(ctx, admin(), "2026-10-20")
	if err != nil {
		t.Fatalf("closing the contracts due: %v", err)
	}
	if len(closed) != 1 || closed[0].InvestorID != ana.ID || closed[0].Amount != 10000 || len(failed) != 1 {
		t.Fatalf("closed %d (want Ana's, 10000), failed %v (want Bruno's)", len(closed), failed)
	}
}
