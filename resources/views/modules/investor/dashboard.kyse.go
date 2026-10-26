//go:build kyse

package investor

import (
	"html/template"
	"strconv"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// DashboardData is what the handler hands this page.
//
// It is an alias rather than a struct declared here, so the shape is written
// once, beside the code that fills it.
type DashboardData = investor.DashboardPageData

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

func dashboardContracts(data DashboardData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Contracts))
	for _, c := range data.Contracts {
		links := string(components.Link(components.LinkProps{Label: data.Labels.T("control.statement"), URL: c.StatementURL, Variant: "hover"})) +
			" · " + string(components.Link(components.LinkProps{Label: data.Labels.T("control.projection"), URL: c.ProjectionURL, Variant: "hover"}))
		if c.FileURL != "" {
			links += " · " + string(components.Link(components.LinkProps{Label: data.Labels.T("field.file"), URL: c.FileURL, Variant: "hover"}))
		}
		variant := "outline"
		if c.Active {
			variant = ""
		}
		rows = append(rows, components.TableRow{Key: c.ID, Cells: []components.TableCell{
			{Text: c.Number + " v" + strconv.Itoa(c.Version)},
			{Text: c.Share},
			{Text: c.Products},
			{Text: strconv.Itoa(c.BillingDay), SortValue: strconv.Itoa(100 + c.BillingDay)},
			{Text: c.Period},
			{HTML: components.Badge(components.BadgeProps{Label: c.Status, Variant: variant})},
			{HTML: template.HTML(links)},
		}})
	}
	return components.DataTableProps{
		ID: "investor-contracts", Label: data.Labels.T("screen.contracts_title"), URL: data.Path,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 12,
		FullPager: true, HeaderMenu: true,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.number"), Key: "number", Sortable: true},
			{Label: data.Labels.T("field.share"), Key: "share"},
			{Label: data.Labels.T("field.products"), Key: "products", Hideable: true},
			{Label: data.Labels.T("field.billing_day"), Key: "billing_day", Hideable: true},
			{Label: data.Labels.T("field.period"), Key: "period", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: data.Labels.T("field.actions"), Key: "links"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.contracts_empty")},
	}
}

func dashboardInvoices(data DashboardData) components.DataTableProps {
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
func withdrawableTable(data DashboardData) components.DataTableProps {
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
func accountsTable(data DashboardData) components.DataTableProps {
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
func hasWithdrawable(data DashboardData) bool {
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
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.dashboard_title") }}</h1>
		</div>
		<p class="text-muted-foreground max-w-prose text-sm">{{ .Labels.T("screen.dashboard_lead") }}</p>
	</header>

	<dl class="mt-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
		<div class="card p-4">
			<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.available") }}</dt>
			<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Available }}</dd>
		</div>
		<div class="card p-4">
			<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.held") }}</dt>
			<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Held }}</dd>
		</div>
		<div class="card p-4">
			<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.received_year") }}</dt>
			<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .ReceivedYear }}</dd>
		</div>
		<div class="card p-4">
			<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.withdrawn_total") }}</dt>
			<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .WithdrawnTotal }}</dd>
		</div>
	</dl>

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

	<div class="mt-10">
		<h2 class="text-lg font-semibold">{{ .Labels.T("screen.contracts_title") }}</h2>
		{!! components.DataTable(dashboardContracts(.)) !!}
	</div>

	<div class="mt-10 grid gap-6 md:grid-cols-2">
		<div class="card p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.monthly_report") }}</h2>
			@if(len(.ReportMonths) == 0)
				<p class="text-muted-foreground mt-3 text-sm">{{ .Labels.T("screen.reports_empty") }}</p>
			@else
				<ul class="mt-3 flex flex-wrap gap-2">
				@foreach(.ReportMonths as month)
					<li><a class="btn" data-variant="outline" data-size="sm" href="{{ month.URL }}">{{ month.Label }}</a></li>
				@endforeach
				</ul>
			@endif
		</div>
		<div class="card p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.annual_report") }}</h2>
			<p class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.annual_lead") }}</p>
			@if(len(.ReportYears) == 0)
				<p class="text-muted-foreground mt-3 text-sm">{{ .Labels.T("screen.reports_empty") }}</p>
			@else
				<div class="mt-3 flex flex-wrap gap-2">
					@foreach(.ReportYears as year)
						<a class="btn" data-variant="outline" data-size="sm" href="{{ year.URL }}">{{ year.Label }}</a>
					@endforeach
				</div>
			@endif
		</div>
	</div>
</section>
@endsection
