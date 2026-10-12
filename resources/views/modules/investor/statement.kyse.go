//go:build kyse

package investor

import (
	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// StatementData is what the handler hands this page.
type StatementData = investor.StatementPageData

func statementTable(data StatementData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		cost := r.Cost
		if r.Estimate {
			cost += " *"
		}
		description := r.Description
		if r.FeeDetail != "" {
			description += " (" + r.FeeDetail + ")"
		}
		rows = append(rows, components.TableRow{Key: r.ID, Cells: []components.TableCell{
			{Text: r.Day, SortValue: r.DaySortValue},
			{Text: r.Kind},
			{Text: r.Product},
			{Text: description},
			{Text: r.Gross, SortValue: r.GrossSort},
			{Text: r.Fee, SortValue: r.FeeSort},
			{Text: cost, SortValue: r.CostSort},
			{Text: r.Net, SortValue: r.NetSort},
		}})
	}
	return components.DataTableProps{
		ID: "investor-statement", Label: data.Labels.T("screen.statement_title"), URL: data.URL,
		Caption: data.Labels.T("screen.statement_lead"), ColumnsLabel: data.Labels.T("control.columns"),
		Lines: data.TableLines(), Complete: true, PageSize: 50, PageSizes: []int{25, 50, 100, 500},
		SearchName: "q", SearchPlaceholder: data.Labels.T("control.search"),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.occurred_on"), Key: "day", Sortable: true},
			{Label: data.Labels.T("field.kind"), Key: "kind", Sortable: true},
			{Label: data.Labels.T("field.product"), Key: "product", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.description"), Key: "description", Hideable: true},
			{Label: data.Labels.T("field.gross"), Key: "gross", Sortable: true, Align: "right"},
			{Label: data.Labels.T("field.fee"), Key: "fee", Sortable: true, Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.cost_share"), Key: "cost", Sortable: true, Hideable: true, Align: "right"},
			{Label: data.Labels.T("field.line_net"), Key: "net", Sortable: true, Align: "right"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.statement_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.contract") }} {{ .Contract.Number }} · v{{ .Contract.Version }} · {{ .Contract.Share }}</p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.statement_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.statement_lead") }}</p>
		</div>
		<nav class="flex gap-2">
			<a class="btn" data-variant="outline" data-size="sm" href="{{ .Contract.ProjectionURL }}">{{ .Labels.T("control.projection") }}</a>
		</nav>
	</header>

	<form class="card mt-6 flex flex-wrap items-end gap-3 p-4" method="get" action="{{ .URL }}">
		<div>
			{!! components.Field(components.FieldProps{Name: "from", Label: .Labels.T("field.from"), Type: "date", Value: .From}) !!}
		</div>
		<div>
			{!! components.Field(components.FieldProps{Name: "to", Label: .Labels.T("field.to"), Type: "date", Value: .To}) !!}
		</div>
		<div class="min-w-44">
			{!! components.Select(components.SelectProps{
				Name: "kind", Label: .Labels.T("field.kind"), Value: .Kind,
				Options: .KindOptions(), Placeholder: .Labels.T("control.all"),
			}) !!}
		</div>
		<div>
			{!! components.Field(components.FieldProps{Name: "product", Label: .Labels.T("field.product"), Value: .Product}) !!}
		</div>
		{!! components.Button(components.ButtonProps{Label: .Labels.T("control.filter"), Type: "submit", Variant: "outline"}) !!}
	</form>

	<dl class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.gross") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Gross }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.fees") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Fees }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.costs") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Costs }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.net") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Net }}</dd></div>
	</dl>

	<div class="mt-6">{!! components.DataTable(statementTable(.)) !!}</div>

	@if(.HasEstimates)
		<p class="text-muted-foreground mt-3 text-xs">* {{ .Labels.T("screen.estimate_note") }}</p>
	@endif
</section>
@endsection
