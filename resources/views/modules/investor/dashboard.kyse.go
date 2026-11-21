//go:build kyse

package investor

import (
	"html/template"

	"github.com/arandu-io/kyse/components"

	investor "github.com/hyz-is/arandu-investor"
)

@go
// DashboardData is what the handler hands this page.
//
// It is an alias rather than a struct declared here, so the shape is written
// once, beside the code that fills it.
type DashboardData = investor.DashboardPageData

// SectionMenu is the menu of the investor's area: a button that says which
// section the page belongs to, and the list of every section it opens. Every
// page of the area draws it in its header; a page the application owns draws
// the same list from Screen.Sections.
func SectionMenu(s investor.Screen) template.HTML {
	if len(s.Sections) == 0 {
		return ""
	}
	items := make([]components.MenuItem, 0, len(s.Sections))
	for _, section := range s.Sections {
		items = append(items, components.MenuItem{Label: section.Label, URL: section.URL})
	}
	return components.DropdownMenu(components.DropdownMenuProps{
		ID: "investor-sections", Label: s.CurrentSection(), Variant: "outline", Align: "end", Items: items,
	})
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.investor") }} · {{ .Investor }} · <span class="font-mono">{{ .Document }}</span></p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.dashboard_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.summary_lead") }}</p>
		</div>
		{!! SectionMenu(.Screen) !!}
	</header>

	<dl class="mt-8 grid gap-3 sm:grid-cols-3">
		<div class="card p-4">
			<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.to_receive") }}</dt>
			<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Available }}</dd>
			@if(.WithdrawableCount > 0)
				<dd class="mt-1 text-sm"><a class="underline underline-offset-4" href="{{ .URLs.MyWithdrawals() }}#saque">{{ .Labels.T("screen.withdraw_now") }}</a></dd>
			@else
				<dd class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.to_receive_none") }}</dd>
			@endif
		</div>
		<div class="card p-4">
			<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.received") }}</dt>
			<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .WithdrawnTotal }}</dd>
			<dd class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.received_note") }}</dd>
		</div>
		@if(.NextPayout != "")
			<div class="card p-4">
				<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.next_payout") }}</dt>
				<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .NextPayout }}</dd>
				<dd class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.next_payout_note") }}</dd>
			</div>
		@else
			<div class="card p-4">
				<dt class="text-muted-foreground text-xs">{{ .Labels.T("screen.in_payment") }}</dt>
				<dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Held }}</dd>
				<dd class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.in_payment_note") }}</dd>
			</div>
		@endif
	</dl>
</section>
@endsection
