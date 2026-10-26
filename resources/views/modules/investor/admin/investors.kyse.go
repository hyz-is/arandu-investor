//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// InvestorsData is what the handler hands this page.
type InvestorsData = investor.AdminInvestorsPageData

// investorsTable is the registry. The package answers a short list whole, and
// the browser then searches, orders, filters and hides columns of it; a list
// longer than one page it answers a page at a time by cursor, with no search,
// order or total, and the table then only steps to the next page.
func investorsTable(data InvestorsData) components.DataTableProps {
	whole := data.Next == ""
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		variant := "outline"
		if r.Active {
			variant = ""
		}
		rows = append(rows, components.TableRow{Key: r.ID, Facets: map[string]string{"status": r.Status}, Cells: []components.TableCell{
			{HTML: components.Link(components.LinkProps{Label: r.Name, URL: r.URL, Variant: "hover"}), SortValue: r.Name},
			{Text: r.Document},
			{Text: r.Kind},
			{Text: r.Email},
			{Text: r.User},
			{HTML: components.Badge(components.BadgeProps{Label: r.Status, Variant: variant}), SortValue: r.Status},
		}})
	}
	p := components.DataTableProps{
		ID: "investor-registry", Label: data.Labels.T("screen.admin_investors_title"), URL: data.StoreURL,
		Caption: data.Labels.T("screen.admin_investors_lead"), ColumnsLabel: data.Labels.T("control.columns"),
		Lines: data.TableLines(), Complete: whole, PageSize: 25, FullPager: true, HeaderMenu: true,
		Searchable: whole, SearchPlaceholder: data.Labels.T("control.search_placeholder"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.admin_investors_title")),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.name"), Key: "name", Sortable: whole},
			{Label: data.Labels.T("field.document"), Key: "document"},
			{Label: data.Labels.T("field.person_kind"), Key: "kind", Sortable: whole, Hideable: whole},
			{Label: data.Labels.T("field.email"), Key: "email", Hideable: whole},
			{Label: data.Labels.T("field.user"), Key: "user", Hideable: whole, Hidden: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: whole},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.admin_investors_empty")},
	}
	if whole {
		p.Filters = investor.CompleteFilters(rows, components.DataTableFilter{Key: "status", Label: data.Labels.T("field.status")})
	} else {
		p.Paginator = investor.NextOnly(data.NextURL)
	}
	return p
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
