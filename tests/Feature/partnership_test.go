package feature_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	investor "github.com/hyz-is/arandu-investor"
)

// cnpjAcme is a valid CNPJ, for a company that is a partner.
const cnpjAcme = "11.222.333/0001-81"

// terms builds the terms of a contract starting on 2026-09-20 and closing on
// the 20th, over the products given or everything, for a test to change.
func terms(number string, share int64, products ...string) investor.ContractRequest {
	return investor.ContractRequest{
		Number: number, SignedOn: "2026-09-15", StartsOn: "2026-09-20",
		ShareBps: share, BillingDay: 20, AllProducts: len(products) == 0, Products: products,
		FileName: "contrato.pdf", FileType: "application/pdf",
	}
}

func register(t *testing.T, w world, owner *investor.Investor, in investor.ContractRequest) *investor.Contract {
	t.Helper()
	c, err := w.svc.CreateContract(context.Background(), admin(), owner.ID, in, strings.NewReader("%PDF"))
	if err != nil {
		t.Fatalf("registering contract %s: %v", in.Number, err)
	}
	return c
}

func partnership(number string, share int64, products ...string) investor.ContractRequest {
	in := terms(number, share, products...)
	in.Kind = investor.KindPartnership
	return in
}

// workbook is the text of the first sheet of an .xlsx, enough to ask whether a
// figure is in it.
func workbook(t *testing.T, body []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the file is not an .xlsx: %v", err)
	}
	for _, f := range z.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		sheet, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(sheet)
	}
	t.Fatal("the workbook has no first sheet")
	return ""
}

// The partner is a person: the INSS of 11% and the income tax of the monthly
// table are held from their part, and the wallet receives the rest.
func TestAPartnershipWithAPersonHoldsINSSAndIRRFAndPaysTheRest(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := register(t, w, ana, partnership("P-ana", 3000))

	// R$ 20.000 of revenue and no costs: the net is R$ 20.000 and the part,
	// at 30%, R$ 6.000.
	record(t, w, "sale-1", investor.KindSale, "corujao", "2026-10-05", 2000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if invoice.Net != 2000000 || invoice.GrossPart() != 600000 {
		t.Fatalf("net %d part %d; want 2000000 and 600000", invoice.Net, invoice.GrossPart())
	}
	// 11% of R$ 6.000 is R$ 660; the tax is R$ 380,02 (see the unit tests).
	if invoice.INSSAmount != 66000 || invoice.INSSBase != 600000 || invoice.INSSRateBps != 1100 ||
		invoice.IRRFAmount != 38002 || invoice.IRRFBase != 534000 || invoice.IRRFReduction != 17975 || invoice.IRRFRateBps != 2750 {
		t.Fatalf("INSS %d over %d at %d, IRRF %d over %d at %d less %d",
			invoice.INSSAmount, invoice.INSSBase, invoice.INSSRateBps, invoice.IRRFAmount, invoice.IRRFBase, invoice.IRRFRateBps, invoice.IRRFReduction)
	}
	if invoice.Amount != 495998 || invoice.Withheld() != 104002 || invoice.Status != investor.InvoiceAvailable {
		t.Fatalf("amount %d withheld %d status %s; want 495998, 104002, available", invoice.Amount, invoice.Withheld(), invoice.Status)
	}
	if invoice.WithholdingTable != "2026-01" || invoice.WithholdingMonth == "" || len(invoice.WithholdingMonth) != 7 {
		t.Fatalf("table %q month %q", invoice.WithholdingTable, invoice.WithholdingMonth)
	}
	// The wallet holds what is paid, not what the part was.
	if got := balance(t, w, "user-ana"); got != 495998 {
		t.Fatalf("the wallet holds %d, want 495998", got)
	}
	// The base of the contract did not move by it: the net is the net.
	stored, err := w.svc.Invoices(ctx, user("user-ana"), ana.ID)
	if err != nil || len(stored) != 1 || stored[0].Net != 2000000 || stored[0].Amount != 495998 {
		t.Fatalf("the stored invoice: %+v, %v", stored, err)
	}
}

// An investment holds nothing back, whatever the amount: the figures are the
// ones every release before the kind existed produced.
func TestAnInvestmentHoldsNothingBack(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 3000)
	if c.Kind != investor.KindInvestment {
		t.Fatalf("a contract with no kind is %q, want investment", c.Kind)
	}

	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 2000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if invoice.Amount != 600000 || invoice.GrossPart() != 600000 || invoice.Withheld() != 0 ||
		invoice.WithholdingMonth != "" || invoice.WithholdingTable != "" || invoice.CarriedIn != 0 || invoice.CarriedOut != 0 {
		t.Fatalf("an investment held back: %+v", invoice)
	}
	if got := balance(t, w, "user-ana"); got != 600000 {
		t.Fatalf("the wallet holds %d, want the whole part (600000)", got)
	}
	if _, _, err := w.svc.InvoiceRPA(ctx, user("user-ana"), invoice.ID); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("an invoice that held nothing has an RPA: %v", err)
	}
}

