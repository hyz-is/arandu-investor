//go:build kyse

package admin

import (
	"html/template"
	"strconv"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// InvestorData is what the handler hands this page.
type InvestorData = investor.AdminInvestorPageData

func badgeFor(key string) string {
	switch key {
	case "available", "active":
		return ""
	case "overdue", "rejected":
		return "destructive"
	case "withdrawn", "paid", "superseded":
		return "outline"
	}
	return "secondary"
}

func contractsTable(data InvestorData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Contracts))
	for _, c := range data.Contracts {
		links := string(components.Link(components.LinkProps{Label: data.Labels.T("control.statement"), URL: c.StatementURL, Variant: "hover"})) +
			" · " + string(components.Link(components.LinkProps{Label: data.Labels.T("control.projection"), URL: c.ProjectionURL, Variant: "hover"}))
		if c.FileURL != "" {
			links += " · " + string(components.Link(components.LinkProps{Label: data.Labels.T("field.file"), URL: c.FileURL, Variant: "hover"}))
		}
		close := template.HTML("")
		if c.Active {
			close = data.CellForm(c.CloseURL, data.Labels.T("screen.close_period"), "outline",
				investor.CellInput{Name: "as_of", Type: "date", Value: data.Today, Label: data.Labels.T("field.as_of"), Required: true})
		}
		status := "superseded"
		if c.Active {
			status = "active"
		}
		rows = append(rows, components.TableRow{Key: c.ID, Cells: []components.TableCell{
			{Text: c.Number, SortValue: c.Number + "-" + strconv.Itoa(1000+c.Version)},
			{Text: "v" + strconv.Itoa(c.Version)},
			{Text: c.Share},
			{Text: c.Products},
			{Text: strconv.Itoa(c.BillingDay)},
			{Text: c.Starts + " — " + c.Ends},
			{HTML: components.Badge(components.BadgeProps{Label: c.Status, Variant: badgeFor(status)})},
			{HTML: template.HTML(links)},
			{HTML: close},
		}})
	}
	return components.DataTableProps{
		ID: "investor-admin-contracts", Label: data.Labels.T("screen.contracts_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.number"), Key: "number", Sortable: true},
			{Label: data.Labels.T("field.version"), Key: "version"},
			{Label: data.Labels.T("field.share"), Key: "share"},
			{Label: data.Labels.T("field.products"), Key: "products", Hideable: true},
			{Label: data.Labels.T("field.billing_day"), Key: "billing_day", Hideable: true},
			{Label: data.Labels.T("field.period"), Key: "period", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: "", Key: "links"},
			{Label: data.Labels.T("screen.close_period"), Key: "close"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.contracts_empty")},
	}
}

func invoicesTable(data InvestorData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Invoices))
	for _, i := range data.Invoices {
		withdrawal := template.HTML("")
		if i.Withdrawal != "" {
			withdrawal = components.Badge(components.BadgeProps{Label: i.Withdrawal, Variant: badgeFor(i.WithdrawalKey)})
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
			{Text: i.Amount},
			{HTML: components.Badge(components.BadgeProps{Label: i.Status, Variant: badgeFor(i.StatusKey)})},
			{HTML: withdrawal},
		}})
	}
	return components.DataTableProps{
		ID: "investor-admin-invoices", Label: data.Labels.T("screen.invoices_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 12,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.period"), Key: "period"},
			{Label: data.Labels.T("field.gross"), Key: "gross", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.fees"), Key: "fees", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.costs"), Key: "costs", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.net"), Key: "net", Align: "right"},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: data.Labels.T("field.withdrawal"), Key: "withdrawal"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.invoices_empty")},
	}
}

