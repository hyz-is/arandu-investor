<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Arandu Investor

Angel-investor revenue sharing for an Arandu application: the investors and
their contracts, the application's money in and out, the costs of running it,
and a monthly closing per contract that pays each investor their share of the
**net** into an [arandu-wallet](https://github.com/hyz-is/arandu-wallet) wallet
they withdraw from by PIX.

It is one install per application. Every investor's contract says what share of
the net they hold and which products' revenue counts; the costs the admin
declares are spread over the revenue of each period, so every line of an
investor's statement carries its part of them.

## How a period closes

A contract closes on its billing day (1–28), once a month. Closing the period
`[start, end)`:

1. **Gross** is every entry of the contract's products dated in the period:
   sales, renewals, refunds (negative) and adjustments.
2. **Fees** are what was taken on the way: the gateway, the invoice issuer,
   taxes, expenses typed by hand.
3. **Costs** are the declared costs that ran in the period — monthly, yearly
   (a twelfth a month) or once, prorated by the days of the billing month —
   each one shared out over the products it applies to by their revenue.
4. **Net** is gross − fees − costs. The cost ratio (costs over gross, in parts
   per million) is what every statement line carries as its part of the costs.
5. The investor's **amount** is the contract's share of the net, rounded to
   the centavo. It is deposited into their `investor` wallet in
   arandu-wallet, idempotently, and becomes an **invoice**.

A period whose net is zero or negative closes with nothing due, and the loss is
**not** carried into the next one, unless the contract asks for it (see
[Carry a loss forward](#carry-a-loss-forward)). An invoice is never summed with the next:
it is withdrawn whole, with no minimum, and becomes *overdue* once a later
period closes while it is still unpaid. An entry that arrives late — dated in a
period already closed — is counted in the next closing, never twice.

Money is `int64` centavos and shares are basis points (1500 = 15%). An
investment withholds no tax; a partnership with a person does (see below).

## Two modes of contract

A contract has a `Kind`, and it says how the person takes part:

| `Kind` | who | what is paid | retention at source |
| --- | --- | --- | --- |
| `investment` (the default) | puts money in and receives a return | the share of the net | none |
| `partnership` | brings work and no money and receives a share of the net | the share of the net, less the retention | INSS and IRRF, when the partner is a person |

Every contract that exists is an investment, an empty `Kind` reads as one, and
nothing changes for an application that never sets it: the migration defaults
the column to `investment`. The screens say the mode ("Investimento",
"Parceria").

Carrying a loss forward, a pay day per contract and a separate product are
options of **both** modes, off by default.

## Partnership: the retention at source

The partner is paid as an autonomous worker (contribuinte individual). The
company that pays holds the INSS and the IRRF back from **the partner's part**
when the invoice is created, and the invoice says how it came out:

| invoice | meaning |
| --- | --- |
| `Amount` | what the partner is paid: the part less the retention. It is what is deposited in the wallet and what the withdrawal asks for |
| `GrossPart()` | the part, the share of the net before the retention |
| `INSSBase`, `INSSRateBps`, `INSSAmount` | the contribution and what it was over |
| `IRRFBase`, `IRRFRateBps`, `IRRFReduction`, `IRRFAmount` | the income tax, the month's base, the bracket's rate, the reduction of Lei 15.270/2025 and what this payment holds |
| `WithholdingTable`, `WithholdingMonth` | the table of rates used and the calendar month the tax is over |

The retention is a discount on the part. It is never a cost of the base: the
net, the costs and the other investors' figures do not move by it. Only a
partnership with an investor who is a **person** holds anything back; a
partner that is a company is paid the whole part. An investor's dependents
(`CreateRequest.Dependents`, `UpdateRequest.Dependents`) each deduct from the
income tax base.

How it is computed, with the figures of 2026:

- **INSS** is 11% of what is paid in the month, up to the ceiling of the
  salary of contribution, R$ 8.475,55. Payments of the same month to the same
  partner count together toward the ceiling: the month's contribution is rounded
  once and each payment holds the difference.
- **IRRF** follows the monthly table over the month's income less the larger
  of the legal deductions (the INSS and R$ 189,59 per dependent) and the
  simplified discount of R$ 607,20, then the reduction of Lei 15.270/2025 (up
  to R$ 312,89 on income up to R$ 5.000,00, and R$ 978,62 less 0,133145 of the
  income up to R$ 7.350,00). Like the INSS, it is computed over the month's
  payments together, and a payment holds the difference.
- Amounts are rounded half away from zero to the centavo, once per figure.

The sources, which the tables in `withholding.go` repeat:

- Teto do salário de contribuição, R$ 8.475,55: Portaria Interministerial
  MPS/MF nº 13, de 9 de janeiro de 2026, art. 2.
- 11% held by the company from a contribuinte individual: Lei 10.666/2003,
  art. 4º, and Lei 8.212/1991, art. 30.
- Monthly table, R$ 189,59 per dependent, R$ 607,20 simplified discount and the
  reduction of Lei 15.270/2025: Receita Federal, "Tabelas" of 2026,
  https://www.gov.br/receitafederal/pt-br/assuntos/meu-imposto-de-renda/tabelas/2026.

**An accountant has to confirm these rules and figures before the first real
payment.** The package computes what the tables say; it does not decide whether
a partner may take the simplified discount, whether the INSS of another source
already reached the ceiling in the month (only this package's payments are
counted), what the DARF and the GPS are paid with, or how the municipal ISS
applies. **The income tax on the return of an investment (the regressive table
of the angel investor) is not computed**: it is left for the accountant to
evaluate, and the package withholds nothing from an investment.

The rates are in tables kept by the day they take effect
(`WithholdingTable`, `NewBrazilWithholder`). The one this release carries is
2026's, and a table stays in force until a later one replaces it, so the first
closing of a new year is computed with the last table known; each invoice says
which one it used (`WithholdingTable`). Upgrade in January, or pass a
`Withholder` of the application's own in `Config.Withholder`.

### RPA and the income report

`Service().InvoiceRPA` (and `GET .../invoices/{id}/rpa`) writes the monthly
receipt of the payment to an autonomous worker for an invoice that held a
retention: payer, beneficiary, period, part, INSS and IRRF with how each came
out, and what was paid. The file is kept on the Disk of `Config.Files`, like
the signed contracts, under the invoice and the language, so the next request
reads what the first wrote. The yearly income report lists what was credited,
withheld (INSS and IRRF) and paid, month by month, by the month the amount was
credited; the monthly report gets the columns of the retention when an invoice
of the month has one.

## Carry a loss forward

`ContractRequest.CarryLoss` makes a period whose net is negative keep the loss
(`Invoice.CarriedOut`) and take it off the net of the next periods
(`Invoice.CarriedIn`), before the share, until it is used up. With a net of
-R$ 1.000, then R$ 500, then R$ 4.000 and a share of 15%, the invoices pay 0, 0
and 15% of R$ 3.500. The statement and the projection show the loss the open
period starts with. Off, a losing period pays nothing and carries nothing.

## A pay day per contract

`ContractRequest.PayDay` (1–28) is the day of the month the partner is paid.
The summary shows the earliest of the investor's contracts in force from today
(`Service().NextPayoutOf`); a contract with none leaves it to
`Config.NextPayout`, the application's day, as before.

## Keep a product apart

`Service().SeparateProduct(ctx, actor, "corujao")` marks a product as separate:
a contract over "the whole application" and a cost of "the whole application"
do not cover it. A contract or a cost that names it covers it all the same,
and the product's revenue is still in the books. It is how one application
carries another partner's line without that partner's revenue, costs and share
leaking into the contracts that cover everything else. `JoinProduct` brings it
back and `SeparateProducts` lists them; the change counts from the closings
made after it. Nothing is separate by default.

## Install

It needs arandu-wallet v0.13.0 or newer, which brings the withdrawal requests
an investor is paid through and takes them only through this package:

```bash
go get github.com/hyz-is/arandu-wallet@v0.13.0
go get github.com/hyz-is/arandu-investor
```

## Wire it

An Arandu application registers a module explicitly. There is no service
provider, no container and no discovery, so these are the lines to paste into
`bootstrap/app.go`, and the views add imports below.

The imports, with the other module imports:

```go
import (
	investor "github.com/hyz-is/arandu-investor"
	wallet "github.com/hyz-is/arandu-wallet"
)
```

The construction, in `App`, once `NewAppServiceProvider` has returned and the
Disk and the wallet module exist. `New` refuses a wiring that cannot work and
`Configure` has no way to return that error, so the module is built where the
error can leave; the database and the session store are the ones the framework
built from the configuration:

```go
	// walletModule, activityModule and investorModule share drivers.DB: the
	// closing, the transfer and the withdrawal write this package's tables and
	// arandu-wallet's in one transaction, and that holds only over one handle.
	investorModule, err := investor.New(investor.Config{
		Files:  contractsDisk,         // the application's filesystem.Disk, local or S3
		Wallet: walletModule.Service(), // the one arandu-wallet instance
		Payer:  investor.Payer{Name: "Empresa Ltda", Document: "00.000.000/0001-00"},
	}, drivers.DB, drivers.Sessions)
	if err != nil {
		_ = drivers.Close()
		return nil, err
	}
```

And the registration, in the `WithProviders(...)` chain the builder is made of:

```go
	return Configure(cfg, drivers, app).
		WithProviders(investorModule).
		WithVersion(version, commit).
		Create()
```

Then, once, before the application serves:

```bash
aru migrate
```

The package owns nine tables, which is why `arandu.mod.toml` says
`migrations = true`. arandu-wallet's own migrations run in the same step.

## Give people access

The policy is in `policy.go`, and roles reach it through the Subject, which
[arandu-permission](https://github.com/hyz-is/arandu-permission) fills:

| role or action | what it opens |
| --- | --- |
| role `investor.admin`, or action `investor.admin` (`AdminAction`) | the registry, contracts, entries, costs, closings, every investor's invoices and reports |
| action `investor.investors.transfer` (`InvestorsTransfer`) | passing an investor to another person, and nothing else; no role carries it |
| role `wallet.operator` | arandu-wallet's operator actions; the admin who pays withdrawals and closes periods needs it too |

The action is what an arandu-permission group carries: mount its `Roles()` in
`Config.Middleware` (or `AdminMiddleware`), so the actions are resolved on each
request, and the people of that group are the admin.

An investor needs no role. A signed-in user whose `user_id` is on an investor's
registration reads that investor's panel, contracts, statements, projection,
invoices and reports, and nobody else's. Anybody else is refused. The tenant of
every statement is the Grant's, never a value from the request.

## Record the application's money

The application's own code — the place its payments are confirmed — records
each movement through the service. Sending the same `ExternalRef` twice records
it once:

```go
	_, err := investorModule.Service().RecordEntry(ctx, actor, investor.EntryRequest{
		ExternalRef: "mp:" + payment.ID,
		Kind:        investor.KindSale, // or KindRenewal, KindRefund, ...
		ProductID:   "placaflow",
		OccurredAt:  payment.ApprovedAt,
		Gross:       19900, // centavos
		Fee:         798,   // what the gateway took
		FeeDetail:   "gateway",
	})
```

An entry is never edited; a wrong one is undone by a reversal. The admin types
what did not come through the application on the books screen.

## Close the periods

The admin closes one contract by hand, or every contract due on a day with
"close the contracts due today". An application that wants it unattended runs
the same call from its scheduler once a day:

```go
	closed, failed, err := investorModule.Service().CloseDue(ctx, operator, investor.DayOf(time.Now(), loc))
```

A contract that fails to close does not stop the others: it is reported in
`failed` and closed on the next run.

## Withdrawals

The investor declares a PIX key or a bank account on their panel — always in
the name and document of their registration — and asks for an invoice
("Solicitar saque"). arandu-wallet holds
the amount. The wallet is opened with `PayoutVia: "investor"`, so a request
that does not come through this package — the wallet's own route — is refused,
and no part of an invoice leaves around these rules. The admin, on the withdrawals screen, takes the request, makes the
PIX outside the system, and records the end-to-end identifier with the receipt
attached; or rejects it with a reason, and the invoice can be asked for again.
The investor downloads the receipt from their panel ("Ver comprovante"). With
`Config.Activity`, every one of these acts is in arandu-activitylog.

## Pass an investor to another person

An investor can be given away, sold or inherited: the registration keeps its
contracts, its entries, its invoices and its closings, and a different person
becomes its owner. The admin's code does it with one call:

```go
	record, err := investorModule.Service().Transfer(ctx, operator, investorID, investor.TransferRequest{
		UserID:      newUser.ID,
		Document:    "529.982.247-25",
		Name:        "Ana Souza",
		Email:       "ana@example.com",
		EffectiveOn: "2026-11-24", // the day it counts from, never a day to come
		Reason:      "gift to the investor's son",
	})
```

The registration's login user, document, names and e-mail become the new
owner's, and what they were is kept in a row of `investor_transfers`
(`Service().Transfers` lists them by the day they took effect). The act is in
arandu-activitylog with the owner before and the owner after, the documents
masked. It needs `InvestorsTransfer`, an action of its own that the admin role
and `AdminAction` do **not** carry: it changes whose money a registration is, so
the application puts it in the group of the few who may (and that person needs
arandu-wallet's operator role too). Everything the transfer decides is read
again after the investor's row is locked, and the act and its entry in the
activity log are one transaction.

The previous owner's payout accounts in arandu-wallet are disabled, so the new
owner declares their own. The transfer is refused, and nothing changes, while
the previous owner's investor wallet still holds money or one of their
withdrawals is open: that money is theirs and is paid first. What the
application grants the previous owner beside the registration, such as a role
that opens the investor's area, is the application's to take away.

## Reports

Each investor has a monthly report and a yearly income report (*informe de
rendimentos*), both `.xlsx`, written with the standard library.

## Screens

Eight screens, in pt-BR and English, built on Kyse components with
`components.DataTable`: the investor's panel, a contract's statement and
projection, and the admin's registry, investor, books, costs and withdrawals.
Every `GET` route answers the screen to a browser and JSON to a client that
asks for it; every form posts to the same routes the JSON API uses.

## Publish the views

Look at what would be written, then write it:

```bash
aru vendor:publish --tag=view
aru vendor:publish --tag=view --apply
```

The files land under `resources/views/modules/investor/`, and from that point
they are yours. Compile them:

```bash
aru view:build
```

and import the two directories it wrote into, with the other imports:

```go
	_ "your/module/path/storage/framework/views/modules/investor"
	_ "your/module/path/storage/framework/views/modules/investor/admin"
```

Without them the views are not in the binary, and the module refuses to boot
rather than answering the first request that reaches one with a 500. The
refusal names the view, the command and the import.

The sentences come from `resources/lang/<locale>/investor.json`, shipped in
English and Brazilian Portuguese: the screens, the data tables (columns,
search, pager), the validation messages, the reports and the activity
descriptions. To change one, put the key in the application's own catalogue
and pass it as `Config.Translator`; it is asked first, key by key.

The language is `Config.Locale` when the application fixes one, else the one
`translation.Middleware` or `translation.InLocale` put on the request, else
English. A product that speaks only Brazilian Portuguese sets it once:

```go
investor.Config{
	Locale:         "pt-BR", // screens, messages, reports, activity log
	TitleSeparator: " — ",   // "Saques dos investidores — <APP_NAME>"
	// ...
}
```

Validation messages are written in English where they are produced -- by this
package, the framework's helpers and arandu-wallet -- so a JSON client reads
them as it always did, and each route draws them again in the request's
language before the router puts them in the flash or in a 422.
`Labels.Message`, `Labels.Errors` and `Labels.Summary` do the same for an
application that calls the service directly. The error banner names each
field by its label ("Chave PIX: Preencha este campo.").

## Configuration

| field | required | meaning |
| --- | --- | --- |
| `Files` | yes | the Disk the signed contracts are kept on, under the tenant's prefix. |
| `Wallet` | yes | arandu-wallet's service. The package keeps no balance of its own. |
| `Withholder` | no | computes the retention at source of a partnership with a person; nil is `DefaultWithholder()`, the tables of 2026 |
| `Prefix` | no | where the routes are mounted. Defaults to `/investor`. |
| `PageSize` | no | rows per page. Defaults to 25, refused above 200. |
| `Location` | no | the timezone days are counted in. Defaults to `America/Sao_Paulo`. |
| `Payer` | no | the paying company, printed on the income report. |
| `Translator` | no | the application's catalogue, asked before the shipped one. |
| `Locale` | no | the language of every screen, message, report and activity description (`"pt-BR"`). Empty follows the request, then English. |
| `TitleSeparator` | no | joins a screen's title to the application's `APP_NAME` in the document title (`" — "` gives "Saques dos investidores — Alpr.sh"). Empty titles a screen with its own title. |
| `Paths` | no | the segments of every route. Empty fields keep `DefaultPaths`; `PortuguesePaths` is the set in pt-BR (`/contratos`, `/faturas/{id}/saque`, `/faturas/{id}/comprovante`, `/conta-de-recebimento`, `/investidores`, `/saques`). |
| `AdminPrefix` | no | where the admin's routes are mounted. Defaults to `Prefix` + `Paths.Admin`. |
| `Middleware`, `AdminMiddleware` | no | run after `RequireAuth`, on every route and on the admin's only. |
| `Chrome` | no | what the application's layout draws around a screen (its sidebar, its menu), carried on the page as `Screen.Chrome()`. |
| `Sections` | no | the pages the application adds to the investor's menu of sections, each after the one it names (`Section.After`); `Module.SectionLinks(ctx, key)` answers the same list for a page the application draws itself. |
| `NextPayout` | no | when the investor is paid next, which only the application knows: the summary says it as a figure of its own. Nil draws what was asked for and is not yet paid. |
| `Activity` | no | arandu-activitylog's logger; every act that changes something is recorded in the log `investor`. |

An application that keeps the investor's screens in its customer panel and the
admin's in its operator console mounts the two halves itself:

```go
investorModule.InvestorRoutes(panel)   // under Prefix, with the panel's guards
investorModule.AdminRoutes(console)    // under AdminPrefix, with the console's
```

The pages embed hesape's `view.Page` and answer `HasErrors` and
`ErrorSummary` too, so they fit the layout of an application on Arandu
Framework v0.62 and of an older one. `Module.URLs()` builds every address, for
an application that links to the screens from its own pages.

`New` returns an error rather than starting half-wired.

## Routes

Every route sits behind `RequireAuth`: a visitor with no session is sent to
`/auth/login`, and a handler reads who is asking with `ctx.User()`. The paths below are the
defaults; `Config.Paths` and `Config.AdminPrefix` move them, and the route names
never change. The policy
then decides; a refusal is 403, a record that is not there 404, a rejected
input goes back with the messages in the flash (422 with the messages keyed by
field to a JSON client).

| method | path | what |
| --- | --- | --- |
| `GET` | `/investor/me` | the summary: what is to receive, what was received, the next payment |
| `GET` | `/investor/me/withdrawals` | the closed periods with the request to withdraw, and the payout accounts |
| `GET` | `/investor/me/contract` | the contract in force and the versions it replaced |
| `GET` | `/investor/me/reports` | the reports to download |
| `GET` | `/investor/me/contracts`, `/me/invoices` | the investor's contracts and invoices, as JSON |
| `POST` | `/investor/me/payout-accounts` | declare a PIX key, or a bank account with `kind=bank` |
| `GET` | `/investor/me/reports/{year}[/{month}]` | income report, monthly report |
| `GET` | `/investor/contracts/{id}/statement`, `/projection`, `/file` | a contract's statement, open period, signed copy |
| `GET` | `/investor/costs` | the declared costs |
| `POST` | `/investor/invoices/{id}/withdrawal` | ask for an invoice to be paid |
| `GET` | `/investor/invoices/{id}/receipt` | the receipt of a paid withdrawal |
| `GET`, `POST` | `/investor/admin/investors` | the registry, register an investor |
| `GET`, `PUT` | `/investor/admin/investors/{id}` | one investor |
| `POST`, `DELETE` | `/investor/admin/investors/{id}/suspension` | suspend, reactivate |
| `GET`, `POST` | `/investor/admin/investors/{id}/contracts` | contracts, register one (multipart, with the PDF) |
| `POST` | `/investor/admin/contracts/{id}/amendments` | register an amendment |
| `POST` | `/investor/admin/contracts/{id}/closings` | close one contract's period |
| `POST` | `/investor/admin/closings/due` | close every contract due on a day |
| `GET`, `POST` | `/investor/admin/entries` | the books, a typed entry |
| `POST` | `/investor/admin/entries/{id}/reversal` | reverse an entry |
| `GET`, `POST` | `/investor/admin/costs` | the costs, declare one |
| `POST` | `/investor/admin/costs/{id}/end` | end a cost |
| `GET` | `/investor/admin/investors/{id}/invoices`, `/reports/{year}[/{month}]` | one investor's invoices and reports |
| `GET` | `/investor/admin/withdrawals` | the withdrawal queue |
| `POST` | `/investor/admin/withdrawals/{id}/processing`, `/payment`, `/rejection` | take, pay (multipart, with the receipt), reject |

## The data path

Each entity embeds `model.Model` and declares its table beside it.
`aru model:build` generates the queries — `InvestorQuery.go` with
`Investors(db)`, and one per entity — which are committed and never edited by
hand; `aru model:build --check` fails when one is out of date.

`InvestorService` owns `*data.DB` and follows
`validate -> security.Authorize -> Grant -> Model terminal` in every exported
method, which the unit suite audits. Handlers never hold the database or start
a query. Every query starts at `Investors(s.db)` or its sibling and reads the
tenant from the Grant through `data.Tenant(g)`.

## Example

```bash
go run -tags example ./example
```

It opens SQLite in a temporary directory, migrates arandu-wallet and this
package, and runs one investor through a whole period: the contract with its
PDF, a sale and a renewal with the gateway's fee, a monthly cost, the closing
on the billing day, and the withdrawal of the invoice by PIX with the receipt.
It leaves nothing behind. The build tag is there so that installing this
package never compiles it.

## Tests

```bash
GOWORK=off go test ./... -race
```

The unit suite audits the data path, holds the policy, checks the
calculation, and builds every published view with the view compiler an
application uses — then renders each screen with sample data. The feature suite
runs the closings, statements, withdrawals and routes against SQLite with
arandu-wallet installed beside it.

## Licence

MIT. See [LICENSE.md](LICENSE.md).
