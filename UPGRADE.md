# Upgrade Guide

## v0.9.2

No migration, no new address and no removed symbol. Two things need a look.

- **The transfer needs its own permission.** `InvestorsTransfer`
  (`investor.investors.transfer`) is no longer opened by the admin role or by
  `AdminAction`. An application that calls `Service().Transfer` (a command, a
  screen) grants the action to the accounts that may: in arandu-permission, put
  `investor.investors.transfer` in a group of its own, and mount `Roles()` in
  `Config.Middleware` (or give the subject the action another way). Without it
  the transfer answers `security.ErrForbidden`. The subject also needs
  arandu-wallet's operator role, as before. A test that lists the admin's
  actions drops this one from the list.
- **One database handle.** arandu-wallet, arandu-activitylog and this package
  have to be built over the same `*data.DB`; over three, the closing and the
  transfer are not atomic with the wallet and the log.

Invoices paid before this version keep reading as paid for their owner, from
the withdrawal request, as they did. A transfer made with this version marks the
registration's paid invoices `withdrawn`; a registration already transferred by
0.9.0 or 0.9.1 with a paid invoice should be checked by hand: the new owner may
see that invoice as available, and it must not be withdrawn.

## v0.9.1

Run `aru migrate`: one migration widens the date-time columns of
`investor_transfers` to microseconds. No code change, no address, table or
exported function changes. An application that lists the tables whose precision
a test checks (`TestEveryOwnTableIsWidened` in Peráta) needs nothing for it: the
module widens its own.

## v0.9.0

Run `aru migrate`: one migration creates `investor_transfers`. Nothing is
removed and no address changes.

`InvestorService.Transfer` and `Transfers` are new, behind the new action
`InvestorsTransfer` (`investor.investors.transfer`). The admin role and the
admin action open it, so an application whose arandu-permission group carries
`investor.admin` needs nothing. An application that lists every action of this
package in a test adds the new one. A transfer changes the registration only:
the role or group the application gave the previous owner to open the
investor's area is the application's to take away.

## v0.8.1

No migration and no code change. The registry's ordering by type and by status
works, and an unknown ordering is a refused request.

## v0.8.0

The investor's panel is a handful of pages and `dashboard` is only the summary,
so an application that published the views before has to take the new ones.
Nothing is removed from the routes, the tables or the exported functions an
application calls; what changes is the page data and the published views.

`go get github.com/hyz-is/arandu-investor@v0.8.0`, then:

- `aru vendor:publish --tag=view --apply` writes the three new views
  (`modules/investor/withdrawals`, `contract` and `reports`) and, for an
  application that never edited them, the new `dashboard`, `statement` and
  `projection`. Run `aru view:build`.
- An application that **edited** `dashboard.kyse.go` ports its copy by hand:
  its helpers for the withdrawals, the accounts, the contract and the reports
  move to the file of the page that draws them (the module's `withdrawals`,
  `contract` and `reports` show where), and each page draws the menu in its
  header with `SectionMenu(.Screen)` or its own component over `.Sections`. The
  build says what is left in the wrong file: a helper declared twice, or a field
  of `DashboardPageData` that moved to `WithdrawalsPageData`, `ContractPageData`
  or `ReportsPageData` (`Contracts`, `Invoices`, `Accounts`, `ReportMonths`,
  `ReportYears`, `ReportsURL`, `PixURL`, `PayoutAccountsURL`).
- An application that mounts a page of its own in the area (a Faturamento next
  to the summary) passes it as `Config.Sections` and draws the menu on it from
  `Module.SectionLinks(ctx, key)`; `Config.NextPayout` gives the summary the
  day the team pays.
- An application that sets `Paths.Me` to something other than `/` and links to
  the panel from its own pages finds the new pages under it (`Me` +
  `Withdrawals`, `Contract`, `Reports`); `Module.URLs().MyWithdrawals()`,
  `MyContract()` and `MyReports()` build them.
- An application that overrides catalogue lines in its own translator keeps its
  lines; the new ones (`section.*`, `screen.to_receive`, `screen.received`,
  `screen.next_payout`, ...) are answered by the shipped catalogue until it
  writes its own.

## v0.7.1

Run `aru migrate`: one migration widens the module's date-time columns to
microseconds where they were created in whole seconds. No code change, no
address, table or exported function changes. An application that lists the
tables whose precision a test checks (`TestEveryOwnTableIsWidened` in Peráta)
needs nothing for these: the module widens its own.

