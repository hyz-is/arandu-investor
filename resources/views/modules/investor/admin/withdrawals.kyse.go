//go:build kyse

package admin

import (
	"html/template"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// WithdrawalsData is what the handler hands this page.
type WithdrawalsData = investor.AdminWithdrawalsPageData

func withdrawalVariant(key string) string {
	switch key {
	case "requested":
		return ""
	case "rejected", "cancelled":
		return "destructive"
	case "paid":
		return "outline"
	}
	return "secondary"
}

// pendingTable is the operator's work: the requests waiting to be taken or
// paid, each with what can be done to it. Taking one starts its payment; a
// request being paid is marked paid with the end-to-end identifier and the
// receipt; either can be rejected with a reason the investor reads.
func pendingTable(data WithdrawalsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, w := range data.Rows {
		if !w.Open && !w.Processing {
			continue
		}
		action := template.HTML("")
		if w.Open {
			action += data.CellForm(w.ProcessURL, data.Labels.T("control.process"), "")
		}
		if w.Processing {
			action += data.PayForm(w.PayURL)
		}
		action += data.CellForm(w.RejectURL, data.Labels.T("control.reject"), "destructive",
			investor.CellInput{Name: "reason", Placeholder: data.Labels.T("field.reason"), Label: data.Labels.T("field.reason"), Required: true})
		rows = append(rows, components.TableRow{Key: w.ID, Cells: []components.TableCell{
			{Text: w.RequestedAt},
			{Text: w.Investor},
			{Text: w.Amount},
			{Text: w.Account},
			{HTML: components.Badge(components.BadgeProps{Label: w.Status, Variant: withdrawalVariant(w.StatusKey)}), SortValue: w.StatusKey},
			{HTML: template.HTML(`<div class="grid gap-2">`) + action + template.HTML(`</div>`)},
		}})
	}
	return components.DataTableProps{
		ID: "investor-withdrawals-pending", Label: data.Labels.T("screen.pending_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 25,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.requested_at"), Key: "requested_at", Sortable: true},
			{Label: data.Labels.T("noun.investor"), Key: "investor", Sortable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.account"), Key: "account"},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: data.Labels.T("field.actions"), Key: "actions"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.pending_empty")},
	}
}

func withdrawalsTable(data WithdrawalsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, w := range data.Rows {
		status := components.Badge(components.BadgeProps{Label: w.Status, Variant: withdrawalVariant(w.StatusKey)})
		if w.ReceiptURL != "" {
			status += " " + components.Link(components.LinkProps{Label: data.Labels.T("control.view_receipt"), URL: w.ReceiptURL, Variant: "hover"})
		}
		rows = append(rows, components.TableRow{Key: w.ID, Cells: []components.TableCell{
			{Text: w.RequestedAt},
			{Text: w.Investor},
			{Text: w.Amount},
			{Text: w.Account},
			{HTML: status, SortValue: w.StatusKey},
			{Text: w.Reason},
		}})
	}
	var selected []string
	if data.Status != "" {
		selected = []string{data.Status}
	}
	return components.DataTableProps{
		ID: "investor-withdrawals", Label: data.Labels.T("screen.admin_withdrawals_title"), URL: data.URL,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 25,
		SearchName: "q", SearchPlaceholder: data.Labels.T("control.search_placeholder"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.admin_withdrawals_title")),
		Filters: []components.DataTableFilter{{Key: "status", Label: data.Labels.T("field.status"), Options: data.WithdrawalStatusOptions(), Selected: selected}},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.requested_at"), Key: "requested_at", Sortable: true},
			{Label: data.Labels.T("noun.investor"), Key: "investor", Sortable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.account"), Key: "account", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.withdrawals_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("withdrawals") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_withdrawals_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_withdrawals_lead") }}</p>
	</header>

	<div class="mt-6">{!! components.DataTable(pendingTable(.)) !!}</div>

	<div class="mt-8">{!! components.DataTable(withdrawalsTable(.)) !!}</div>
</section>
@endsection
