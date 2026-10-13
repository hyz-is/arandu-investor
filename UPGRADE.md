# Upgrade Guide

## Unreleased

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
