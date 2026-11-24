package investor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/translation"
	br "github.com/hyz-is/arandu-br"
)

// Payer is who pays the investors, as it is printed on the income report: the
// company that owns the application. The application passes it in
// Config.Payer; an empty one prints a dash, never an invented name.
type Payer struct {
	Name     string
	Document string // CNPJ, digits or formatted
}

// reportOwner loads and authorizes the investor a report is about.
func (s *InvestorService) reportOwner(ctx context.Context, actor security.Subject, g security.Grant, investorID string) (*Investor, error) {
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
	return owner, nil
}

func formattedDocument(i Investor) string {
	if i.Kind == Company {
		return br.CNPJ(i.Document)
	}
	return br.CPF(i.Document)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// MonthlyReport writes, as .xlsx, the invoices of one investor whose period
// closed in the month, and the entries each one counted with its share of the
// costs.
func (s *InvestorService) MonthlyReport(ctx context.Context, actor security.Subject, investorID string, year, month int, w io.Writer) error {
	if year < 2000 || month < 1 || month > 12 {
		return validation.Errors{"period": {message("report_month")}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return err
	}
	owner, err := s.reportOwner(ctx, actor, g, investorID)
	if err != nil {
		return err
	}
	from := Day(fmt.Sprintf("%04d-%02d-01", year, month))
	to := Day(from.Time().AddDate(0, 1, 0).Format(DayLayout))
	invoices, err := Invoices(s.db).Where("investor_id", "=", owner.ID).
		Where("period_end", ">=", string(from)).Where("period_end", "<", string(to)).
		OrderBy("period_end").OrderBy("root_id").Get(ctx, g)
	if err != nil {
		return err
	}

	l := s.labelsFor(ctx)
	// The columns of the retention and of the carried loss are there when some
	// invoice of the month has one, so the report of an investment reads as it
	// always did.
	var withheld, carried bool
	for _, inv := range invoices {
		withheld = withheld || inv.WithholdingMonth != ""
		carried = carried || inv.CarriedIn != 0 || inv.CarriedOut != 0
	}
	header := rowOf(bold(l.T("field.period")), bold(l.T("field.gross")), bold(l.T("field.fees")), bold(l.T("field.costs")),
		bold(l.T("field.net")))
	if carried {
		header = append(header, bold(l.T("report.carried_in")), bold(l.T("report.carried_out")))
	}
	header = append(header, bold(l.T("field.share")))
	if withheld {
		header = append(header, bold(l.T("report.part")), bold(l.T("report.withheld_inss")), bold(l.T("report.withheld_irrf")))
	}
	header = append(header, bold(l.T("field.amount")), bold(l.T("field.status")))
	summary := sheet{name: l.T("report.summary_sheet"), rows: [][]cell{
		rowOf(bold(l.line("report.monthly_title", translation.Replace{"month": l.Month(month), "year": year}))),
		rowOf(text(l.T("noun.investor")), text(owner.DisplayName())),
		rowOf(text(l.T("field.document")), text(formattedDocument(*owner))),
		blankRow(),
		header,
	}}
	detail := sheet{name: l.T("report.entries_sheet"), rows: [][]cell{
		rowOf(bold(l.T("field.occurred_on")), bold(l.T("field.product")), bold(l.T("field.kind")), bold(l.T("field.description")),
			bold(l.T("report.gross")), bold(l.T("field.fee")), bold(l.T("field.fee_detail")), bold(l.T("field.cost_share")),
			bold(l.T("field.line_net"))),
	}}
	var total int64
	for _, inv := range invoices {
		line := rowOf(
			text(l.line("report.period", translation.Replace{"from": string(inv.PeriodStart), "to": string(inv.PeriodEnd.AddDays(-1))})),
			money(inv.Gross), money(inv.Fees), money(inv.Costs), money(inv.Net))
		if carried {
			line = append(line, money(inv.CarriedIn), money(inv.CarriedOut))
		}
		line = append(line, percentBps(inv.ShareBps))
		if withheld {
			line = append(line, money(inv.GrossPart()), money(inv.INSSAmount), money(inv.IRRFAmount))
		}
		summary.rows = append(summary.rows, append(line, money(inv.Amount), text(l.Status(inv.Status))))
		total += inv.Amount

		links, err := InvoiceEntries(s.db).Where("invoice_id", "=", inv.ID).Get(ctx, g)
		if err != nil {
			return err
		}
		ids := make([]any, 0, len(links))
		for _, l := range links {
			ids = append(ids, l.EntryID)
		}
		if len(ids) == 0 {
			continue
		}
		entries, err := Entries(s.db).WhereIn("id", ids).OrderBy("occurred_at").OrderBy("id").Get(ctx, g)
		if err != nil {
			return err
		}
		for _, e := range entries {
			var cost int64
			if e.Gross > 0 {
				cost = roundDiv(e.Gross*inv.CostRatioPPM, 1_000_000)
			}
			detail.rows = append(detail.rows, rowOf(
				text(string(e.OccurredOn)), text(e.ProductID), text(l.Kind(e.Kind)), text(e.Description),
				money(e.Gross), money(e.Fee), text(e.FeeDetail), money(cost), money(e.Gross-e.Fee-cost)))
		}
	}
	totalRow := rowOf(bold(l.T("report.month_total")), text(""), text(""), text(""), text(""))
	if carried {
		totalRow = append(totalRow, text(""), text(""))
	}
	totalRow = append(totalRow, text(""))
	if withheld {
		totalRow = append(totalRow, text(""), text(""), text(""))
	}
	summary.rows = append(summary.rows, blankRow(), append(totalRow, money(total)))
	return writeWorkbook(w, []sheet{summary, detail})
}

// AnnualReport writes the year's income report of one investor as .xlsx: the
// payer, the beneficiary, and the amounts credited month by month, laid out
// the way the Receita Federal's informe de rendimentos is. What a partnership
// held back at source, the INSS and the IRRF, is in its columns by the month
// it was credited; an investment holds nothing back, and the report says so.
func (s *InvestorService) AnnualReport(ctx context.Context, actor security.Subject, investorID string, year int, w io.Writer) error {
	if year < 2000 {
		return validation.Errors{"year": {message("report_year")}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return err
	}
	owner, err := s.reportOwner(ctx, actor, g, investorID)
	if err != nil {
		return err
	}
	from := Day(fmt.Sprintf("%04d-01-01", year))
	to := Day(fmt.Sprintf("%04d-01-01", year+1))
	// An invoice without retention is the income of the month its period
	// closed in; one with it, of the month it was credited and held over, which
	// is the competence the tax is paid by.
	plain, err := Invoices(s.db).Where("investor_id", "=", owner.ID).
		Where("period_end", ">=", string(from)).Where("period_end", "<", string(to)).
		Where("withholding_month", "=", "").Get(ctx, g)
	if err != nil {
		return err
	}
	retained, err := Invoices(s.db).Where("investor_id", "=", owner.ID).
		Where("withholding_month", "like", fmt.Sprintf("%04d-%%", year)).Get(ctx, g)
	if err != nil {
		return err
	}
	var credited, inss, irrf, paid [12]int64
	var total, totalINSS, totalIRRF, totalPaid int64
	var anyRetention bool
	for _, inv := range plain {
		m := inv.PeriodEnd.Time().Month() - 1
		credited[m] += inv.Amount
		paid[m] += inv.Amount
		total += inv.Amount
		totalPaid += inv.Amount
	}
	for _, inv := range retained {
		anyRetention = true
		m := Day(inv.WithholdingMonth+"-01").Time().Month() - 1
		credited[m] += inv.GrossPart()
		inss[m] += inv.INSSAmount
		irrf[m] += inv.IRRFAmount
		paid[m] += inv.Amount
		total += inv.GrossPart()
		totalINSS += inv.INSSAmount
		totalIRRF += inv.IRRFAmount
		totalPaid += inv.Amount
	}

	payerDocument := s.payer.Document
	if digits, err := br.ParseCNPJ(payerDocument); err == nil {
		payerDocument = br.CNPJ(digits)
	}
	l := s.labelsFor(ctx)
	report := sheet{name: l.T("report.annual_sheet"), rows: [][]cell{
		rowOf(bold(l.line("report.annual_title", translation.Replace{"year": year}))),
		blankRow(),
		rowOf(bold(l.T("report.payer"))),
		rowOf(text(l.T("report.payer_name")), text(dash(s.payer.Name))),
		rowOf(text(l.T("report.payer_document")), text(dash(payerDocument))),
		blankRow(),
		rowOf(bold(l.T("report.beneficiary"))),
		rowOf(text(l.T("report.beneficiary_name")), text(owner.DisplayName())),
		rowOf(text(l.T("field.document")), text(formattedDocument(*owner))),
		blankRow(),
		rowOf(bold(l.T("report.income"))),
		rowOf(bold(l.T("report.month")), bold(l.T("report.credited")), bold(l.T("report.withheld")),
			bold(l.T("report.withheld_inss")), bold(l.T("report.withheld_irrf")), bold(l.T("report.paid"))),
	}}
	for m := range credited {
		report.rows = append(report.rows, rowOf(text(l.Month(m+1)), money(credited[m]), money(inss[m]+irrf[m]),
			money(inss[m]), money(irrf[m]), money(paid[m])))
	}
	note := l.T("report.tax_note")
	if anyRetention {
		note = l.T("report.tax_note_withheld")
	}
	report.rows = append(report.rows,
		rowOf(bold(l.T("report.total")), money(total), money(totalINSS+totalIRRF), money(totalINSS), money(totalIRRF), money(totalPaid)),
		blankRow(),
		rowOf(text(l.T("report.share_note"))),
		rowOf(text(note)),
	)
	return writeWorkbook(w, []sheet{report})
}

// InvoiceRPA is the receipt of the payment to an autonomous worker (RPA) of one
// invoice that held a retention at source: the payer, the beneficiary, the
// period, the part, the INSS and the IRRF with how each came out, and what was
// paid. It answers the file's name and its .xlsx bytes.
//
// The file is kept on the Disk of Config.Files, under the invoice and the
// language it was written in, like the signed contracts: the first request
// writes it and the next ones read it, so what the beneficiary was handed is
// what the payer keeps, and a later change of name does not rewrite it. The
// investor reads their own, the admin anybody's. An invoice with no retention
// has none, and answers ErrNotFound.
func (s *InvestorService) InvoiceRPA(ctx context.Context, actor security.Subject, invoiceID string) (string, []byte, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ReportsView, Investor{})
	if err != nil {
		return "", nil, err
	}
	inv, err := Invoices(s.db).WhereKey(invoiceID).First(ctx, g)
	if err != nil {
		return "", nil, err
	}
	if inv == nil {
		return "", nil, ErrNotFound
	}
	owner, err := s.reportOwner(ctx, actor, g, inv.InvestorID)
	if err != nil {
		return "", nil, err
	}
	if inv.WithholdingMonth == "" {
		return "", nil, ErrNotFound
	}
	l := s.labelsFor(ctx)
	name := l.line("rpa.file", translation.Replace{"period": string(inv.PeriodEnd)})
	body, err := s.rpaFile(ctx, g, owner, inv, l)
	if err != nil {
		return "", nil, err
	}
	return name, body, nil
}

// rpaKey is where the RPA of an invoice is kept, in one language.
func rpaKey(invoiceID, locale string) string {
	return path.Join("investor", "rpa", invoiceID+"."+locale+".xlsx")
}

// rpaFile reads the kept RPA of the invoice, or writes it the first time.
func (s *InvestorService) rpaFile(ctx context.Context, g security.Grant, owner *Investor, inv *Invoice, l Labels) ([]byte, error) {
	key := rpaKey(inv.ID, l.Locale)
	if s.files != nil {
		kept, err := s.files.Get(ctx, g, key)
		switch {
		case err == nil:
			defer kept.Body.Close()
			return io.ReadAll(kept.Body)
		case !errors.Is(err, filesystem.ErrNotFound):
			return nil, err
		}
	}
	var contract *Contract
	if inv.ContractID != "" {
		var err error
		if contract, err = Contracts(s.db).WhereKey(inv.ContractID).First(ctx, g); err != nil {
			return nil, err
		}
	}
	var body bytes.Buffer
	if err := writeWorkbook(&body, []sheet{s.rpaSheet(owner, inv, contract, l)}); err != nil {
		return nil, err
	}
	if s.files != nil {
		if err := s.files.Put(ctx, g, key, bytes.NewReader(body.Bytes()), xlsxType); err != nil {
			return nil, err
		}
	}
	return body.Bytes(), nil
}

// xlsxType is the media type of the workbooks the package writes.
const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// rpaSheet is the RPA of one invoice, as a sheet.
func (s *InvestorService) rpaSheet(owner *Investor, inv *Invoice, contract *Contract, l Labels) sheet {
	payerDocument := s.payer.Document
	if digits, err := br.ParseCNPJ(payerDocument); err == nil {
		payerDocument = br.CNPJ(digits)
	}
	month := inv.WithholdingMonth
	year, _ := strconv.Atoi(month[:4])
	monthNumber, _ := strconv.Atoi(month[5:])
	number := ""
	if contract != nil {
		number = contract.Number
	}
	return sheet{name: l.T("rpa.sheet"), rows: [][]cell{
		rowOf(bold(l.line("rpa.title", translation.Replace{"month": l.Month(monthNumber), "year": year}))),
		blankRow(),
		rowOf(bold(l.T("report.payer"))),
		rowOf(text(l.T("report.payer_name")), text(dash(s.payer.Name))),
		rowOf(text(l.T("report.payer_document")), text(dash(payerDocument))),
		blankRow(),
		rowOf(bold(l.T("report.beneficiary"))),
		rowOf(text(l.T("report.beneficiary_name")), text(owner.DisplayName())),
		rowOf(text(l.T("field.document")), text(formattedDocument(*owner))),
		rowOf(text(l.T("field.dependents")), integer(int64(inv.Dependents))),
		blankRow(),
		rowOf(bold(l.T("rpa.service"))),
		rowOf(text(l.T("field.number")), text(dash(number))),
		rowOf(text(l.T("field.period")), text(l.line("report.period", translation.Replace{"from": string(inv.PeriodStart), "to": string(inv.PeriodEnd.AddDays(-1))}))),
		blankRow(),
		rowOf(bold(l.T("rpa.calculation")), bold(l.T("rpa.base")), bold(l.T("rpa.rate")), bold(l.T("rpa.amount"))),
		rowOf(text(l.T("report.part")), text(""), text(""), money(inv.GrossPart())),
		rowOf(text(l.T("report.withheld_inss")), money(inv.INSSBase), percentBps(inv.INSSRateBps), money(inv.INSSAmount)),
		rowOf(text(l.T("rpa.irrf_reduction")), text(""), text(""), money(inv.IRRFReduction)),
		rowOf(text(l.T("report.withheld_irrf")), money(inv.IRRFBase), percentBps(inv.IRRFRateBps), money(inv.IRRFAmount)),
		rowOf(bold(l.T("rpa.total_withheld")), text(""), text(""), money(inv.Withheld())),
		rowOf(bold(l.T("report.paid")), text(""), text(""), money(inv.Amount)),
		blankRow(),
		rowOf(text(l.line("rpa.table_note", translation.Replace{"table": inv.WithholdingTable}))),
		rowOf(text(l.T("rpa.month_note"))),
		rowOf(text(l.T("rpa.employer_note"))),
		rowOf(text(l.T("rpa.review_note"))),
	}}
}