// A partner that is a company is not an autonomous worker: a partnership holds
// nothing back from it.
func TestAPartnershipWithACompanyHoldsNothingBack(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	company, err := w.svc.Create(ctx, admin(), investor.CreateRequest{
		UserID: "user-acme", Kind: investor.Company, Document: cnpjAcme, Name: "Acme", LegalName: "Acme Ltda",
		LegalRepresentative: "Ana", Email: "acme@example.com",
	})
	if err != nil {
		t.Fatalf("registering the company: %v", err)
	}
	c := register(t, w, company, partnership("P-acme", 3000))
	record(t, w, "sale-1", investor.KindSale, "corujao", "2026-10-05", 2000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if invoice.Amount != 600000 || invoice.Withheld() != 0 || invoice.WithholdingMonth != "" {
		t.Fatalf("a company was held back from: amount %d withheld %d", invoice.Amount, invoice.Withheld())
	}
}

// Two closings of the same partner in one month are taxed as the one payment
// they add up to: the ceiling of the INSS is the month's, and the income tax
// of the second is the difference.
func TestTwoPaymentsInOneMonthShareTheCeilingAndTheTable(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	a := register(t, w, ana, partnership("P-a", 10000, "loccrm"))
	b := register(t, w, ana, partnership("P-b", 10000, "tayi"))
	record(t, w, "loccrm-1", investor.KindSale, "loccrm", "2026-10-05", 800000, 0)
	record(t, w, "tayi-1", investor.KindSale, "tayi", "2026-10-06", 200000, 0)

	first, err := w.svc.Close(ctx, admin(), a.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing the first: %v", err)
	}
	second, err := w.svc.Close(ctx, admin(), b.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing the second: %v", err)
	}
	if first.INSSAmount != 88000 || first.IRRFAmount != 104927 {
		t.Fatalf("the first holds INSS %d IRRF %d; want 88000 and 104927", first.INSSAmount, first.IRRFAmount)
	}
	if second.INSSBase != 47555 || second.INSSAmount != 5231 || second.IRRFAmount != 53561 {
		t.Fatalf("the second holds INSS %d over %d, IRRF %d; want 5231 over 47555 and 53561", second.INSSAmount, second.INSSBase, second.IRRFAmount)
	}
	// The month's total is what a single payment of R$ 10.000 holds.
	if first.INSSAmount+second.INSSAmount != 93231 || first.IRRFAmount+second.IRRFAmount != 158488 {
		t.Fatalf("the month holds INSS %d IRRF %d", first.INSSAmount+second.INSSAmount, first.IRRFAmount+second.IRRFAmount)
	}
	if second.Amount != 200000-5231-53561 {
		t.Fatalf("the second is paid %d, want %d", second.Amount, 200000-5231-53561)
	}
}

// The dependents are the investor's, and each deducts from the base.
func TestTheDependentsLowerTheIncomeTaxHeldFromAPartner(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, err := w.svc.Create(ctx, admin(), investor.CreateRequest{
		UserID: "user-ana", Kind: investor.Person, Document: cpfAna, Name: "Ana", Email: "ana@example.com", Dependents: 2,
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	if ana.Dependents != 2 {
		t.Fatalf("dependents %d", ana.Dependents)
	}
	c := register(t, w, ana, partnership("P-ana", 3000))
	record(t, w, "sale-1", investor.KindSale, "corujao", "2026-10-05", 2000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if invoice.IRRFAmount != 27575 || invoice.Dependents != 2 || invoice.IRRFBase != 496082 {
		t.Fatalf("IRRF %d over %d with %d dependents; want 27575 over 496082 with 2", invoice.IRRFAmount, invoice.IRRFBase, invoice.Dependents)
	}

	// Changed afterwards, kept when nothing is said.
	three := 3
	updated, err := w.svc.Update(ctx, admin(), ana.ID, investor.UpdateRequest{Name: "Ana", Email: "ana@example.com", Dependents: &three})
	if err != nil || updated.Dependents != 3 {
		t.Fatalf("changing the dependents: %v, %v", updated, err)
	}
	kept, err := w.svc.Update(ctx, admin(), ana.ID, investor.UpdateRequest{Name: "Ana S", Email: "ana@example.com"})
	if err != nil || kept.Dependents != 3 {
		t.Fatalf("an update that says nothing about them: %v, %v", kept, err)
	}
	tooMany := 99
	var errs validation.Errors
	if _, err := w.svc.Update(ctx, admin(), ana.ID, investor.UpdateRequest{Name: "Ana", Email: "ana@example.com", Dependents: &tooMany}); !errors.As(err, &errs) || len(errs["dependents"]) == 0 {
		t.Fatalf("99 dependents answered %v", err)
	}
}

func TestAContractRefusesAKindOrADayItDoesNotKnow(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := registerPerson(t, w, "user-ana", cpfAna)

	bad := terms("C-1", 1500)
	bad.Kind = "loan"
	var errs validation.Errors
	if _, err := w.svc.CreateContract(context.Background(), admin(), ana.ID, bad, strings.NewReader("%PDF")); !errors.As(err, &errs) || len(errs["kind"]) == 0 {
		t.Fatalf("an unknown kind answered %v", err)
	}
	bad = terms("C-2", 1500)
	bad.PayDay = 29
	errs = nil
	if _, err := w.svc.CreateContract(context.Background(), admin(), ana.ID, bad, strings.NewReader("%PDF")); !errors.As(err, &errs) || len(errs["pay_day"]) == 0 {
		t.Fatalf("a pay day of 29 answered %v", err)
	}
	ok := terms("C-3", 1500)
	ok.PayDay, ok.CarryLoss = 10, true
	c, err := w.svc.CreateContract(context.Background(), admin(), ana.ID, ok, strings.NewReader("%PDF"))
	if err != nil || c.PayDay != 10 || !bool(c.CarryLoss) || c.Kind != investor.KindInvestment {
		t.Fatalf("the terms were not kept: %+v, %v", c, err)
	}
}

// A period that lost money keeps the loss, takes it off the next two and only
// then pays, over three months.
func TestALossIsCarriedForThreeMonthsUntilItIsUsedUp(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	in := terms("C-ana", 1500)
	in.CarryLoss = true
	c := register(t, w, ana, in)
	monthlyCost(t, w, "servidores", 600000)

	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 500000, 0)
	one, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing the first month: %v", err)
	}
	if one.Net != -100000 || one.Amount != 0 || one.CarriedIn != 0 || one.CarriedOut != 100000 || one.Status != investor.InvoiceNothingDue {
		t.Fatalf("the first month: net %d amount %d in %d out %d status %s", one.Net, one.Amount, one.CarriedIn, one.CarriedOut, one.Status)
	}
	// The statement of the open period says what it starts with.
	st, err := w.svc.StatementOf(ctx, user("user-ana"), c.ID, investor.StatementFilter{})
	if err != nil || st.CarriedLoss != 100000 {
		t.Fatalf("the statement carries %d, %v; want 100000", st.CarriedLoss, err)
	}
	pr, err := w.svc.ProjectionOf(ctx, user("user-ana"), c.ID)
	if err != nil || pr.CarriedIn != 100000 {
		t.Fatalf("the projection carries %d, %v", pr.CarriedIn, err)
	}

	record(t, w, "sale-2", investor.KindSale, "loccrm", "2026-11-05", 650000, 0)
	two, err := w.svc.Close(ctx, admin(), c.ID, "2026-11-20")
	if err != nil {
		t.Fatalf("closing the second month: %v", err)
	}
	if two.Net != 50000 || two.Amount != 0 || two.CarriedIn != 100000 || two.CarriedOut != 50000 || two.Status != investor.InvoiceNothingDue {
		t.Fatalf("the second month: net %d amount %d in %d out %d status %s", two.Net, two.Amount, two.CarriedIn, two.CarriedOut, two.Status)
	}

	record(t, w, "sale-3", investor.KindSale, "loccrm", "2026-12-05", 1000000, 0)
	three, err := w.svc.Close(ctx, admin(), c.ID, "2026-12-20")
	if err != nil {
		t.Fatalf("closing the third month: %v", err)
	}
	if three.Net != 400000 || three.CarriedIn != 50000 || three.CarriedOut != 0 || three.Amount != 52500 || three.Status != investor.InvoiceAvailable {
		t.Fatalf("the third month: net %d in %d out %d amount %d status %s; want 400000, 50000, 0, 52500 (15%% of 350000)",
			three.Net, three.CarriedIn, three.CarriedOut, three.Amount, three.Status)
	}
	if got := balance(t, w, "user-ana"); got != 52500 {
		t.Fatalf("the wallet holds %d, want only what the third month paid", got)
	}
}

// A reversal of an entry changes the net of the period it falls in, and the
// loss it leaves is carried like any other.
func TestAReversalInTheOpenPeriodChangesWhatIsCarried(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	in := terms("C-ana", 1500)
	in.CarryLoss = true
	c := register(t, w, ana, in)
	monthlyCost(t, w, "servidores", 600000)

	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 500000, 0)
	if _, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20"); err != nil {
		t.Fatalf("first closing: %v", err)
	}
	sale := record(t, w, "sale-2", investor.KindSale, "loccrm", "2026-11-05", 900000, 0)
	if _, err := w.svc.ReverseEntry(ctx, admin(), sale.ID, "chargeback"); err != nil {
		t.Fatalf("reversing: %v", err)
	}
	// The reversal is dated when it is made, so the period that closes after it
	// is the one that holds both: from the first closing to 20/12, two billing
	// months of costs.
	two, err := w.svc.Close(ctx, admin(), c.ID, "2026-12-20")
	if err != nil {
		t.Fatalf("second closing: %v", err)
	}
	// The sale and its reversal cancel: the period is its costs, R$ 12.000,
	// on top of the R$ 1.000 already carried.
	if two.Gross != 0 || two.Net != -1200000 || two.CarriedIn != 100000 || two.CarriedOut != 1300000 || two.Amount != 0 {
		t.Fatalf("gross %d net %d in %d out %d amount %d; want 0, -1200000, 100000, 1300000, 0",
			two.Gross, two.Net, two.CarriedIn, two.CarriedOut, two.Amount)
	}
}