func reportLinks(data InvestorData) []components.LinkProps {
	out := make([]components.LinkProps, 0, 15)
	for year := data.Year; year > data.Year-3; year-- {
		out = append(out, components.LinkProps{Label: data.Labels.T("screen.annual_short") + " " + strconv.Itoa(year), URL: data.ReportsURL + "/" + strconv.Itoa(year)})
	}
	year, month := data.Year, data.Month
	for range 12 {
		label := strconv.Itoa(month) + "/" + strconv.Itoa(year)
		if month < 10 {
			label = "0" + label
		}
		out = append(out, components.LinkProps{Label: label, URL: data.ReportsURL + "/" + strconv.Itoa(year) + "/" + strconv.Itoa(month)})
		month--
		if month == 0 {
			month, year = 12, year-1
		}
	}
	return out
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("investors") !!}

	<header class="mt-6 flex flex-wrap items-start justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Investor.Kind }} · {{ .Investor.Document }}</p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Investor.Name }}</h1>
			<p class="text-muted-foreground mt-1 text-sm">{{ .Investor.Email }} · {{ .Labels.T("field.user") }} <span class="font-mono text-xs">{{ .Investor.User }}</span></p>
		</div>
		<div class="flex items-center gap-2">
			@if(.Investor.Active)
				{!! components.Badge(components.BadgeProps{Label: .Investor.Status}) !!}
				<form method="post" action="{{ .SuspensionURL }}">
					@csrf
					{!! components.Button(components.ButtonProps{Label: .Labels.T("control.suspend"), Type: "submit", Variant: "destructive", Size: "sm"}) !!}
				</form>
			@else
				{!! components.Badge(components.BadgeProps{Label: .Investor.Status, Variant: "outline"}) !!}
				<form method="post" action="{{ .SuspensionURL }}">
					@csrf
					<input type="hidden" name="_method" value="DELETE">
					{!! components.Button(components.ButtonProps{Label: .Labels.T("control.reactivate"), Type: "submit", Size: "sm"}) !!}
				</form>
			@endif
		</div>
	</header>

	<div class="mt-8">{!! components.DataTable(contractsTable(.)) !!}</div>

	<div class="mt-8 grid gap-6 lg:grid-cols-2">
		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .ContractStoreURL }}" enctype="multipart/form-data">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.new_contract") }}</h2>
			<div class="grid gap-3 sm:grid-cols-2">
				{!! components.Field(components.FieldProps{Name: "number", Label: .Labels.T("field.number"), Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "share_percent", Label: .Labels.T("field.share_percent"), Placeholder: "15", Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "signed_on", Label: .Labels.T("field.signed_on"), Type: "date", Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "billing_day", Label: .Labels.T("field.billing_day"), Type: "number", Hint: "1–28", Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "starts_on", Label: .Labels.T("field.starts_on"), Type: "date", Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "ends_on", Label: .Labels.T("field.ends_on"), Type: "date", Page: .Form()}) !!}
			</div>
			<label class="flex items-center gap-2 text-sm"><input type="checkbox" class="input" name="all_products" value="1" checked> {{ .Labels.T("field.all_products") }}</label>
			{!! components.Field(components.FieldProps{Name: "products", Label: .Labels.T("field.products"), Hint: .Labels.T("screen.products_hint"), Page: .Form()}) !!}
			<div class="grid gap-2">
				<label class="label" for="contract-file">{{ .Labels.T("field.file") }}</label>
				<input class="input" id="contract-file" type="file" name="file" accept="application/pdf" required>
			</div>
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
		</form>

		<div class="grid content-start gap-6">
			@foreach(.Contracts as contract)
				@if(contract.Active)
					<details class="card p-5">
						<summary class="cursor-pointer text-base font-semibold">{{ .Labels.T("screen.amend_contract") }} — {{ contract.Number }} v{{ contract.Version }}</summary>
						<form class="mt-4 grid gap-3" method="post" action="{{ contract.AmendURL }}" enctype="multipart/form-data">
							@csrf
							<div class="grid gap-3 sm:grid-cols-2">
								{!! components.Field(components.FieldProps{Name: "number", Label: .Labels.T("field.number"), Value: contract.Number, Required: true}) !!}
								{!! components.Field(components.FieldProps{Name: "share_percent", Label: .Labels.T("field.share_percent"), Required: true}) !!}
								{!! components.Field(components.FieldProps{Name: "signed_on", Label: .Labels.T("field.signed_on"), Type: "date", Required: true}) !!}
								{!! components.Field(components.FieldProps{Name: "billing_day", Label: .Labels.T("field.billing_day"), Type: "number", Required: true}) !!}
								{!! components.Field(components.FieldProps{Name: "starts_on", Label: .Labels.T("field.starts_on"), Type: "date", Required: true}) !!}
								{!! components.Field(components.FieldProps{Name: "ends_on", Label: .Labels.T("field.ends_on"), Type: "date"}) !!}
							</div>
							<label class="flex items-center gap-2 text-sm"><input type="checkbox" class="input" name="all_products" value="1" checked> {{ .Labels.T("field.all_products") }}</label>
							{!! components.Field(components.FieldProps{Name: "products", Label: .Labels.T("field.products"), Hint: .Labels.T("screen.products_hint")}) !!}
							<input class="input" type="file" name="file" accept="application/pdf" required aria-label="{{ .Labels.T("field.file") }}">
							<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("screen.amend_contract"), Type: "submit", Variant: "outline"}) !!}</div>
						</form>
					</details>
				@endif
			@endforeach

			<form class="card grid content-start gap-3 p-5" method="post" action="{{ .UpdateURL }}">
				@csrf
				<input type="hidden" name="_method" value="PUT">
				<h2 class="text-base font-semibold">{{ .Labels.T("screen.edit_investor") }}</h2>
				{!! components.Field(components.FieldProps{Name: "name", Label: .Labels.T("field.name"), Value: .Investor.Name, Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "legal_name", Label: .Labels.T("field.legal_name"), Value: .LegalName, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "legal_representative", Label: .Labels.T("field.legal_representative"), Value: .LegalRepresentative, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "email", Label: .Labels.T("field.email"), Type: "email", Value: .Investor.Email, Required: true, Page: .Form()}) !!}
				<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit", Variant: "outline"}) !!}</div>
			</form>
		</div>
	</div>

	<div class="mt-10">{!! components.DataTable(invoicesTable(.)) !!}</div>

	<div class="card mt-8 p-5">
		<h2 class="text-base font-semibold">{{ .Labels.T("screen.reports_title") }}</h2>
		<ul class="mt-3 flex flex-wrap gap-2">
		@foreach(reportLinks(.) as report)
			<li><a class="btn" data-variant="outline" data-size="sm" href="{{ report.URL }}">{{ report.Label }}</a></li>
		@endforeach
		</ul>
	</div>
</section>
@endsection
