//go:build kyse

package investor

import (
	"strconv"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// ProjectionData is what the handler hands this page.
type ProjectionData = investor.ProjectionPageData

func projectionTable(data ProjectionData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Costs))
	for i, c := range data.Costs {
		rows = append(rows, components.TableRow{Key: strconv.Itoa(i), Cells: []components.TableCell{
			{Text: c.Name},
			{Text: c.Category},
			{Text: c.Recurrence},
			{Text: c.Scope},
			{Text: c.Amount},
		}})
	}
	return components.DataTableProps{
		ID: "investor-projection", Label: data.Labels.T("screen.projection_costs"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.name"), Key: "name", Sortable: true},
			{Label: data.Labels.T("field.category"), Key: "category", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.recurrence"), Key: "recurrence", Hideable: true},
			{Label: data.Labels.T("field.products"), Key: "scope", Hideable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.projection_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.contract") }} {{ .Contract.Number }} · v{{ .Contract.Version }} · {{ .Contract.Share }}</p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.projection_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.projection_lead") }}</p>
			<p class="mt-2 text-sm font-medium">{{ .PeriodStart }} a {{ .PeriodEnd }}</p>
		</div>
		<nav class="flex gap-2">
			<a class="btn" data-variant="outline" data-size="sm" href="{{ .Contract.StatementURL }}">{{ .Labels.T("control.statement") }}</a>
		</nav>
	</header>

	<dl class="mt-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.gross") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Gross }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.fees") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Fees }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.projection_costs") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .ProjectedCosts }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.ratio") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .Ratio }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.estimated_net") }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ .EstimatedNet }}</dd></div>
		<div class="card border-primary p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.estimated_amount") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .EstimatedAmount }}</dd></div>
	</dl>

	<div class="mt-8">{!! components.DataTable(projectionTable(.)) !!}</div>
	<p class="text-muted-foreground mt-3 text-xs">{{ .Labels.T("screen.estimate_note") }}</p>
</section>
@endsection
