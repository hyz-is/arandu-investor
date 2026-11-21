//go:build kyse

package investor

import (
	"html/template"
	"strconv"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// ContractData is what the handler hands this page.
type ContractData = investor.ContractPageData

func dashboardContracts(data ContractData) components.DataTableProps {
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
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.investor") }} · {{ .Investor }} · <span class="font-mono">{{ .Document }}</span></p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("section.contract") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.contract_lead") }}</p>
		</div>
		{!! SectionMenu(.Screen) !!}
	</header>

	<div class="mt-8">{!! components.DataTable(dashboardContracts(.)) !!}</div>
</section>
@endsection
