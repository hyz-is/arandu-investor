# Upgrade Guide

## Unreleased

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