// A partnership can carry its loss too, and the retention is held only over
// what is paid.
func TestAPartnershipCarriesALossAndHoldsOnlyOverWhatIsPaid(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	in := partnership("P-ana", 10000)
	in.CarryLoss = true
	c := register(t, w, ana, in)
	monthlyCost(t, w, "servidores", 600000)

	record(t, w, "sale-1", investor.KindSale, "corujao", "2026-10-05", 400000, 0)
	one, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing the first month: %v", err)
	}
	if one.Amount != 0 || one.Withheld() != 0 || one.WithholdingMonth != "" || one.CarriedOut != 200000 {
		t.Fatalf("a losing period held back: amount %d withheld %d out %d", one.Amount, one.Withheld(), one.CarriedOut)
	}
	record(t, w, "sale-2", investor.KindSale, "corujao", "2026-11-05", 1400000, 0)
	two, err := w.svc.Close(ctx, admin(), c.ID, "2026-11-20")
	if err != nil {
		t.Fatalf("closing the second month: %v", err)
	}
	// Net R$ 8.000 less the R$ 2.000 carried is R$ 6.000 at 100%.
	if two.Net != 800000 || two.GrossPart() != 600000 || two.INSSAmount != 66000 || two.IRRFAmount != 38002 || two.Amount != 495998 || two.CarriedOut != 0 {
		t.Fatalf("net %d part %d INSS %d IRRF %d amount %d out %d", two.Net, two.GrossPart(), two.INSSAmount, two.IRRFAmount, two.Amount, two.CarriedOut)
	}
}

