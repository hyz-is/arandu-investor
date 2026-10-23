//go:build kyse

package admin

import (
	"html/template"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// CostsData is what the handler hands this page.
type CostsData = investor.AdminCostsPageData

func costsTable(data CostsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, c := range data.Rows {
		end := template.HTML("")
		if !c.Ended {
			end = data.CellForm(c.EndURL, data.Labels.T("control.end_cost"), "outline",
				investor.CellInput{Name: "ends_on", Type: "date", Value: data.Today, Label: data.Labels.T("field.ends_on"), Required: true})
		}
		rows = append(rows, components.TableRow{Key: c.ID, Cells: []components.TableCell{
			{Text: c.Name},
			{Text: c.Category},
			{Text: c.Amount},
			{Text: c.Recurrence},
			{Text: c.Scope},
			{Text: c.Starts},
			{Text: c.Ends},
			{HTML: end},
		}})
	}
	return components.DataTableProps{
		ID: "investor-costs", Label: data.Labels.T("screen.admin_costs_title"),
		Caption: data.Labels.T("screen.admin_costs_lead"), ColumnsLabel: data.Labels.T("control.columns"),
		Lines: data.TableLines(), Complete: true, PageSize: 25,
		SearchName: "q", SearchPlaceholder: data.Labels.T("control.search_placeholder"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.admin_costs_title")),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.name"), Key: "name", Sortable: true},
			{Label: data.Labels.T("field.category"), Key: "category", Sortable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.recurrence"), Key: "recurrence", Sortable: true},
			{Label: data.Labels.T("field.products"), Key: "scope", Hideable: true},
			{Label: data.Labels.T("field.starts_on"), Key: "starts", Hideable: true},
			{Label: data.Labels.T("field.ends_on"), Key: "ends", Hideable: true},
			{Label: "", Key: "end"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.admin_costs_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("costs") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_costs_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_costs_lead") }}</p>
	</header>

	<div class="mt-6 grid gap-6 lg:grid-cols-3">
		<div class="lg:col-span-2">{!! components.DataTable(costsTable(.)) !!}</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .StoreURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.new_cost") }}</h2>
			{!! components.Field(components.FieldProps{Name: "name", Label: .Labels.T("field.name"), Placeholder: .Labels.T("screen.cost_name_example"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "category", Label: .Labels.T("field.category"), Placeholder: .Labels.T("screen.cost_category_example"), Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "amount_brl", Label: .Labels.T("field.amount_brl"), Placeholder: "1.250,00", Required: true, Page: .Form()}) !!}
			{!! components.Select(components.SelectProps{
				Name: "recurrence", Label: .Labels.T("field.recurrence"),
				Options: .RecurrenceOptions(), Required: true, Page: .Form(),
			}) !!}
			<div class="grid gap-3 sm:grid-cols-2">
				{!! components.Field(components.FieldProps{Name: "starts_on", Label: .Labels.T("field.starts_on"), Type: "date", Value: .Today, Required: true, Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "ends_on", Label: .Labels.T("field.ends_on"), Type: "date", Page: .Form()}) !!}
			</div>
			<label class="flex items-center gap-2 text-sm"><input type="checkbox" class="input" name="all_products" value="1" checked> {{ .Labels.T("field.all_products") }}</label>
			{!! components.Field(components.FieldProps{Name: "products", Label: .Labels.T("field.products"), Hint: .Labels.T("screen.products_hint"), Page: .Form()}) !!}
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
		</form>
	</div>
</section>
@endsection
