package investor

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/kyse/components"
	br "github.com/hyz-is/arandu-br"
	wallet "github.com/hyz-is/arandu-wallet"
)

// What the screens show, built from what the services answer. Nothing here
// reads the database or asks the policy: the handler already did both, and
// these only turn centavos, days and statuses into the text a page draws.

// brl is centavos as reais: 123456 is "R$ 1.234,56".
func brl(c int64) string { return br.BRL(c) }

// percentText is basis points as a percentage: 1500 is "15,00%".
func percentText(bps int64) string {
	return strings.Replace(strconv.FormatFloat(float64(bps)/100, 'f', 2, 64), ".", ",", 1) + "%"
}

// ratioText is parts per million as a percentage: 125000 is "12,50%".
func ratioText(ppm int64) string {
	return strings.Replace(strconv.FormatFloat(float64(ppm)/10000, 'f', 2, 64), ".", ",", 1) + "%"
}

// dayText is a day as Brazilians write it, and a dash for none.
func dayText(d Day) string {
	if d.IsZero() {
		return "—"
	}
	return br.Date(d.Time())
}

// periodText is a closed period as the investor reads it: the end day of
// [start, end) belongs to the next period, so the last day shown is the one
// before it.
func periodText(start, end Day) string {
	return dayText(start) + " a " + dayText(end.AddDays(-1))
}

// sortCents is centavos as text that orders like the number, for a column
// the browser sorts.
func sortCents(c int64) string { return fmt.Sprintf("%020d", c+1<<62) }

