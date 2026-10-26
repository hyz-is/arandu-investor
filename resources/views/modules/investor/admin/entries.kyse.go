//go:build kyse

package admin

import (
	"html/template"

	"github.com/arandu-io/hesape/pagination"
	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// EntriesData is what the handler hands this page.
type EntriesData = investor.AdminEntriesPageData

// entriesState is what the package's handler read from the address and handed
// on, as the table's state: the search, the order, the kind, the page, the page
// size and the columns the reader hid. Every control the table draws is an
// address this state builds, so a sort keeps the search and a page keeps both.
func entriesState(data EntriesData) components.DataTableState {
	state := components.DataTableValues{
		Q: data.Query, Sort: data.Sort, Dir: data.Dir, CurrentPage: data.CurrentPage,
		DefaultSize: entriesSize(data), Size: entriesSize(data), Hide: data.Hide,
	}
	if data.Kind != "" {
		state.Facets = map[string][]string{"kind": {data.Kind}}
	}
	return state
}

// entriesSize is how many rows a page of the books holds, the package's own
// default when the handler did not say.
func entriesSize(data EntriesData) int {
	if data.PageSize < 1 {
		return investor.DefaultPageSize
	}
	return data.PageSize
}

// entriesTable is the books, paged, searched, filtered and ordered by the
// server: the rows are a window onto a list that only grows. The package reads
// its own page size, so the table offers no chooser.
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
	return components.DataTableProps{
		ID: "investor-books", Label: data.Labels.T("screen.admin_books_title"), URL: data.URL,
		Caption: data.Labels.T("screen.admin_books_lead"), ColumnsLabel: data.Labels.T("control.columns"),
		Lines: data.TableLines(), FullPager: true, HeaderMenu: true,
		State: entriesState(data),
		Paginator: pagination.NewLengthAwarePage(len(data.Rows), data.Total, entriesSize(data), data.CurrentPage,
			pagination.Options{Path: data.URL}),
		Searchable: true, SearchPlaceholder: data.Labels.T("screen.books_search"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.admin_books_title")),
		Filters:     []components.DataTableFilter{{Key: "kind", Label: data.Labels.T("field.kind"), Options: data.KindOptions()}},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.occurred_on"), Key: "occurred_on", Sortable: true},
			{Label: data.Labels.T("field.kind"), Key: "kind", Sortable: true},
			{Label: data.Labels.T("field.product"), Key: "product", Sortable: true},
			{Label: data.Labels.T("field.description"), Key: "description", Hideable: true},
			{Label: data.Labels.T("field.gross"), Key: "gross", Sortable: true, Align: "end"},
			{Label: data.Labels.T("field.fee"), Key: "fee", Hideable: true, Align: "end"},
			{Label: data.Labels.T("field.net"), Key: "net", Align: "end"},
			{Label: data.Labels.T("field.source"), Key: "source", Hideable: true},
			{Label: data.Labels.T("field.external_ref"), Key: "ref", Hideable: true},
			{Label: data.Labels.T("field.actions"), Key: "reverse"},
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
