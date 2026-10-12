//go:build kyse

package admin

import (
	"html/template"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// EntriesData is what the handler hands this page.
type EntriesData = investor.AdminEntriesPageData

// entriesTable is the books, paged, searched, filtered and ordered by the
// server: the rows are a window onto a list that only grows.
func entriesTable(data EntriesData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, e := range data.Rows {
		reverse := template.HTML("")
		if e.Reversible {
			reverse = data.CellForm(e.ReverseURL, data.Labels.T("control.reverse"), "outline",
				investor.CellInput{Name: "reason", Placeholder: data.Labels.T("field.reason"), Label: data.Labels.T("field.reason"), Required: true})
		}
		rows = append(rows, components.TableRow{Key: e.ID, Cells: []components.TableCell{
			{Text: e.Day},
			{Text: e.Kind},
			{Text: e.Product},
			{Text: e.Description},
			{Text: e.Gross},
			{Text: e.Fee},
			{Text: e.Net},
			{Text: e.Source},
			{Text: e.Ref},
			{HTML: reverse},
		}})
	}
	var selected []string
	if data.Kind != "" {
		selected = []string{data.Kind}
	}
	return components.DataTableProps{
		ID: "investor-books", Label: data.Labels.T("screen.admin_books_title"), URL: data.URL,
		Caption: data.Labels.T("screen.admin_books_lead"), ColumnsLabel: data.Labels.T("control.columns"),
		Lines: data.TableLines(),
		Query: data.Query, SearchName: "q", SearchPlaceholder: data.Labels.T("screen.books_search"),
		Filters: []components.DataTableFilter{{Key: "kind", Label: data.Labels.T("field.kind"), Options: data.KindOptions(), Selected: selected}},
		SortKey: data.Sort, SortDir: data.Dir,
		Page: data.CurrentPage, Pages: data.Pages, Total: data.Total,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.occurred_on"), Key: "occurred_on", Sortable: true},
			{Label: data.Labels.T("field.kind"), Key: "kind", Sortable: true},
			{Label: data.Labels.T("field.product"), Key: "product", Sortable: true},
			{Label: data.Labels.T("field.description"), Key: "description", Hideable: true},
			{Label: data.Labels.T("field.gross"), Key: "gross", Sortable: true, Align: "right"},
			{Label: data.Labels.T("field.fee"), Key: "fee", Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.net"), Key: "net", Align: "right"},
			{Label: data.Labels.T("field.source"), Key: "source", Hideable: true, Hidden: true},
			{Label: data.Labels.T("field.external_ref"), Key: "ref", Hideable: true, Hidden: true},
			{Label: "", Key: "reverse"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.entries_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("entries") !!}

	<header class="mt-6 flex flex-wrap items-end justify-between gap-4">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_books_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_books_lead") }}</p>
		</div>
		<form class="flex flex-wrap items-end gap-2" method="post" action="{{ .CloseDueURL }}">
			@csrf
			{!! components.Field(components.FieldProps{Name: "as_of", Label: .Labels.T("field.as_of"), Type: "date", Value: .Today, Required: true}) !!}
			{!! components.Button(components.ButtonProps{Label: .Labels.T("screen.close_due"), Type: "submit"}) !!}
		</form>
	</header>

	<div class="mt-6">{!! components.DataTable(entriesTable(.)) !!}</div>

	<form class="card mt-8 grid gap-3 p-5" method="post" action="{{ .StoreURL }}">
		@csrf
		<h2 class="text-base font-semibold">{{ .Labels.T("screen.new_entry") }}</h2>
		<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.new_entry_lead") }}</p>
		<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
			{!! components.Select(components.SelectProps{
				Name: "kind", Label: .Labels.T("field.kind"), Options: .ManualKindOptions(), Required: true, Page: .Form(),
			}) !!}
			{!! components.Field(components.FieldProps{Name: "occurred_on", Label: .Labels.T("field.occurred_on"), Type: "date", Value: .Today, Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "product_id", Label: .Labels.T("field.product"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "external_ref", Label: .Labels.T("field.external_ref"), Hint: .Labels.T("screen.external_ref_hint"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "gross_brl", Label: .Labels.T("field.gross_brl"), Placeholder: "0,00", Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "fee_brl", Label: .Labels.T("field.fee_brl"), Placeholder: "0,00", Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "fee_detail", Label: .Labels.T("field.fee_detail"), Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "description", Label: .Labels.T("field.description"), Page: .Form()}) !!}
		</div>
		<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
	</form>
</section>
@endsection