// The RPA of a retention is a file of the invoice: written on the first request
// and the same on the next, for its owner and the admin, and refused to anybody
// else.
func TestTheRPAOfARetentionIsKeptAndReadByItsOwnerAndTheAdmin(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	registerPerson(t, w, "user-bruno", cpfBruno)
	c := register(t, w, ana, partnership("P-ana", 3000))
	record(t, w, "sale-1", investor.KindSale, "corujao", "2026-10-05", 2000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}

	name, body, err := w.svc.InvoiceRPA(ctx, user("user-ana"), invoice.ID)
	if err != nil {
		t.Fatalf("the investor's RPA: %v", err)
	}
	if name != "rpa-2026-10-20.xlsx" {
		t.Fatalf("the file is called %q", name)
	}
	sheet := workbook(t, body)
	for _, want := range []string{"6000.00", "660.00", "380.02", "4959.98", "179.75", "2026-01", "529.982.247-25"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the RPA does not carry %s", want)
		}
	}
	_, again, err := w.svc.InvoiceRPA(ctx, admin(), invoice.ID)
	if err != nil || !bytes.Equal(body, again) {
		t.Fatalf("the admin reads another file (%v)", err)
	}
	if _, _, err := w.svc.InvoiceRPA(ctx, user("user-bruno"), invoice.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else read the RPA: %v", err)
	}
	if _, _, err := w.svc.InvoiceRPA(ctx, admin(), "no-such-invoice"); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("an unknown invoice: %v", err)
	}
}

