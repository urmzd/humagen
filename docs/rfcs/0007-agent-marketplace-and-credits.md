# Agent marketplace and credits

Status: not planned. Nothing here is built. It describes a hosted Humagen and
a marketplace, written for a separate hosted service that is not being built.
It is kept as a record. In the tables and lists below, "here" means that
separate service, not this repository.

## Goal

Anyone can define an agent, publish it, and set a price for it. Anyone can run
it without operating anything: they add credits to an account, and every run
spends from that balance. Self-hosted Humagen stays complete and free; the
hosted service sells convenience and the marketplace.

## Context

- **Open core.** Public `urmzd/humagen` (Apache 2.0) is the whole platform:
  agents on any model, projects, roles, policies, budgets, a usage ledger in
  int64 micro-USD, evals, knowledge, the console and Slack.
- **Extension, not fork.** Humagen's rule is "every choice is a file or an
  interface, never a fork": `humagen.Main` takes options, `humagen.WithKind`
  adds a backend to a spec slot. This repository is a program that imports
  Humagen and adds what only the hosted service needs.
- **What the hosted service needs that Humagen lacks:** many tenants on shared
  infrastructure (Humagen runs one replica per database today), isolation
  strong enough for strangers' agents (tools in a sandbox with an egress gate),
  prices for every model (the Vertex models are unpriced in saige's catalog),
  money in and out, and a catalog of agents with trust signals.

## Decision

Build it in three stages. Each one is useful on its own, and each stage's
prerequisites are listed so it never ships ahead of the safety it needs.

| Stage | What people get | Where it lives | Prerequisites |
|-------|------------------|----------------|---------------|
| **1. Registry** | Publish an agent package; install it into any Humagen, hosted or self-hosted. Free | Package format and install command: public Humagen. Registry service: here | Eval suites as a listing gate |
| **2. Hosted Humagen with credits** | Sign up, add credits, run agents with no infrastructure | Here | Multi-replica stores, egress gate and remote sandbox driver, every model priced |
| **3. Paid listings** | Creators set prices and are paid; buyers rate and get refunds | Here | Stage 2, feedback signals, review and abuse handling |

## Agent packages

A package is what a listing installs, and it is the same for free and paid
agents:

| Part | Content |
|------|---------|
| Definition | The agent spec Humagen already pins per run (instructions, model policy, tools, call policy) |
| Requirements | Tools and connectors it needs, each with the permission class it asks for |
| Evals | The suite a listing must pass, run by the registry on submission and on every new version |
| Pricing | Optional: the creator's price per run or per task, on top of usage |
| Provenance | Publisher, version, digest; an install pins a version and never upgrades silently |

The format and the install path belong in public Humagen, so a self-hoster can
install any free package without this service.

## Credits

- **Prepaid and closed-loop.** Credits are bought with a card and spent only on
  runs. They are never withdrawn or transferred, which keeps the service out of
  money-transmission rules. Unused credits follow a published expiry and refund
  policy.
- **One ledger.** A credit is micro-USD on the account, written to the same
  usage ledger Humagen keeps. A run's cost is the model usage at the catalog
  price plus the platform margin plus the agent's listed price, each recorded
  as its own line so a statement explains every charge.
- **Holds at admission.** Admission reserves an estimate against the balance
  and settles the actual cost when the run ends; a run never starts on an empty
  balance, and a long run is stopped at the account's limit the way budgets
  already stop runs.
- **Organizations.** An organization holds the balance; its projects and people
  draw on it within the budgets Humagen already enforces.

## Payouts

Creators are connected accounts in Stripe Connect. Earnings accrue per settled
run, are held for a refund window, then paid out on a schedule minus the
platform's share. Stripe handles identity checks and tax forms for creators;
the merchant-of-record and sales-tax position is an open question below.

## Trust and safety

Running a stranger's agent with a buyer's data and tools is the main risk.

- **Isolation first.** Paid and hosted runs execute tools only in isolated
  instances with an egress gate that grants each run the network it declared.
  No hosted run uses Humagen's unisolated mode.
- **Consent per install.** Installing a package shows the tools, connectors and
  data it asks for. Granting them is explicit and revocable, and a new version
  that asks for more is not adopted until the buyer agrees.
- **Review.** Every version passes its evals and an automated policy scan
  before listing. Reports, takedowns and refunds have an owner and a timeline.
- **Data boundaries.** A creator never sees a buyer's runs, data or
  credentials, only aggregate usage and ratings.

## What stays public

| Public `humagen` | This repository |
|------------------|-----------------|
| Everything a self-hoster needs to run their team | Credits, Stripe billing, Stripe Connect payouts |
| The package format and the install command | The registry service, listings, review, revenue share |
| Usage ledger, budgets, evals, policies, the extension points | Sign-up, tenant provisioning, plans and quotas |
| | The hosted infrastructure and its operations |

A feature a self-hoster needs never moves here. The open core is what earns
trust; the hosted service competes on convenience.

## Alternatives

- **Everything public.** Simpler, but the billing and marketplace operations
  are business logic with no use to a self-hoster, and publishing them invites
  clones of the service rather than adoption of the platform.
- **An "enterprise" fork of Humagen.** Rejected: a fork diverges. The hosted
  service plugs into Humagen's extension points, and changes it needs go
  upstream as interfaces.
- **Postpaid billing.** Rejected for launch: invoices and collections add risk
  for small accounts. Postpaid can come later for organizations on contract.

## Changes

1. Agent package format and `install` in public Humagen, with eval gating.
2. Registry service here: publish, version, search, install counts.
3. Prerequisites in public Humagen: multi-replica stores, egress gate, remote
   sandbox driver, prices for every catalog model.
4. Hosted Humagen here: sign-up, organizations, credits, holds and settlement.
5. Paid listings: creator onboarding, prices, payouts, ratings, refunds, review.

## Open questions

- Merchant of record and sales tax: the service, or a reseller such as Paddle
  or Lemon Squeezy for buyers, with Stripe Connect only for payouts.
- Pricing units creators may use: per run, per task, per seat, or per month.
- The platform's share, and whether usage margin and listing share differ.
- A free tier: a small starting balance, and how it is protected from abuse.
- Whether self-hosted installs of paid agents should pay creators, and how
  without phoning home.
