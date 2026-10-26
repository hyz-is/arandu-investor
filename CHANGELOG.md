# Changelog

Everything worth knowing about a release of Arandu Investor is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [0.5.0] - 2026-10-26

### Changed

- The published views draw their lists with the Kyse `v0.45.0` data table
  (shadcn/ui's): a toolbar with the search box and the faceted filters, the
  Asc, Desc and Hide menu on a sortable header, the columns menu, a footer with
  the row count, the page sizes and the full pager. Require Kyse `v0.45.0` and
  Framework `v0.67.0` with Hesape `v0.88.0` (the view layer's script from
  Hesape `v0.87.0` makes the menus, the selection and the swap work); the
  manifest floor is `framework = ">= 0.67"`.
- The lists that hold all their rows (the investor's contracts, invoices,
  withdrawable invoices and payout accounts, the statement, the projected
  costs, the admin's costs, withdrawals queue and an investor's contracts and
  invoices) are `Complete`: the browser searches, orders, filters and pages
  them, with `PageSize`, and `PageSizes` where a year at a time is useful. The
  admin's costs, registry, withdrawals and the investor's invoices get faceted
  filters (`TableRow.Facets`) drawn from the rows.
- The admin's books are paged, searched, filtered and ordered by the server as
  before, and the table is drawn from one state value
  (`components.DataTableValues`) built from the address, so every control keeps
  the others. The columns Source and External reference are no longer hidden at
  first: with the new state the hidden columns travel in the address (`hide`),
  and a list that starts with columns hidden could not show them again.
- The admin's registry steps to the next page of a list longer than one page
  through the table's own pager (`NextOnly`) instead of a button outside it.
- Numeric columns are aligned with `Align: "end"`; `"right"` is not a value of
  the new table. The empty header cells over the actions are named
  (`field.actions`). The requested-at column of the withdrawals queues orders by
  date (`SortValue`).
- The catalogue's `table` group follows the new table: `showing`, `per_page`
  and `page` are gone; `filter`, `rows`, `selected_count`, `rows_per_page`,
  `first`, `last`, `select_row`, `reset`, `clear_filters`, `no_facet_results`,
  `facet_selected`, `open_menu`, `actions_header`, `toggle_columns`, `asc`,
  `desc` and `hide` are new, in `en` and `pt-BR`. `Screen.TableLines` answers
  the new `components.DataTableLines`.

### Added

- `CompleteFilters`: the faceted filters of a list that holds all its rows,
  built from `TableRow.Facets`, with the count beside each value.
- `NextOnly`: the `components.Paginator` of a list read by cursor.
- `AdminEntriesPageData.PageSize` and `Hide`: the page size the books were
  read with and the hideable columns the address asked to hide
  (`?hide=source`), which the table draws its state from.

## [0.4.0] - 2026-10-25

### Changed

- Require Go `1.26.10`, the release that fixes the standard library
  vulnerabilities a module built on the Arandu kernel reaches (CVE-2026-111943
  in crypto/x509 and the net/http HTTP/2 and CONNECT fixes), Framework
  `v0.66.0` and Hesape `v0.85.0`; the manifest floor is
  `framework = ">= 0.66"`.

## [0.3.0] - 2026-10-23

### Added

- `Config.Locale`: the language every screen, validation message, report and
  activity description is written in, whatever the request negotiated
  (`"pt-BR"`). Empty follows `translation.Middleware` or
  `translation.InLocale`, then English.
- `Config.TitleSeparator`: the document title joins the screen's title to the
  application's `APP_NAME` the request carries ("Saques dos investidores —
  Alpr.sh"). `ChromeRequest.Title` stays the screen's own title.
- Validation messages in the request's language. Every route draws the
  messages of `validation.Errors` -- this package's, the framework helpers'
  ("is required") and arandu-wallet's -- again from the new `validation` group
  of the catalogue before the router answers them. `Labels.Message`,
  `Labels.Errors`, `Labels.Summary`, `Labels.Field`, `Labels.Month` and
  `Lines` for an application that calls the service itself.
- The error banner (`Screen.All`, `Screen.ErrorSummary`) names each field by
  its label in the catalogue: "Chave PIX: Preencha este campo.".
- The reports read the catalogue (`report` and `month` groups), and so do
  their file names; `Screen.SearchLabel` names every table's search box
  ("Buscar em Extrato"), and the search boxes say `control.search_placeholder`
  ("Buscar…").
- `DashboardPageData.ReportMonths` and `ReportYears`, `ReportLink`: the panel
  offers a monthly report only from the month the investor's first contract
  started (the latest 12) and an income report only from that year on; none
  before a contract exists (`screen.reports_empty`).
- `ContractRow.Period`: the contract's period, "20/09/2026 — em aberto" for a
  contract with no end; `ContractRow.Ends` reads `screen.open_ended` instead
  of a dash.
- A complete pt-BR catalogue, with tests that every locale has every key, that
  no pt-BR line is left in English, and that every message the code writes has
  a line.

### Changed

- `FallbackLocale` is `"en"`, as in the other hyz-is modules: a request that
  negotiated no locale is answered in English unless `Config.Locale` says
  otherwise. It was `"pt-BR"`.
- The investor's panel titles itself with `screen.dashboard_title`; the
  investor's name and document moved to the line above it.
- The statement's filters are a grid: one column on a phone, two on a tablet,
  one row on a desktop, every input on the same baseline. The card is a flex
  column in Basecoat, which pushed them to its right.
- Suspending a suspended investor, or reactivating an active one, answers
  "is already suspended" / "is already active" under `status`, without the
  identifier the model's error carried; reversing a reversal answers
  "is itself a reversal; record the original again instead" under `entry`.

## [0.2.0] - 2026-10-23

### Fixed

- Every screen answered 500 under Arandu Framework v0.62 and Hesape v0.78:
  the layouts there assert `hesape/view.Layout` (`Any`, `All`, `Flashed`,
  `FieldError`, `OldOr`), and the pages embedded `framework/view.Page`, which
  answers none of them. `Screen` now embeds `hesape/view.Page`, and answers
  `HasErrors` and `ErrorSummary` as well, so a page fits both contracts.

### Added

- `Config.Paths`, the segments every route is built from, with
  `DefaultPaths` (English, the addresses of v0.1) and `PortuguesePaths`
  (`/contratos`, `/faturas/{id}/saque`, `/faturas/{id}/comprovante`,
  `/conta-de-recebimento`, `/investidores`, `/saques`...); `Config.AdminPrefix`,
  where the admin's routes are mounted when they belong on another side of the
  application.
- `Module.InvestorRoutes` and `Module.AdminRoutes`, each half of `Routes`, for
  an application that mounts the investor's screens in its panel and the
  admin's in its operator console with each side's guards; `Config.Middleware`
  and `Config.AdminMiddleware`, run after `RequireAuth`.
- `Config.Chrome` and `ChromeRequest`: what the application's layout draws
  around a screen, carried on the page as `Screen.Chrome()`.
- `Module.URLs()`, `NewURLs` and `URLs`: every address of the package, for an
  application that links to them; `Screen.URLs`.
- `AdminAction` (`investor.admin`): the admin held as an arandu-permission
  action, opening what `AdminRole` opens; `IsAdmin`.
- Bank accounts beside PIX keys: `AddBankAccount`, `BankAccountRequest`,
  `BankAccountTypes`, `PayoutPix`, `PayoutBank`; the payout account route takes
  `kind=bank` with `bank`, `branch`, `account_number` and `account_type`.
- `Config.Activity`: every act that changes something is recorded in
  arandu-activitylog under the Grant it was authorized with, in the log
  `ActivityLogName` (`investor`), with the events `Event*`.
- The investor's panel lists the payout accounts, the invoices that can be
  asked for with "Solicitar saque", and every withdrawal with its state and
  "Ver comprovante"; the admin's queue is a data table with "Iniciar
  pagamento", "Marcar como pago" with the receipt, and "Rejeitar".
  `Screen.WithdrawForm`, `Screen.PayForm`, `Screen.BankAccountTypeOptions`;
  `DashboardPageData.PayoutAccountsURL`; `AccountOption.Kind`.

### Changed

- Requires Framework v0.62.1, Hesape v0.78.0, Kyse v0.41.0, arandu-wallet
  v0.17.0, arandu-br v0.4.1 and arandu-activitylog v0.4.0; the manifest's
  framework floor is `>= 0.62`.
- `Screen` embeds `hesape/view.Page` instead of `framework/view.Page`, and
  `FormState` likewise. Route names and the default addresses are unchanged.

## [0.1.1] - 2026-10-13

### Fixed

- An investor could ask arandu-wallet's own route for a withdrawal of any
  part of their wallet, around the rule that an invoice is withdrawn whole and
  around the invoice's record of it. The investor's wallet is now opened with
  `PayoutVia: "investor"` (arandu-wallet v0.13.0), and this package asks with
  the same name, so the wallet refuses every request that did not come through
  `WithdrawInvoice`. `PayoutVia` is exported.

### Changed

- Requires arandu-wallet v0.13.0.

## [0.1.0] - 2026-10-12

The first release: angel-investor revenue sharing over arandu-wallet.

### Added

- Investors, natural persons (CPF) and companies (CNPJ), each tied to the
  login user who reads their panel; suspended investors keep their history and
  are left out of new closings.
- Contracts with the share in basis points, the billing day (1–28), the
  products whose revenue counts or the whole application, and the signed PDF
  kept on the application's Disk under the tenant's prefix. Amendments are new
  versions of the same contract; the old one is kept as superseded.
- The books: sales, renewals, refunds, fees, expenses, taxes and adjustments,
  recorded by the application's code (`RecordEntry`, idempotent by external
  reference) or typed by the admin, and undone only by reversals.