// parseReais reads an amount typed as reais -- "1.234,56", "1234,56",
// "1234.56", "R$ 10" -- as centavos.
func parseReais(s string) (int64, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "R$"))
	if s == "" {
		return 0, false
	}
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 || whole == "" {
		return 0, false
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > 1<<50 {
		return 0, false
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	v := w*100 + f
	if negative {
		v = -v
	}
	return v, true
}

// tableLines are the sentences of every data table, from the catalogue.
func (l Labels) tableLines() components.DataTableLines {
	return components.DataTableLines{
		Showing: l.T("table.showing"), PerPage: l.T("table.per_page"), NoResult: l.T("table.no_result"),
		Previous: l.T("table.previous"), Next: l.T("table.next"), Page: l.T("table.page"), PageOf: l.T("table.page_of"),
		Back: l.T("table.back"), Forward: l.T("table.forward"), SelectAll: l.T("table.select_all"), Bulk: l.T("table.bulk"),
		Apply: l.T("table.apply"), Ascending: l.T("table.ascending"), Descending: l.T("table.descending"), Unsorted: l.T("table.unsorted"),
	}
}

// TableLines are the sentences a data table on these screens says.
func (s Screen) TableLines() components.DataTableLines { return s.Labels.tableLines() }

// contractRows are an investor's contracts, the version in force first.
func contractRows(prefix string, l Labels, list []ContractResource) []ContractRow {
	rows := make([]ContractRow, 0, len(list))
	for _, r := range list {
		c := r.Contract()
		products := l.T("field.all_products")
		if !bool(c.AllProducts) {
			products = strings.Join(r.Products(), ", ")
		}
		row := ContractRow{
			ID: c.ID, Number: c.Number, Share: percentText(c.ShareBps), Products: products,
			Status: l.T("status." + string(c.Status)), Starts: dayText(c.StartsOn), Ends: dayText(c.EndsOn),
			Version: c.Version, BillingDay: c.BillingDay, Active: c.Status == ContractActive,
			StatementURL:  prefix + "/contracts/" + c.ID + "/statement",
			ProjectionURL: prefix + "/contracts/" + c.ID + "/projection",
			CloseURL:      prefix + adminSegment + "/contracts/" + c.ID + "/closings",
			AmendURL:      prefix + adminSegment + "/contracts/" + c.ID + "/amendments",
		}
		if c.FileKey != "" {
			row.FileURL = prefix + "/contracts/" + c.ID + "/file"
		}
		rows = append(rows, row)
	}
	return rows
}

// invoiceRows are an investor's invoices, as the panel and the admin show them.
func invoiceRows(prefix string, l Labels, views []InvoiceView) []InvoiceRow {
	rows := make([]InvoiceRow, 0, len(views))
	for _, v := range views {
		inv := v.Invoice
		row := InvoiceRow{
			ID: inv.ID, Period: periodText(inv.PeriodStart, inv.PeriodEnd),
			Gross: brl(inv.Gross), Fees: brl(inv.Fees), Costs: brl(inv.Costs), Net: brl(inv.Net),
			Share: percentText(inv.ShareBps), Amount: brl(inv.Amount),
			Status: l.Status(inv.Status), StatusKey: string(inv.Status),
			Withdrawable: v.Withdrawable(),
			WithdrawURL:  prefix + "/invoices/" + inv.ID + "/withdrawal",
		}
		if w := v.Withdrawal; w != nil {
			row.Withdrawal = l.T("withdrawal." + string(w.Status))
			row.WithdrawalKey = string(w.Status)
			row.RejectedReason = w.RejectedReason
			if w.Status == wallet.WithdrawalPaid && w.ReceiptKey != "" {
				row.ReceiptURL = prefix + "/invoices/" + inv.ID + "/receipt"
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// invoiceTotals are the panel's four figures: what can be withdrawn now, what
// is waiting for the operator, what closed this year, and what was paid out.
func invoiceTotals(views []InvoiceView, year int) (available, held, receivedYear, withdrawn int64) {
	for _, v := range views {
		inv := v.Invoice
		switch {
		case v.Withdrawn():
			withdrawn += inv.Amount
		case v.Pending():
			held += inv.Amount
		case v.Withdrawable():
			available += inv.Amount
		}
		if inv.Amount > 0 && inv.PeriodEnd.Time().Year() == year {
			receivedYear += inv.Amount
		}
	}
	return available, held, receivedYear, withdrawn
}

// accountOptions are the payout accounts a withdrawal can go to.
func accountOptions(accounts []*wallet.PayoutAccount) []AccountOption {
	out := make([]AccountOption, 0, len(accounts))
	for _, a := range accounts {
		if a == nil || bool(a.Disabled) {
			continue
		}
		out = append(out, AccountOption{ID: a.ID, Label: a.Label})
	}
	return out
}

// statementRows are a statement's lines.
func statementRows(l Labels, st *Statement) []StatementRow {
	rows := make([]StatementRow, 0, len(st.Lines))
	for _, line := range st.Lines {
		e := line.Entry
		rows = append(rows, StatementRow{
			ID: e.ID, Day: dayText(e.OccurredOn), Product: e.ProductID, Kind: l.Kind(e.Kind), Description: e.Description,
			Gross: brl(e.Gross), Fee: brl(e.Fee), FeeDetail: e.FeeDetail, Cost: brl(line.Cost), Net: brl(line.Net),
			Estimate:  st.Open[e.ID],
			GrossSort: sortCents(e.Gross), FeeSort: sortCents(e.Fee), CostSort: sortCents(line.Cost), NetSort: sortCents(line.Net),
			DaySortValue: string(e.OccurredOn),
		})
	}
	return rows
}

// entryKinds are the kinds an entry can be, in the order the filters list them.
var entryKinds = []EntryKind{KindSale, KindRenewal, KindRefund, KindFee, KindExpense, KindTax, KindAdjustment, KindReversal}

// kindOptions are the entry kinds as select options.
func kindOptions(l Labels, withReversal bool) []components.SelectOption {
	out := make([]components.SelectOption, 0, len(entryKinds))
	for _, k := range entryKinds {
		if k == KindReversal && !withReversal {
			continue
		}
		out = append(out, components.SelectOption{Value: string(k), Label: l.Kind(k)})
	}
	return out
}

// KindOptions are the kinds of entry a filter offers.
func (s Screen) KindOptions() []components.SelectOption { return kindOptions(s.Labels, true) }

// ManualKindOptions are the kinds of entry the admin types by hand: a
// reversal is made from the entry it undoes, never typed.
func (s Screen) ManualKindOptions() []components.SelectOption { return kindOptions(s.Labels, false) }

// RecurrenceOptions are how often a cost is due.
func (s Screen) RecurrenceOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, 3)
	for _, r := range []Recurrence{Monthly, Yearly, Once} {
		out = append(out, components.SelectOption{Value: string(r), Label: s.Labels.T("recurrence." + string(r))})
	}
	return out
}

// PersonOptions are the kinds of person an investor can be.
func (s Screen) PersonOptions() []components.SelectOption {
	return []components.SelectOption{
		{Value: string(Person), Label: s.Labels.T("person.person")},
		{Value: string(Company), Label: s.Labels.T("person.company")},
	}
}

// PixKeyOptions are the kinds of PIX key.
func (s Screen) PixKeyOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, len(PixKeyTypes))
	for _, k := range PixKeyTypes {
		out = append(out, components.SelectOption{Value: k, Label: s.Labels.T("pix." + k)})
	}
	return out
}

// investorRow is one investor as the registry shows it.
func investorRow(prefix string, l Labels, i *Investor) InvestorRow {
	doc := br.CPF(i.Document)
	if i.Kind == Company {
		doc = br.CNPJ(i.Document)
	}
	return InvestorRow{
		ID: i.ID, Name: i.Name, Document: doc, Kind: l.T("person." + string(i.Kind)), Email: i.Email,
		Status: l.T("status." + string(i.Status)), User: i.UserID, Active: i.IsActive(),
		URL: prefix + adminSegment + "/investors/" + i.ID,
	}
}

// costRows are the declared costs.
func costRows(prefix string, l Labels, views []CostView, today Day) []CostRow {
	rows := make([]CostRow, 0, len(views))
	for _, v := range views {
		c := v.Item
		scope := l.T("field.all_products")
		if !bool(c.AllProducts) {
			scope = strings.Join(v.Products, ", ")
		}
		rows = append(rows, CostRow{
			ID: c.ID, Name: c.Name, Category: c.Category, Amount: brl(c.Amount),
			Recurrence: l.T("recurrence." + string(c.Recurrence)), Starts: dayText(c.StartsOn), Ends: dayText(c.EndsOn),
			Scope: scope, EndURL: prefix + adminSegment + "/costs/" + c.ID + "/end",
			Ended: !c.EndsOn.IsZero() && c.EndsOn.Before(today),
		})
	}
	return rows
}

// entryRows are a page of the admin's books.
func entryRows(prefix string, l Labels, entries []*Entry) []EntryRow {
	rows := make([]EntryRow, 0, len(entries))
	for _, e := range entries {
		source := l.T("source." + string(e.Source))
		rows = append(rows, EntryRow{
			ID: e.ID, Day: dayText(e.OccurredOn), Product: e.ProductID, Kind: l.Kind(e.Kind), Description: e.Description,
			Gross: brl(e.Gross), Fee: brl(e.Fee), Net: brl(e.Net), Source: source, Ref: e.ExternalRef,
			ReverseURL: prefix + adminSegment + "/entries/" + e.ID + "/reversal",
			Reversible: e.Kind != KindReversal,
			GrossSort:  sortCents(e.Gross), DaySort: string(e.OccurredOn),
		})
	}
	return rows
}

// withdrawalRows are the operator's queue.
func withdrawalRows(prefix string, l Labels, list []InvestorWithdrawal, loc *time.Location) []WithdrawalRow {
	rows := make([]WithdrawalRow, 0, len(list))
	for _, w := range list {
		r := w.Request
		name := ""
		if w.Investor != nil {
			name = w.Investor.Name
		}
		account := r.AccountSnapshot["label"]
		if holder := r.AccountSnapshot["holder_name"]; holder != "" {
			account += " · " + holder
		}
		base := prefix + adminSegment + "/withdrawals/" + r.ID
		row := WithdrawalRow{
			ID: r.ID, Investor: name, Amount: brl(int64(r.Amount)), Account: account,
			Status: l.T("withdrawal." + string(r.Status)), StatusKey: string(r.Status),
			RequestedAt: r.RequestedAt.In(loc).Format("02/01/2006 15:04"), Reason: r.RejectedReason,
			Open: r.Status == wallet.WithdrawalRequested, Processing: r.Status == wallet.WithdrawalProcessing,
			Paid:       r.Status == wallet.WithdrawalPaid,
			ProcessURL: base + "/processing", PayURL: base + "/payment", RejectURL: base + "/rejection",
		}
		if row.Paid && r.ReceiptKey != "" && w.Invoice != nil {
			row.ReceiptURL = prefix + "/invoices/" + w.Invoice.ID + "/receipt"
		}
		rows = append(rows, row)
	}
	return rows
}

// AdminNav is the bar on top of every admin screen: the four lists the admin
// works in, the current one marked.
func (s Screen) AdminNav(current string) template.HTML {
	var b strings.Builder
	b.WriteString(`<nav class="flex flex-wrap items-center gap-1 border-b pb-3" aria-label="` + template.HTMLEscapeString(s.Labels.T("noun.investors")) + `">`)
	for _, item := range []struct{ key, path string }{
		{"investors", "/investors"}, {"entries", "/entries"}, {"costs", "/costs"}, {"withdrawals", "/withdrawals"},
	} {
		b.WriteString(string(components.Link(components.LinkProps{
			Label: s.Labels.T("noun." + item.key), URL: s.Prefix + adminSegment + item.path,
			Variant: "hover", Current: item.key == current,
			ComponentProps: components.ComponentProps{Class: "rounded-md px-3 py-1.5 text-sm"},
		})))
	}
	b.WriteString(`</nav>`)
	return template.HTML(b.String())
}

// CellInput is one field of a form drawn inside a table cell.
type CellInput struct {
	Name, Type, Placeholder, Value, Label string
	Required                              bool
}

// CellForm is a small form posted from a table cell: the CSRF token of the
// page, the fields given, and one button. Every value is escaped; nothing the
// caller passes is written as markup.
func (s Screen) CellForm(action, label, variant string, inputs ...CellInput) template.HTML {
	esc := template.HTMLEscapeString
	var b strings.Builder
	b.WriteString(`<form class="flex flex-wrap items-center gap-1" method="post" action="` + esc(action) + `">`)
	b.WriteString(`<input type="hidden" name="_token" value="` + esc(s.CSRFToken()) + `">`)
	for _, in := range inputs {
		kind := in.Type
		if kind == "" {
			kind = "text"
		}
		if kind == "hidden" {
			b.WriteString(`<input type="hidden" name="` + esc(in.Name) + `" value="` + esc(in.Value) + `">`)
			continue
		}
		b.WriteString(`<input class="input h-8 w-36 text-xs" type="` + esc(kind) + `" name="` + esc(in.Name) + `" value="` + esc(in.Value) + `"`)
		if in.Placeholder != "" {
			b.WriteString(` placeholder="` + esc(in.Placeholder) + `"`)
		}
		if in.Label != "" {
			b.WriteString(` aria-label="` + esc(in.Label) + `"`)
		}
		if in.Required {
			b.WriteString(` required`)
		}
		b.WriteString(`>`)
	}
	if variant == "" {
		variant = "outline"
	}
	b.WriteString(`<button class="btn" data-variant="` + esc(variant) + `" data-size="sm" type="submit">` + esc(label) + `</button></form>`)
	return template.HTML(b.String())
}

// WithdrawalStatusOptions are where a withdrawal request can stand.
func (s Screen) WithdrawalStatusOptions() []components.SelectOption {
	statuses := []wallet.WithdrawalStatus{
		wallet.WithdrawalRequested, wallet.WithdrawalProcessing, wallet.WithdrawalPaid,
		wallet.WithdrawalRejected, wallet.WithdrawalCancelled,
	}
	out := make([]components.SelectOption, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, components.SelectOption{Value: string(st), Label: s.Labels.T("withdrawal." + string(st))})
	}
	return out
}
