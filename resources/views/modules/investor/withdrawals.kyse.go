//go:build kyse

package investor

import (
	"html/template"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// WithdrawalsData is what the handler hands this page.
type WithdrawalsData = investor.WithdrawalsPageData

func statusVariant(key string) string {
	switch key {
	case "available":
		return ""
	case "overdue", "rejected":
		return "destructive"
	case "withdrawn", "paid":
		return "outline"
	}
	return "secondary"
}

func dashboardInvoices(data WithdrawalsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Invoices))
	for _, i := range data.Invoices {
		withdrawal := template.HTML("")
		if i.Withdrawal != "" {
			withdrawal = components.Badge(components.BadgeProps{Label: i.Withdrawal, Variant: statusVariant(i.WithdrawalKey)})
		}
		if i.ReceiptURL != "" {
			withdrawal += " " + components.Link(components.LinkProps{Label: data.Labels.T("control.view_receipt"), URL: i.ReceiptURL, Variant: "hover"})
		}
		facets := map[string]string{"status": i.Status}
		if i.Withdrawal != "" {
			facets["withdrawal"] = i.Withdrawal
		}
		rows = append(rows, components.TableRow{Key: i.ID, Facets: facets, Cells: []components.TableCell{
			{Text: i.Period},
			{Text: i.Gross},
			{Text: i.Fees},
			{Text: i.Costs},
			{Text: i.Net},
			{Text: i.Share},
			{Text: i.Amount},
			{HTML: components.Badge(components.BadgeProps{Label: i.Status, Variant: statusVariant(i.StatusKey)})},
			{HTML: withdrawal},
		}})
	}
	return components.DataTableProps{
		ID: "investor-invoices", Label: data.Labels.T("screen.invoices_title"), URL: data.Path,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 12,
		PageSizes: []int{12, 24, 48}, FullPager: true, HeaderMenu: true,
		Filters: investor.CompleteFilters(rows,
			components.DataTableFilter{Key: "status", Label: data.Labels.T("field.status")},
			components.DataTableFilter{Key: "withdrawal", Label: data.Labels.T("field.withdrawal")}),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.period"), Key: "period"},
			{Label: data.Labels.T("field.gross"), Key: "gross", Hideable: true, Align: "end"},
			{Label: data.Labels.T("field.fees"), Key: "fees", Hideable: true, Align: "end"},
			{Label: data.Labels.T("field.costs"), Key: "costs", Hideable: true, Align: "end"},
			{Label: data.Labels.T("field.net"), Key: "net", Align: "end"},
			{Label: data.Labels.T("field.share"), Key: "share", Hideable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "end"},
			{Label: data.Labels.T("field.status"), Key: "status", Hideable: true},
			{Label: data.Labels.T("field.withdrawal"), Key: "withdrawal", Hideable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.invoices_empty")},
	}
}

// withdrawableTable lists the invoices that can be asked for now, each with
// the form that asks for it: the account to be paid into and the button.
func withdrawableTable(data WithdrawalsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Invoices))
	for _, i := range data.Invoices {
		if !i.Withdrawable {
			continue
		}
		action := template.HTML("")
		if len(data.Accounts) > 0 {
			action = data.WithdrawForm(i.WithdrawURL, data.Accounts)
		}
		rows = append(rows, components.TableRow{Key: i.ID, Cells: []components.TableCell{
			{Text: i.Period},
			{Text: i.Amount},
			{HTML: action},
		}})
	}
	return components.DataTableProps{
		ID: "investor-withdrawable", Label: data.Labels.T("screen.withdraw_title"), URL: data.Path,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 12,
		FullPager: true, HeaderMenu: true,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.period"), Key: "period"},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "end"},
			{Label: data.Labels.T("field.account"), Key: "action"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.withdraw_none")},
	}
}

