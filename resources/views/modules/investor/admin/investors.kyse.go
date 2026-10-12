//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// InvestorsData is what the handler hands this page.
type InvestorsData = investor.AdminInvestorsPageData

func investorsTable(data InvestorsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		variant := "outline"
		if r.Active {
			variant = ""
		}
		rows = append(rows, components.TableRow{Key: r.ID, Cells: []components.TableCell{
			{HTML: components.Link(components.LinkProps{Label: r.Name, URL: r.URL, Variant: "hover"}), SortValue: r.Name},
			{Text: r.Document},
			{Text: r.Kind},
			{Text: r.Email},
			{Text: r.User},
			{HTML: components.Badge(components.BadgeProps{Label: r.Status, Variant: variant}), SortValue: r.Status},
		}})
	}
	return components.DataTableProps{
		ID: "investor-registry", Label: data.Labels.T("screen.admin_investors_title"),
		Caption: data.Labels.T("screen.admin_investors_lead"), ColumnsLabel: data.Labels.T("control.columns"),
		Lines: data.TableLines(), Complete: data.Next == "", PageSize: 25,
		SearchName: "q", SearchPlaceholder: data.Labels.T("control.search"),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.name"), Key: "name", Sortable: true},
			{Label: data.Labels.T("field.document"), Key: "document"},
			{Label: data.Labels.T("field.person_kind"), Key: "kind", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.email"), Key: "email", Hideable: true},
			{Label: data.Labels.T("field.user"), Key: "user", Hideable: true, Hidden: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.admin_investors_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("investors") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_investors_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_investors_lead") }}</p>
	</header>

	<div class="mt-6 grid gap-6 lg:grid-cols-3">
		<div class="lg:col-span-2">
			{!! components.DataTable(investorsTable(.)) !!}
			@if(.NextURL != "")
				<div class="mt-4 flex justify-end">
					<a class="btn" data-variant="outline" data-size="sm" href="{{ .NextURL }}">{{ .Labels.T("table.forward") }}</a>
				</div>
			@endif
		</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .StoreURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.new_investor") }}</h2>
			{!! components.Select(components.SelectProps{
				Name: "person_kind", Label: .Labels.T("field.person_kind"),
				Options: .PersonOptions(), Required: true, Page: .Form(),
			}) !!}
			{!! components.Field(components.FieldProps{Name: "document", Label: .Labels.T("field.document"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "name", Label: .Labels.T("field.name"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "legal_name", Label: .Labels.T("field.legal_name"), Hint: .Labels.T("screen.company_only"), Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "legal_representative", Label: .Labels.T("field.legal_representative"), Hint: .Labels.T("screen.company_only"), Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "email", Label: .Labels.T("field.email"), Type: "email", Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "user_id", Label: .Labels.T("field.user"), Hint: .Labels.T("screen.user_hint"), Required: true, Page: .Form()}) !!}
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
		</form>
	</div>
</section>
@endsection
