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
			{Text: c.Starts + " — " + c.Ends},
			{HTML: components.Badge(components.BadgeProps{Label: c.Status, Variant: variant})},
			{HTML: template.HTML(links)},
		}})
	}
	return components.DataTableProps{
		ID: "investor-contracts", Label: data.Labels.T("screen.contracts_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.number"), Key: "number", Sortable: true},
			{Label: data.Labels.T("field.share"), Key: "share"},
			{Label: data.Labels.T("field.products"), Key: "products", Hideable: true},
			{Label: data.Labels.T("field.billing_day"), Key: "billing_day", Hideable: true},
			{Label: data.Labels.T("field.period"), Key: "period", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: "", Key: "links"},
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
			withdrawal += " " + components.Link(components.LinkProps{Label: data.Labels.T("field.receipt"), URL: i.ReceiptURL, Variant: "hover"})
		}
		rows = append(rows, components.TableRow{Key: i.ID, Cells: []components.TableCell{
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
		ID: "investor-invoices", Label: data.Labels.T("screen.invoices_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 12,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.period"), Key: "period"},
			{Label: data.Labels.T("field.gross"), Key: "gross", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.fees"), Key: "fees", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.costs"), Key: "costs", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.net"), Key: "net", Align: "right"},
			{Label: data.Labels.T("field.share"), Key: "share", Hideable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: data.Labels.T("field.withdrawal"), Key: "withdrawal"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.invoices_empty")},
	}
}

func accountSelect(data DashboardData) []components.SelectOption {
	options := make([]components.SelectOption, 0, len(data.Accounts))
	for _, a := range data.Accounts {
		options = append(options, components.SelectOption{Value: a.ID, Label: a.Label})
	}
	return options
}

func reportMonths(data DashboardData) []components.LinkProps {
	out := make([]components.LinkProps, 0, 12)
	year, month := data.Year, data.Month
	for range 12 {
		out = append(out, components.LinkProps{
			Label: twoDigits(month) + "/" + strconv.Itoa(year),
			URL:   data.ReportsURL + "/" + strconv.Itoa(year) + "/" + strconv.Itoa(month),
		})
		month--
		if month == 0 {
			month, year = 12, year-1
		}
	}
	return out
}

func reportYears(data DashboardData) []components.LinkProps {
	out := make([]components.LinkProps, 0, 3)
	for year := data.Year; year > data.Year-3; year-- {
		out = append(out, components.LinkProps{Label: strconv.Itoa(year), URL: data.ReportsURL + "/" + strconv.Itoa(year)})
	}
	return out
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.investor") }}</p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Investor }}</h1>
			<p class="text-muted-foreground mt-1 font-mono text-xs">{{ .Document }}</p>
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

	<div class="mt-10">
		<h2 class="text-lg font-semibold">{{ .Labels.T("screen.invoices_title") }}</h2>
		{!! components.DataTable(dashboardInvoices(.)) !!}
	</div>

	<div class="mt-10 grid gap-6 lg:grid-cols-3">
		<div class="card p-5 lg:col-span-2">
			<h2 class="text-base font-semibold">{{ .Labels.T("control.withdraw") }}</h2>
			<p class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.withdraw_lead") }}</p>
			@if(len(.Accounts) == 0)
				<p class="text-muted-foreground mt-4 text-sm">{{ .Labels.T("control.no_account") }}</p>
			@else
				<ul class="mt-4 grid gap-3">
				@foreach(.Invoices as invoice)
					@if(invoice.Withdrawable)
						<li class="flex flex-wrap items-end justify-between gap-3 border-t pt-3">
							<div>
								<p class="text-sm font-medium">{{ invoice.Period }}</p>
								<p class="text-lg font-semibold tabular-nums">{{ invoice.Amount }}</p>
							</div>
							<form class="flex flex-wrap items-end gap-2" method="post" action="{{ invoice.WithdrawURL }}">
								@csrf
								<div class="min-w-56">
									{!! components.Select(components.SelectProps{
										Name: "account_id", Label: .Labels.T("field.account"),
										Options: accountSelect(.), Placeholder: .Labels.T("control.choose_account"),
										Required: true, Page: .Form(),
									}) !!}
								</div>
								{!! components.Button(components.ButtonProps{Label: .Labels.T("control.withdraw"), Type: "submit"}) !!}
							</form>
						</li>
					@endif
				@endforeach
				</ul>
			@endif
		</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .PixURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.pix_title") }}</h2>
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.pix_lead") }}</p>
			{!! components.Select(components.SelectProps{
				Name: "pix_key_type", Label: .Labels.T("field.pix_key_type"),
				Options: .PixKeyOptions(), Required: true, Page: .Form(),
			}) !!}
			{!! components.Field(components.FieldProps{
				Name: "pix_key", Label: .Labels.T("field.pix_key"), Required: true, Page: .Form(),
			}) !!}
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit", Variant: "outline"}) !!}</div>
		</form>
	</div>

	<div class="mt-10">
		<h2 class="text-lg font-semibold">{{ .Labels.T("screen.contracts_title") }}</h2>
		{!! components.DataTable(dashboardContracts(.)) !!}
	</div>

	<div class="mt-10 grid gap-6 md:grid-cols-2">
		<div class="card p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.monthly_report") }}</h2>
			<ul class="mt-3 flex flex-wrap gap-2">
			@foreach(reportMonths(.) as month)
				<li><a class="btn" data-variant="outline" data-size="sm" href="{{ month.URL }}">{{ month.Label }}</a></li>
			@endforeach
			</ul>
		</div>
		<div class="card p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.annual_report") }}</h2>
			<p class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.annual_lead") }}</p>
			<div class="mt-3 flex flex-wrap gap-2">
				@foreach(reportYears(.) as year)
					<a class="btn" data-variant="outline" data-size="sm" href="{{ year.URL }}">{{ year.Label }}</a>
				@endforeach
			</div>
		</div>
	</div>
</section>
@endsection