## v0.7.0

Adds the cost recurrence `MonthlyCharged` (`"monthly_charged"`). Nothing is
removed and no table changes: `investor_cost_items.recurrence` is a string
column that already holds the new value. An application that edited the
published costs view and wants the new choice offered adds `monthly_charged` to
its recurrence select (`Screen.RecurrenceOptions` already lists it), and its
`resources/lang/<locale>/investor.json` gets `recurrence.monthly_charged` and
`validation.charge_day`; a missing line is answered in English by the shipped
catalogue.

## v0.6.2

A contract amended to start earlier is closed from the earliest start of its
versions. Nothing to change in an application; a period that closed nothing
because of the amendment is closed by the next closing.

## v0.5.0

The published views are drawn with the Kyse `v0.45.0` data table, so this
release requires Kyse `v0.45.0`, Framework `v0.67.0` and Hesape `v0.88.0` (the
view layer's script that makes the table's menus, selection and swap work is
Hesape's from `v0.87.0`). Read Kyse's `UPGRADE.md` for `v0.45.0` first: only the
lists drawn with `DataTable` change. No route, address, table or exported
function of this package is removed.

`go get github.com/hyz-is/arandu-investor@<tag>`, then:

- An application that **never edited** the published views republishes them
  with `aru vendor:publish --tag=view --apply` and runs `aru view:build`.
- An application that **edited** them must port its copy; republishing would
  overwrite it. The props the new table no longer has, and what replaces them
  (the same table as Kyse's guide lists):
  - `SearchName: "q"` becomes `Searchable: true`; the text is the state's `Q`.
  - `Query`, `SortKey`, `SortDir`, `Page`, `Pages` and `Total` become
    `State: components.DataTableValues{Q: ..., Sort: ..., Dir: ..., CurrentPage: ..., Size: ..., DefaultSize: ...}`
    and `Paginator: pagination.NewLengthAwarePage(rows, total, size, page, pagination.Options{Path: url})`
    (the admin's books: `AdminEntriesPageData` now carries `PageSize` and
    `Hide` for it).
  - `DataTableFilter.Selected` becomes `State.Facets`
    (`map[string][]string{"kind": {data.Kind}}`); on a list that holds all its
    rows, give each `TableRow` its `Facets` and the table its filters with
    `investor.CompleteFilters(rows, ...)`.
  - `Align: "right"` becomes `Align: "end"`.
  - Add `FullPager: true` and `HeaderMenu: true` for the shadcn look, `URL` on
    a `Complete` list (the page's `.Path`), and `Hideable: true` on the columns
    the reader may switch off. A column's `Hidden: true` only applies to a list
    with no `State`; the books no longer start with Source and External
    reference hidden.
  - The admin's registry draws its next-page step with
    `Paginator: investor.NextOnly(data.NextURL)`; delete the button the view
    drew under the table.
- The catalogue: an application that overrides `lang/<locale>/investor.json`
  drops `table.showing`, `table.per_page` and `table.page` and adds the new
  `table.*` lines (see the changelog); a missing line is answered in English by
  Kyse, and a test of this package fails a locale that lacks one.
- A stylesheet needs nothing of the data table beyond what `aru view:build`
  compiles; the `data-table.css` rules and the `.table-columns`, `.table-count`
  and `.table-sort` rules Basecoat shipped for the old markup can go.
- Tests that read the table's HTML by Basecoat classes break; read it by role,
  label or `data-part`/`data-*` attributes (`data-total`, `data-sort-key`,
  `aria-sort`), which the table keeps.

## v0.4.0

Go 1.26.10 is the floor: `go.mod` declares `go 1.26.10`, because every
vulnerability an Arandu application reaches is in the standard library and is
fixed there (crypto/x509, net/http HTTP/2 and CONNECT, html/template,
net/textproto, crypto/tls). A build on an older toolchain fetches 1.26.10 or
fails when `GOTOOLCHAIN=local`.

This release selects Framework v0.66.0, Hesape v0.85.0, arandu-wallet v0.20.0
and arandu-activitylog v0.6.0; read their upgrade guides from the versions the
application required before (Framework v0.62.1, Hesape v0.78.0,
arandu-wallet v0.17.0 and arandu-activitylog v0.4.0 in v0.3.0). No symbol,
route, table or view of this package changes.

`InvestorPolicy` implements `auth.Writes`: a Grant `security.Authorize` issues
for `DashboardView` or `ContractView` only reads, so the database refuses any
insert, update or delete made with it (`auth.ErrReadGrant`). Nothing in this
package writes with such a Grant; an application that reused one to write its
own rows gets the refusal and authorizes that write for an action that writes.
`ReportsView` and the management actions still issue Grants that write.

## v0.3.0

`go get github.com/hyz-is/arandu-investor@v0.3.0`, then republish the views
with `aru vendor:publish --tag=view --apply` and run `aru view:build`: the
dashboard, the statement and every table with a search box changed. Merge the
new markup into views you edited (`.ReportMonths`, `.ReportYears`, `c.Period`,
`SearchLabel: data.SearchLabel(...)`, the statement's filter grid).

- `FallbackLocale` changed value from `"pt-BR"` to `"en"`. An application that
  relied on the screens being in Portuguese without saying so sets
  `Config.Locale: "pt-BR"`; one that negotiates the locale per request keeps
  working as it did.
- To title the screens "Título — <APP_NAME>", set `Config.TitleSeparator`
  (`" — "`). The name is the one the Application puts on the request; nothing
  else is configured.
- Nothing else is incompatible: route names, addresses and tables are
  v0.2.0's. English validation messages are unchanged, except the two
  `status` messages and the `entry` message named in the changelog, which no
  longer carry an identifier.

## v0.2.0

Require Framework v0.62.1 and Hesape v0.78.0 (`go get` them with this
release), then republish the views with `aru vendor:publish --tag=view
--apply` and run `aru view:build`: the dashboard and the admin's withdrawals
changed.

- `Screen.Page` is `hesape/view.Page` now. A published view or a layout that
  named `framework/view.Page`, or called `HasErrors`/`ErrorSummary` on it,
  keeps working through `Screen`; code that built a `Screen` by hand passes a
  `hesape/view.Page` (`view.New(ctx, title)` from `hesape/view`).
- `FormState` embeds `hesape/view.Page`.
- Nothing moved: route names and the default addresses are v0.1.1's. To serve
  the routes in Portuguese, set `Config.Paths: investor.PortuguesePaths`.
- To keep the admin out of the investors' side, set `Config.AdminPrefix` and
  mount `Module.InvestorRoutes` and `Module.AdminRoutes` in their own groups
  instead of `Module.Routes`.
- What `apidiff` reports as incompatible, and what to write instead:
  - `Screen.Page`, `FormState.Page`, `DashboardPageData.Page`,
    `StatementPageData.Page`, `ProjectionPageData.Page`,
    `AdminInvestorsPageData.Page`, `AdminInvestorPageData.Page`,
    `AdminCostsPageData.Page`, `AdminEntriesPageData.Page` and
    `AdminWithdrawalsPageData.Page` are `hesape/view.Page`; build them with
    hesape's `view.New`.
  - `github.com/arandu-io/framework/view.Page.WithToken` promoted through those
    pages now answers `hesape/view.Page`.
  - `Config` and `Module` are no longer comparable: `Config` holds the
    middleware and the chrome function. Compare their fields, never the values.
  - `New` and `(*Module).Routes` keep their signatures; the framework types in
    them are the v0.62.1 ones.
- An admin may now hold `investor.AdminAction` through arandu-permission
  instead of the role `investor.admin`. arandu-wallet still asks for its role
  `wallet.operator` to pay a withdrawal or deposit an invoice.

## v0.1.1

Run `aru migrate` for arandu-wallet v0.13.0's `20261013_0018_add_wallet_payout_via`.
An investor's wallet opened by v0.1.0 has an empty `payout_via` and still takes
requests from the wallet's own route; set it once:
`UPDATE wallets SET payout_via = 'investor' WHERE slug = 'investor';`

## v0.1.0

The first release. Install arandu-wallet v0.12.0 or newer beside it, wire both
in `bootstrap/app.go` as the README shows, run `aru migrate`, publish the views
with `aru vendor:publish --tag=view --apply`, run `aru view:build`, and import
the two view directories it writes. Give the admin the roles `investor.admin`
and `wallet.operator` through arandu-permission.