// The reports show the retention: the monthly one with its columns when there
// is one, the yearly income report by the month it was credited.
func TestTheReportsShowTheRetention(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := register(t, w, ana, partnership("P-ana", 3000))
	record(t, w, "sale-1", investor.KindSale, "corujao", "2026-10-05", 2000000, 0)
	invoice, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing: %v", err)
	}

	var monthly bytes.Buffer
	if err := w.svc.MonthlyReport(ctx, user("user-ana"), ana.ID, 2026, 10, &monthly); err != nil {
		t.Fatalf("monthly report: %v", err)
	}
	for _, want := range []string{"INSS withheld", "IRRF withheld", "660.00", "380.02", "4959.98"} {
		if !strings.Contains(workbook(t, monthly.Bytes()), want) {
			t.Errorf("the monthly report does not carry %q", want)
		}
	}

	year := invoice.WithholdingMonth[:4]
	var annual bytes.Buffer
	yearNumber := 0
	for _, r := range year {
		yearNumber = yearNumber*10 + int(r-'0')
	}
	if err := w.svc.AnnualReport(ctx, user("user-ana"), ana.ID, yearNumber, &annual); err != nil {
		t.Fatalf("annual report: %v", err)
	}
	sheet := workbook(t, annual.Bytes())
	for _, want := range []string{"6000.00", "660.00", "380.02", "1040.02", "4959.98", "withheld at source by the payer"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the income report does not carry %q", want)
		}
	}
}

// An investment's reports read as they always did.
func TestTheReportsOfAnInvestmentAreUnchanged(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	c := contractOver(t, w, ana, 3000)
	record(t, w, "sale-1", investor.KindSale, "loccrm", "2026-10-05", 2000000, 0)
	if _, err := w.svc.Close(ctx, admin(), c.ID, "2026-10-20"); err != nil {
		t.Fatalf("closing: %v", err)
	}
	var monthly bytes.Buffer
	if err := w.svc.MonthlyReport(ctx, user("user-ana"), ana.ID, 2026, 10, &monthly); err != nil {
		t.Fatal(err)
	}
	if sheet := workbook(t, monthly.Bytes()); strings.Contains(sheet, "INSS") || !strings.Contains(sheet, "6000.00") {
		t.Fatalf("the monthly report of an investment changed: %s", sheet)
	}
}

