# Changelog

Everything worth knowing about a release of Arandu Investor is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

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

