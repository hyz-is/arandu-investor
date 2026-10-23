package investor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
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
	summary := sheet{name: l.T("report.summary_sheet"), rows: [][]cell{
		rowOf(bold(l.line("report.monthly_title", translation.Replace{"month": l.Month(month), "year": year}))),
		rowOf(text(l.T("noun.investor")), text(owner.DisplayName())),
		rowOf(text(l.T("field.document")), text(formattedDocument(*owner))),
		blankRow(),
		rowOf(bold(l.T("field.period")), bold(l.T("field.gross")), bold(l.T("field.fees")), bold(l.T("field.costs")),
			bold(l.T("field.net")), bold(l.T("field.share")), bold(l.T("field.amount")), bold(l.T("field.status"))),
	}}
	detail := sheet{name: l.T("report.entries_sheet"), rows: [][]cell{
		rowOf(bold(l.T("field.occurred_on")), bold(l.T("field.product")), bold(l.T("field.kind")), bold(l.T("field.description")),
			bold(l.T("report.gross")), bold(l.T("field.fee")), bold(l.T("field.fee_detail")), bold(l.T("field.cost_share")),
			bold(l.T("field.line_net"))),
	}}
	var total int64
	for _, inv := range invoices {
		summary.rows = append(summary.rows, rowOf(
			text(l.line("report.period", translation.Replace{"from": string(inv.PeriodStart), "to": string(inv.PeriodEnd.AddDays(-1))})),
			money(inv.Gross), money(inv.Fees), money(inv.Costs), money(inv.Net),
			percentBps(inv.ShareBps), money(inv.Amount), text(l.Status(inv.Status))))
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
	summary.rows = append(summary.rows, blankRow(), rowOf(bold(l.T("report.month_total")), text(""), text(""), text(""),
		text(""), text(""), money(total)))
	return writeWorkbook(w, []sheet{summary, detail})
}

// AnnualReport writes the year's income report of one investor as .xlsx: the
// payer, the beneficiary, and the amounts credited month by month, laid out
// the way the Receita Federal's informe de rendimentos is. Nothing is withheld
// by this package, and the report says so.
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
	invoices, err := Invoices(s.db).Where("investor_id", "=", owner.ID).
		Where("period_end", ">=", string(from)).Where("period_end", "<", string(to)).Get(ctx, g)
	if err != nil {
		return err
	}
	var byMonth [12]int64
	var total int64
	for _, inv := range invoices {
		m := inv.PeriodEnd.Time().Month()
		byMonth[m-1] += inv.Amount
		total += inv.Amount
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
		rowOf(bold(l.T("report.month")), bold(l.T("report.credited")), bold(l.T("report.withheld"))),
	}}
	for m, v := range byMonth {
		report.rows = append(report.rows, rowOf(text(l.Month(m+1)), money(v), money(0)))
	}
	report.rows = append(report.rows,
		rowOf(bold(l.T("report.total")), money(total), money(0)),
		blankRow(),
		rowOf(text(l.T("report.share_note"))),
		rowOf(text(l.T("report.tax_note"))),
	)
	return writeWorkbook(w, []sheet{report})
}