// The day a contract pays its partner speaks first in the summary; a contract
// that sets none leaves it to the application.
func TestTheNextPayoutFollowsTheContractsPayDay(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	register(t, w, ana, terms("C-none", 1000))

	none, err := w.svc.NextPayoutOf(ctx, user("user-ana"), "2026-11-24")
	if err != nil || none != "" {
		t.Fatalf("a contract with no pay day answered %q, %v", none, err)
	}
	early := terms("C-10", 1000)
	early.PayDay = 10
	register(t, w, ana, early)
	late := terms("C-27", 1000)
	late.PayDay = 27
	register(t, w, ana, late)
	next, err := w.svc.NextPayoutOf(ctx, user("user-ana"), "2026-11-24")
	if err != nil || next != "2026-11-27" {
		t.Fatalf("the earliest pay day from the 24th is %q, %v; want 2026-11-27", next, err)
	}
	next, err = w.svc.NextPayoutOf(ctx, user("user-ana"), "2026-11-28")
	if err != nil || next != "2026-12-10" {
		t.Fatalf("from the 28th: %q, %v; want 2026-12-10", next, err)
	}
	if _, err := w.svc.NextPayoutOf(ctx, user("user-bruno"), "2026-11-24"); !errors.Is(err, investor.ErrNotFound) {
		t.Fatalf("somebody who is no investor: %v", err)
	}
}

// A product marked as separate is not in the contracts, nor in the costs, that
// cover the whole application; naming it covers it again.
func TestASeparateProductStaysOutOfTheWholeApplication(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := registerPerson(t, w, "user-ana", cpfAna)
	bia := registerPerson(t, w, "user-bia", cpfBruno)
	everything := register(t, w, ana, terms("C-all", 1000))
	corujao := register(t, w, bia, partnership("C-corujao", 5000, "corujao"))

	record(t, w, "loccrm-1", investor.KindSale, "loccrm", "2026-10-05", 2000000, 0)
	record(t, w, "corujao-1", investor.KindSale, "corujao", "2026-10-06", 1000000, 0)
	monthlyCost(t, w, "servidores", 600000)
	monthlyCost(t, w, "ferramentas do corujao", 300000, "corujao")

	if err := w.svc.SeparateProduct(ctx, user("user-ana"), "corujao"); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("an investor separated a product: %v", err)
	}
	if err := w.svc.SeparateProduct(ctx, admin(), "corujao"); err != nil {
		t.Fatalf("separating: %v", err)
	}
	if err := w.svc.SeparateProduct(ctx, admin(), "corujao"); err != nil {
		t.Fatalf("separating twice: %v", err)
	}
	if list, err := w.svc.SeparateProducts(ctx, admin()); err != nil || len(list) != 1 || list[0] != "corujao" {
		t.Fatalf("the separate products: %v, %v", list, err)
	}

	all, err := w.svc.Close(ctx, admin(), everything.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing the whole application: %v", err)
	}
	// Only the other line's revenue, and only the shared cost: R$ 20.000 less
	// R$ 6.000, at 10%.
	if all.Gross != 2000000 || all.Costs != 600000 || all.Net != 1400000 || all.Amount != 140000 {
		t.Fatalf("the whole application: gross %d costs %d net %d amount %d; want 2000000, 600000, 1400000, 140000", all.Gross, all.Costs, all.Net, all.Amount)
	}
	// The separate line has its own revenue and its own cost, and none of the
	// shared one.
	own, err := w.svc.Close(ctx, admin(), corujao.ID, "2026-10-20")
	if err != nil {
		t.Fatalf("closing the separate line: %v", err)
	}
	if own.Gross != 1000000 || own.Costs != 300000 || own.Net != 700000 || own.GrossPart() != 350000 {
		t.Fatalf("the separate line: gross %d costs %d net %d part %d; want 1000000, 300000, 700000, 350000", own.Gross, own.Costs, own.Net, own.GrossPart())
	}

	// Brought back, the next closing of the whole application takes it in.
	if err := w.svc.JoinProduct(ctx, admin(), "corujao"); err != nil {
		t.Fatalf("bringing it back: %v", err)
	}
	if list, _ := w.svc.SeparateProducts(ctx, admin()); len(list) != 0 {
		t.Fatalf("still separate: %v", list)
	}
	if err := w.svc.JoinProduct(ctx, admin(), "never-separate"); err != nil {
		t.Fatalf("bringing back one that was not apart: %v", err)
	}
}
