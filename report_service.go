package investor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	br "github.com/hyz-is/arandu-br"
)

// Payer is who pays the investors, as it is printed on the income report: the
// company that owns the application. The application passes it in
// Config.Payer; an empty one prints a dash, never an invented name.
type Payer struct {
	Name     string
	Document string // CNPJ, digits or formatted
}

// monthNames are the months as the reports print them.
var monthNames = [...]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}

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
		return validation.Errors{"period": {"must be a year and a month, 1 to 12"}}
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

	summary := sheet{name: "Resumo", rows: [][]cell{
		rowOf(bold(fmt.Sprintf("Relatório mensal — %s de %d", monthNames[month-1], year))),
		rowOf(text("Investidor"), text(owner.DisplayName())),
		rowOf(text("CPF/CNPJ"), text(formattedDocument(*owner))),
		blankRow(),
		rowOf(bold("Período"), bold("Faturamento"), bold("Tarifas"), bold("Custos"), bold("Líquido"),
			bold("Participação"), bold("Valor"), bold("Situação")),
	}}
	detail := sheet{name: "Lançamentos", rows: [][]cell{
		rowOf(bold("Data"), bold("Produto"), bold("Tipo"), bold("Descrição"), bold("Bruto"),
			bold("Tarifa"), bold("Detalhe da tarifa"), bold("Custo rateado"), bold("Líquido")),
	}}
	var total int64
	for _, inv := range invoices {
		summary.rows = append(summary.rows, rowOf(
			text(string(inv.PeriodStart)+" a "+string(inv.PeriodEnd.AddDays(-1))),
			money(inv.Gross), money(inv.Fees), money(inv.Costs), money(inv.Net),
			percentBps(inv.ShareBps), money(inv.Amount), text(statusLabel(inv.Status))))
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
				text(string(e.OccurredOn)), text(e.ProductID), text(kindLabel(e.Kind)), text(e.Description),
				money(e.Gross), money(e.Fee), text(e.FeeDetail), money(cost), money(e.Gross-e.Fee-cost)))
		}
	}
	summary.rows = append(summary.rows, blankRow(), rowOf(bold("Total do mês"), text(""), text(""), text(""),
		text(""), text(""), money(total)))
	return writeWorkbook(w, []sheet{summary, detail})
}

// AnnualReport writes the year's income report of one investor as .xlsx: the
// payer, the beneficiary, and the amounts credited month by month, laid out
// the way the Receita Federal's informe de rendimentos is. Nothing is withheld
// by this package, and the report says so.
func (s *InvestorService) AnnualReport(ctx context.Context, actor security.Subject, investorID string, year int, w io.Writer) error {
	if year < 2000 {
		return validation.Errors{"year": {"must be a calendar year"}}
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
	report := sheet{name: "Informe de rendimentos", rows: [][]cell{
		rowOf(bold("Comprovante de rendimentos — ano-calendário " + fmt.Sprint(year))),
		blankRow(),
		rowOf(bold("1. Fonte pagadora")),
		rowOf(text("Nome empresarial"), text(dash(s.payer.Name))),
		rowOf(text("CNPJ"), text(dash(payerDocument))),
		blankRow(),
		rowOf(bold("2. Beneficiário")),
		rowOf(text("Nome"), text(owner.DisplayName())),
		rowOf(text("CPF/CNPJ"), text(formattedDocument(*owner))),
		blankRow(),
		rowOf(bold("3. Rendimentos creditados no ano")),
		rowOf(bold("Mês"), bold("Valor creditado"), bold("Imposto retido")),
	}}
	for m, v := range byMonth {
		report.rows = append(report.rows, rowOf(text(monthNames[m]), money(v), money(0)))
	}
	report.rows = append(report.rows,
		rowOf(bold("Total"), money(total), money(0)),
		blankRow(),
		rowOf(text("Os valores são a participação do investidor-anjo nos resultados, creditados por fatura de período fechado.")),
		rowOf(text("Nenhum imposto foi retido na fonte por este sistema. A classificação na declaração é do contador do beneficiário.")),
	)
	return writeWorkbook(w, []sheet{report})
}

func statusLabel(s InvoiceStatus) string {
	switch s {
	case InvoiceAvailable:
		return "Disponível"
	case InvoiceOverdue:
		return "Saque atrasado"
	case InvoiceWithdrawn:
		return "Sacada"
	case InvoiceNothingDue:
		return "Sem valor a receber"
	}
	return string(s)
}

func kindLabel(k EntryKind) string {
	switch k {
	case KindSale:
		return "Venda"
	case KindRenewal:
		return "Renovação"
	case KindRefund:
		return "Reembolso"
	case KindFee:
		return "Tarifa"
	case KindExpense:
		return "Despesa"
	case KindTax:
		return "Imposto"
	case KindAdjustment:
		return "Ajuste"
	case KindReversal:
		return "Estorno"
	}
	return string(k)
}