- Costs, monthly, yearly or once, for the whole application or some products,
  prorated by the days of the billing month and shared out over the products
  by their revenue.
- The closing of a contract's period on its billing day: gross, fees, costs,
  net and the cost ratio every statement line carries; the investor's share of
  the net deposited in their `investor` wallet in arandu-wallet as an invoice.
  A net of zero or less closes with nothing due and no loss carried over. Late
  entries count in the next closing, never twice. `CloseDue` closes every
  contract due on a day.
- The statement of a contract, with each line's part of the costs, and the
  projection of the open period.
- Withdrawals through arandu-wallet's withdrawal requests: the investor
  declares a PIX key in the name of their registration and asks for an invoice,
  whole; the operator takes it, pays it outside the system and attaches the
  receipt, or rejects it with a reason. Unpaid invoices turn overdue once a
  later period closes.
- The monthly report and the yearly income report as `.xlsx`.
- Eight screens on Kyse components and `components.DataTable` — the investor's
  panel, statement and projection; the admin's registry, investor, books,
  costs and withdrawals — in pt-BR and English, with the application's
  catalogue asked first.
- The actions `InvestorsManage`, `EntriesManage`, `CostsManage`,
  `DashboardView`, `ContractView` and `ReportsView`, and the role
  `investor.admin`.
- The migrations `20261012_0001_create_investors`,
  `20261012_0002_create_investor_contracts`,
  `20261012_0003_create_investor_contract_products`,
  `20261012_0004_create_investor_entries`,
  `20261012_0005_create_investor_cost_items`,
  `20261012_0006_create_investor_cost_item_products`,
  `20261012_0007_create_investor_invoices` and
  `20261012_0008_create_investor_invoice_entries`.

