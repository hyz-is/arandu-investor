//go:build kyse

package investor

import (
	investor "github.com/hyz-is/arandu-investor"
)

@go
// ReportsData is what the handler hands this page.
type ReportsData = investor.ReportsPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.investor") }} · {{ .Investor }} · <span class="font-mono">{{ .Document }}</span></p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.reports_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.reports_lead") }}</p>
		</div>
		{!! SectionMenu(.Screen) !!}
	</header>

	<div class="mt-8 grid gap-6 md:grid-cols-2">
		<div class="card p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.monthly_report") }}</h2>
			@if(len(.ReportMonths) == 0)
				<p class="text-muted-foreground mt-3 text-sm">{{ .Labels.T("screen.reports_empty") }}</p>
			@else
				<ul class="mt-3 flex flex-wrap gap-2">
				@foreach(.ReportMonths as month)
					<li><a class="btn" data-variant="outline" data-size="sm" href="{{ month.URL }}">{{ month.Label }}</a></li>
				@endforeach
				</ul>
			@endif
		</div>
		<div class="card p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.annual_report") }}</h2>
			<p class="text-muted-foreground mt-1 text-sm">{{ .Labels.T("screen.annual_lead") }}</p>
			@if(len(.ReportYears) == 0)
				<p class="text-muted-foreground mt-3 text-sm">{{ .Labels.T("screen.reports_empty") }}</p>
			@else
				<div class="mt-3 flex flex-wrap gap-2">
					@foreach(.ReportYears as year)
						<a class="btn" data-variant="outline" data-size="sm" href="{{ year.URL }}">{{ year.Label }}</a>
					@endforeach
				</div>
			@endif
		</div>
	</div>
</section>
@endsection