// accountsTable lists the investor's payout accounts.
func accountsTable(data WithdrawalsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Accounts))
	for _, a := range data.Accounts {
		rows = append(rows, components.TableRow{Key: a.ID, Cells: []components.TableCell{
			{Text: a.Label},
			{HTML: components.Badge(components.BadgeProps{Label: data.Labels.T("payout." + a.Kind), Variant: "outline"}), SortValue: a.Kind},
		}})
	}
	return components.DataTableProps{
		ID: "investor-accounts", Label: data.Labels.T("screen.accounts_title"), URL: data.Path,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 12,
		FullPager: true, HeaderMenu: true,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.account"), Key: "label"},
			{Label: data.Labels.T("field.payout_kind"), Key: "kind"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.accounts_empty")},
	}
}

// hasWithdrawable says whether any invoice can be asked for now.
func hasWithdrawable(data WithdrawalsData) bool {
	for _, i := range data.Invoices {
		if i.Withdrawable {
			return true
		}
	}
	return false
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.investor") }} · {{ .Investor }} · <span class="font-mono">{{ .Document }}</span></p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.withdrawals_page_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.withdrawals_page_lead") }}</p>
		</div>
		{!! SectionMenu(.Screen) !!}
	</header>

	<div class="mt-10" id="saque">
		<h2 class="text-lg font-semibold">{{ .Labels.T("screen.withdraw_title") }}</h2>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.withdraw_lead") }}</p>
		@if(hasWithdrawable(.) && len(.Accounts) == 0)
			<p class="mt-3 text-sm"><a class="underline underline-offset-4" href="#contas">{{ .Labels.T("control.no_account") }}</a></p>
		@endif
		{!! components.DataTable(withdrawableTable(.)) !!}
	</div>

	<div class="mt-10">
		<h2 class="text-lg font-semibold">{{ .Labels.T("screen.withdrawals_title") }}</h2>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.withdrawals_lead") }}</p>
		{!! components.DataTable(dashboardInvoices(.)) !!}
	</div>

	<div class="mt-10" id="contas">
		<h2 class="text-lg font-semibold">{{ .Labels.T("screen.accounts_title") }}</h2>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.accounts_lead") }}</p>
		{!! components.DataTable(accountsTable(.)) !!}

		<div class="mt-6 grid gap-6 lg:grid-cols-2">
			<form class="card grid content-start gap-3 p-5" method="post" action="{{ .PayoutAccountsURL }}">
				@csrf
				<input type="hidden" name="kind" value="pix">
				<h3 class="text-base font-semibold">{{ .Labels.T("control.add_pix") }}</h3>
				{!! components.Select(components.SelectProps{
					Name: "pix_key_type", Label: .Labels.T("field.pix_key_type"),
					Options: .PixKeyOptions(), Required: true, Page: .Form(),
				}) !!}
				{!! components.Field(components.FieldProps{
					Name: "pix_key", Label: .Labels.T("field.pix_key"), Required: true, Page: .Form(),
				}) !!}
				<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit", Variant: "outline"}) !!}</div>
			</form>

			<form class="card grid content-start gap-3 p-5" method="post" action="{{ .PayoutAccountsURL }}">
				@csrf
				<input type="hidden" name="kind" value="bank">
				<h3 class="text-base font-semibold">{{ .Labels.T("control.add_bank") }}</h3>
				{!! components.Field(components.FieldProps{
					Name: "bank", Label: .Labels.T("field.bank"), Required: true, Page: .Form(),
				}) !!}
				<div class="grid gap-3 sm:grid-cols-2">
					{!! components.Field(components.FieldProps{
						Name: "branch", Label: .Labels.T("field.branch"), Required: true, Page: .Form(),
					}) !!}
					{!! components.Field(components.FieldProps{
						Name: "account_number", Label: .Labels.T("field.account_number"), Required: true, Page: .Form(),
					}) !!}
				</div>
				{!! components.Select(components.SelectProps{
					Name: "account_type", Label: .Labels.T("field.account_type"),
					Options: .BankAccountTypeOptions(), Required: true, Page: .Form(),
				}) !!}
				<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit", Variant: "outline"}) !!}</div>
			</form>
		</div>
	</div>
</section>
@endsection
