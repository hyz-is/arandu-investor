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
**not** carried into the next one. An invoice is never summed with the next:
it is withdrawn whole, with no minimum, and becomes *overdue* once a later
period closes while it is still unpaid. An entry that arrives late — dated in a
period already closed — is counted in the next closing, never twice.

Money is `int64` centavos, shares are basis points (1500 = 15%), and no
income tax is withheld.

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
`bootstrap/app.go` and there are no others.

```go
import (
	investor "github.com/hyz-is/arandu-investor"
	wallet "github.com/hyz-is/arandu-wallet"
)
```

In `Build`, after the session store, the Disk and the wallet module exist and
before `k.Register`:

```go
	investorModule, err := investor.New(investor.Config{
		Files:  contractsDisk,         // the application's filesystem.Disk, local or S3
		Wallet: walletModule.Service(), // the one arandu-wallet instance
		Payer:  investor.Payer{Name: "Empresa Ltda", Document: "00.000.000/0001-00"},
	}, db, sessions)
	if err != nil {
		return App{}, err
	}
```

And the registration, inside the `k.Register(...)` call already there:

```go
		investorModule,
```

Then, once, before the application serves:

```bash
aru migrate
```

The package owns eight tables, which is why `arandu.mod.toml` says
`migrations = true`. arandu-wallet's own migrations run in the same step.

## Give people access

The policy is in `policy.go`, and roles reach it through the Subject, which
[arandu-permission](https://github.com/hyz-is/arandu-permission) fills:

| role | what it opens |
| --- | --- |
| `investor.admin` | the registry, contracts, entries, costs, closings, every investor's invoices and reports |
| `wallet.operator` | arandu-wallet's operator actions; the admin who pays withdrawals needs it too |

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

The investor declares a PIX key on their panel — always in the name and
document of their registration — and asks for an invoice. arandu-wallet holds
the amount. The wallet is opened with `PayoutVia: "investor"`, so a request
that does not come through this package — the wallet's own route — is refused,
and no part of an invoice leaves around these rules. The admin, on the withdrawals screen, takes the request, makes the
PIX outside the system, and records the end-to-end identifier with the receipt
attached; or rejects it with a reason, and the invoice can be asked for again.
The investor downloads the receipt from their panel.

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

The sentences come from `resources/lang/<locale>/investor.json`. To change one,
put the key in the application's own catalogue and pass it as
`Config.Translator`; it is asked first, key by key.

## Configuration

| field | required | meaning |
| --- | --- | --- |
| `Files` | yes | the Disk the signed contracts are kept on, under the tenant's prefix. |
| `Wallet` | yes | arandu-wallet's service. The package keeps no balance of its own. |
| `Prefix` | no | where the routes are mounted. Defaults to `/investor`. |
| `PageSize` | no | rows per page. Defaults to 25, refused above 200. |
| `Location` | no | the timezone days are counted in. Defaults to `America/Sao_Paulo`. |
| `Payer` | no | the paying company, printed on the income report. |
| `Translator` | no | the application's catalogue, asked before the shipped one. |

`New` returns an error rather than starting half-wired.

## Routes

Every route sits behind `RequireAuth`: a visitor with no session is sent to
`/auth/login`, and a handler reads who is asking with `ctx.User()`. The policy
then decides; a refusal is 403, a record that is not there 404, a rejected
input goes back with the messages in the flash (422 with the messages keyed by
field to a JSON client).

| method | path | what |
| --- | --- | --- |
| `GET` | `/investor/me` | the investor's panel |
| `GET` | `/investor/me/contracts`, `/me/invoices` | the investor's contracts and invoices |
| `POST` | `/investor/me/payout-accounts` | declare a PIX key |
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
