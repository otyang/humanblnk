#!/usr/bin/env python3
"""Builds docs.html from the tested samples in ledger/docs_examples_test.go.

Run scripts/gendocs.sh rather than this file directly.
"""
import html, re, sys, textwrap

src_path, out_path, csv_path = sys.argv[1], sys.argv[2], sys.argv[3]
csv_lines = open(csv_path).read().strip().split('\n')
csv_excerpt = '\n'.join(csv_lines[:8] + ['…', csv_lines[-1]])
src = open(src_path).read()

# ---------------------------------------------------------------- samples
samples = {}
for m in re.finditer(r'^([ \t]*)// doc:(\w+)\n(.*?)^[ \t]*// enddoc', src, re.M | re.S):
    samples[m.group(2)] = textwrap.dedent(m.group(3)).strip('\n')

KW = set('break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'.split())
BUILTIN = set('nil true false error string int int64 bool any byte len append make new'.split())
TOKEN = re.compile(r'(?P<com>//[^\n]*)|(?P<str>"(?:\\.|[^"\\])*"|`[^`]*`)|(?P<num>\b\d[\d_.]*\b)|(?P<id>[A-Za-z_]\w*)|(?P<other>.)', re.S)

def highlight(code):
    out = []
    for m in TOKEN.finditer(code):
        kind, text = m.lastgroup, m.group()
        esc = html.escape(text)
        if kind == 'com':
            out.append(f'<span class="c">{esc}</span>')
        elif kind == 'str':
            out.append(f'<span class="s">{esc}</span>')
        elif kind == 'num':
            out.append(f'<span class="n">{esc}</span>')
        elif kind == 'id':
            if text in KW:
                out.append(f'<span class="k">{esc}</span>')
            elif text in BUILTIN:
                out.append(f'<span class="b">{esc}</span>')
            elif text[0].isupper():
                out.append(f'<span class="t">{esc}</span>')
            else:
                out.append(esc)
        else:
            out.append(esc)
    return ''.join(out)

def code(name, caption=None):
    body = highlight(samples[name])
    cap = f'<figcaption>{caption}</figcaption>' if caption else ''
    return f'<figure class="code">{cap}<pre><code>{body}</code></pre></figure>'

def raw(text, lang='text', caption=None):
    body = highlight(text) if lang == 'go' else html.escape(text)
    cap = f'<figcaption>{caption}</figcaption>' if caption else ''
    return f'<figure class="code">{cap}<pre><code>{body}</code></pre></figure>'

def entries(rows, note=None):
    """rows: (account, debit, credit) — Blnk debits the source and credits the destination."""
    tr = ''.join(
        f'<tr><td><code>{html.escape(a)}</code></td><td class="num">{d}</td><td class="num">{c}</td></tr>'
        for a, d, c in rows)
    n = f'<p class="tnote">{note}</p>' if note else ''
    return f'''<div class="entries"><table><thead><tr><th>Account</th><th class="num">Debit</th><th class="num">Credit</th></tr></thead><tbody>{tr}</tbody></table>{n}</div>'''

def table(head, rows, cls=''):
    th = ''.join(f'<th>{h}</th>' for h in head)
    tr = ''.join('<tr>' + ''.join(f'<td>{c}</td>' for c in r) + '</tr>' for r in rows)
    return f'<div class="tablewrap"><table class="{cls}"><thead><tr>{th}</tr></thead><tbody>{tr}</tbody></table></div>'

def note(text, kind='note'):
    return f'<aside class="{kind}">{text}</aside>'

# ---------------------------------------------------------------- content
nav = []  # (group, [(id, title)])
sections = []

def group(title):
    nav.append((title, []))

def section(sid, title, body, lead=None):
    nav[-1][1].append((sid, title))
    lede = f'<p class="lede">{lead}</p>' if lead else ''
    sections.append(f'<section id="{sid}"><h2><a href="#{sid}">{title}</a></h2>{lede}{body}</section>')

# --- Getting started
group('Getting started')
section('overview', 'Overview', f'''
<p><strong>humanblnk</strong> is an unofficial Go library over <a href="https://docs.blnkfinance.com">Blnk</a>, the open-source double-entry ledger. Blnk gives you balances, transactions and a strict accounting core; this library turns them into the operations a wallet product actually has: top-ups, transfers, withdrawals, card holds, marketplace splits, escrow, currency exchange, statements and the back-office work behind them.</p>
<p>This guide follows <strong>Oja</strong>, a Nigerian wallet app. Ada keeps naira, dollars and bitcoin with Oja, pays friends and merchants, and saves for rent. Chidi Stores sells through Oja's marketplace. Every code sample on this page is compiled and run against the library's test suite.</p>
<div class="layers">
  <div><span>Your app</span><small>HTTP handlers, jobs, webhook receivers</small></div>
  <div class="arrow">↓ depends on <code>ledger.Ledger</code></div>
  <div><span>ledger</span><small>wallet operations, money maths, fees, limits, statements</small></div>
  <div class="arrow">↓ calls</div>
  <div><span>blnk</span><small>typed HTTP client, retries, structured errors</small></div>
  <div class="arrow">↓ REST</div>
  <div><span>Blnk Core</span><small>Postgres-backed ledger (or <code>ledgertest</code> in tests)</small></div>
</div>
<p>Calls are grouped into namespaces so they read like the product:</p>
{raw("""l.Accounts()        // Open, Get, GetSystem, Find, Freeze, Unfreeze, Close, History, BalanceAt, Statement, LimitUsage
l.Customers()       // Open, Get, OpenPot, ListPots
l.Payments()        // Deposit, Withdraw, Transfer, Split, Exchange, Bulk, Schedule, QuoteDeposit, QuoteWithdraw, QuoteTransfer
l.Pending()         // Hold, Confirm, Cancel, ConfirmMany, CancelMany
l.Transactions()    // Get, GetByReference, Refund, RefundPartial
l.Disputes()        // Open, Resolve
l.Escrows()         // Open, Get, Release, Refund
l.Operations()      // RecordSettlement, ParkInSuspense, AllocateSuspense, Adjust, TrialBalance, WatchBalance, Unwatch
l.Reconciliations() // CreateRule, Start, Get""", 'go')}
''')

section('install', 'Install and run Blnk', f'''
{raw("go get github.com/otyang/humanblnk", caption="Add the module (Go 1.22+, no third-party dependencies)")}
<p>You need a Blnk Core to talk to. Locally, Blnk runs in Docker (server, worker, Postgres, Redis) and listens on <code>http://localhost:5001</code>; see Blnk's install guide. To try the library first with no setup at all, run the bundled walkthrough against the in-memory emulator:</p>
{raw("""go run ./cmd/example                                          # in-memory emulator
BLNK_URL=http://localhost:5001 go run ./cmd/example           # your local Blnk Core""")}
''')

section('setup', 'Set up the service', f'''
<p>Create one <code>Service</code> at startup and share it; it is safe for concurrent use. Depend on the <code>ledger.Ledger</code> interface (or a single namespace) in the rest of your code.</p>
{code('setup', 'main.go')}
{table(['Setting', 'What it does'], [
 ['<code>LedgerID</code>', 'The Blnk ledger customer accounts are opened in.'],
 ['<code>Fees</code>', 'Prices every chargeable call that doesn’t set <code>Fee</code> itself. See <a href="#fees">Fees</a>.'],
 ['<code>Limits</code>, <code>Location</code>', 'Per-tier spending caps and the time zone their days reset in. See <a href="#limits">Limits</a>.'],
 ['<code>EnforceAccountStatus</code>', 'Refuses movements touching frozen, blocked or closed accounts (one extra read per account).'],
 ['<code>Display</code>', 'How amounts are shown per currency. See <a href="#money">Money</a>.'],
 ['<code>Precisions</code>', 'Smallest unit per currency. Built in for fiat and major crypto; never change once money has moved.'],
 ['<code>WithdrawalHoldTTL</code>', 'Pending withdrawals cancel themselves after this (default 24h).'],
 ['<code>UseQueue</code>', 'Route writes through Blnk’s queue for very hot balances; results then arrive by webhook.'],
 ['<code>CoreDryRun</code>', 'Quotes also dry-run on Blnk (Core 0.15.3+ only; older Core would post).'],
 ['<code>FeeIncomeAccount</code>, …', 'Rename system accounts if you already have money on other indicators.'],
])}
''')

# --- Concepts
group('Concepts')
section('money', 'Money', f'''
<p>Amounts are typed as decimal strings, exactly as the user entered them, and never pass through floating point. Every amount the service returns carries three forms:</p>
{table(['Field', 'Example (₦1,250.5)', 'Use it for'], [
 ['<code>Value</code> / JSON <code>amount</code>', '<code>"1250.5"</code>', 'computing, storing, APIs: exact'],
 ['<code>Display</code> / JSON <code>display</code>', '<code>"1250.50"</code>', 'showing to people'],
 ['<code>PreciseAmount</code> + <code>Precision</code>', '<code>125050</code> × <code>100</code>', 'what Blnk stores: the integer in the smallest unit'],
])}
{raw("""ledger.Amount("1250.50", "NGN")   // what the user typed
ledger.Amount("0.0125", "BTC")    // 1,250,000 satoshi
ledger.Amount("17250.50", "")     // currency taken from context (fees, partial captures, splits)
ledger.Minor(125050, "NGN")       // if you already hold the integer""", 'go')}
<p><code>Display</code> follows a rule per currency: fiat and stablecoins are padded to their decimals; crypto is trimmed and <strong>cut, never rounded up</strong>, so a displayed balance can always be withdrawn in full.</p>
{table(['Currency', 'amount', 'display'], [
 ['NGN, USD', '<code>100</code>, <code>100.5</code>', '<code>100.00</code>, <code>100.50</code>'],
 ['JPY', '<code>1500</code>', '<code>1500</code>'],
 ['USDT, USDC', '<code>25.5</code>', '<code>25.50</code>'],
 ['BTC (up to 8)', '<code>0.456</code>', '<code>0.456</code>'],
 ['ETH (up to 6)', '<code>0.4567896</code>', '<code>0.456789</code>'],
])}
{note('Typing more decimals than a currency has (<code>"100.567"</code> NGN) is rejected before anything reaches Blnk, as are exponents like <code>1e3</code>. Currency symbols and thousands separators belong in your front end, which knows the user’s locale.')}
''')

section('references', 'References and retries', f'''
<p>Every movement takes a <code>Reference</code>, and Blnk accepts each reference once. That makes every call safe to retry: send the same request again and you get the original transaction back with <code>Replayed: true</code> instead of moving money twice.</p>
<ul>
<li>Generate the reference <strong>once</strong>, when the user taps "Send", and store it with the pending request, so a retry after a timeout reuses it.</li>
<li>For money arriving from a provider, use <strong>the provider’s reference</strong> (Paystack’s, a blockchain <code>txid:vout</code>), so a repeated webhook can’t credit twice.</li>
<li>The same reference with a different amount, account or kind of movement fails with <code>ErrDuplicateReference</code>: a real conflict to investigate, not to retry.</li>
</ul>
''')

section('accounts-model', 'Accounts and the chart of accounts', f'''
<p>Customer accounts are where users’ money lives. System accounts, named with <code>@</code>, are the other side of every entry and are what finance reconciles. Blnk creates each system account on first use, per currency.</p>
{table(['Account', 'Type', 'Role'], [
 ['<code>@PaystackClearing</code> (any <code>@&lt;Provider&gt;Clearing</code>)', 'Asset / liability', 'Money a provider collected for you, or paid out for you, not yet settled with your bank'],
 ['<code>@FeeIncome</code>', 'Income', 'Every fee'],
 ['<code>@BankOperating</code>', 'Asset', 'Your bank account'],
 ['<code>@ProviderFees</code>', 'Expense', 'Fees providers keep at settlement'],
 ['<code>@FXPool</code>', 'Position', 'Your open position per currency from exchanges'],
 ['<code>@MerchantPayable</code>', 'Liability', 'Captured card payments owed to the card network'],
 ['<code>@Suspense</code>', 'Suspense', 'Money that arrived but can’t be matched to a customer yet'],
 ['<code>@Adjustments</code>', 'Equity / expense', 'Counterparty of approved manual corrections'],
])}
<p>Blnk debits the source and credits the destination, so asset and expense accounts run negative and liability and income accounts run positive. Account <code>Kind</code> tells them apart: <code>customer</code>, <code>system</code>, <code>pot</code> or <code>escrow</code>.</p>
''')

section('status', 'Transaction status', f'''
{table(['EntryStatus', 'Blnk status', 'Meaning'], [
 ['<code>posted</code>', 'APPLIED', 'In the balances'],
 ['<code>pending</code>', 'INFLIGHT', 'Reserved: counts against Available, not yet moved. Finish with <code>Pending().Confirm</code> or <code>Cancel</code>'],
 ['<code>voided</code>', 'VOID', 'A pending amount released'],
 ['<code>scheduled</code>', 'SCHEDULED', 'Will post at a set time'],
 ['<code>queued</code>', 'QUEUED', 'Accepted by Blnk’s queue (only with <code>UseQueue</code>)'],
 ['<code>rejected</code>', 'REJECTED', 'Refused, e.g. a scheduled transfer that found no funds'],
])}
''')

section('errors', 'Errors', f'''
<p>Every error wraps one of the sentinels below; check with <code>errors.Is</code>. The raw <code>*blnk.APIError</code> (HTTP status, Blnk code, details) stays reachable with <code>errors.As</code> for logs.</p>
{table(['Error', 'When', 'What to tell the user'], [
 ['<code>ErrInsufficientFunds</code>', 'Not enough available balance', '“Not enough money for this.”'],
 ['<code>ErrLimitExceeded</code>', 'Over a per-transaction, daily or monthly limit', 'Offer a KYC upgrade'],
 ['<code>ErrAccountFrozen</code>, <code>ErrAccountClosed</code>', 'Account status blocks it', 'Route to support'],
 ['<code>ErrInvalidInput</code>', 'Bad amount, missing fee or reference, wrong currency…', 'A bug in the caller'],
 ['<code>ErrDuplicateReference</code>', 'Reference reused for a different movement', 'Investigate; don’t retry'],
 ['<code>ErrNotPending</code>, <code>ErrAlreadyFinalized</code>', 'Confirm/Cancel on something already finished', 'Usually safe to ignore'],
 ['<code>ErrAmountExceeded</code>', 'Capturing or refunding more than is left', 'A bug in the caller'],
 ['<code>ErrFeePricing</code>', 'Your <code>Config.Fees</code> function failed or returned nonsense', 'Retry later; alert'],
 ['<code>ErrNotFound</code>', 'No such account or transaction', '—'],
 ['<code>ErrConflict</code>, <code>ErrUnavailable</code>', 'Contention or Blnk unreachable (after retries)', '“Try again shortly.”'],
])}
''')

# --- Customers
group('Customers and accounts')
section('onboard', 'Onboard a customer', f'''
{code('onboard')}
<p>This creates a Blnk identity and one account per currency. With <code>ExternalID</code> set (your own user id), calling it again after a timeout returns the same accounts and opens any new currencies, so signup is safe to retry. The <code>tier</code> metadata drives limits and pricing.</p>
''', 'Ada signs up and gets naira, dollar and bitcoin accounts in one call.')

section('balance', 'Read a balance', f'''
{code('balance')}
<p><code>Available</code> is what the app should let Ada spend: the balance minus money reserved by card holds and pending withdrawals.</p>
''')

section('freeze', 'Freeze, unfreeze and close', f'''
{code('freeze')}
<p><code>Accounts().Close</code> refuses while anything is pending, moves any remaining balance to <code>SweepTo</code>, then marks the account closed for good. A frozen account can still be closed.</p>
''')

# --- Money in & out
group('Money in and out')
section('topup', 'Top up through Paystack', f'''
{code('topup')}
{entries([('@PaystackClearing', '150,000.00', ''), ('@FeeIncome', '', '2,000.00'), ('Ada · NGN', '', '148,000.00')],
 'The fee leg and the customer leg post together. Paystack now owes you ₦150,000 until it settles.')}
<p>Set <code>Pending: true</code> to hold a deposit for checks (say, a first large top-up) and approve it later with <code>Pending().Confirm</code>.</p>
''', 'Paystack confirms a payment; credit Ada exactly once.')

section('p2p', 'Send money to a friend', f'''
{code('p2p')}
{entries([('Ada · NGN', '12,510.00', ''), ('Tunde · NGN', '', '12,500.00'), ('@FeeIncome', '', '10.00')])}
<p>A quote runs the same checks as the real transfer (funds, limits, frozen accounts, fee) without moving money, so what Ada confirms is what she is charged.</p>
''', 'Ada sends Tunde her share of the rent, seeing the fee before she confirms.')

section('withdraw', 'Withdraw to a bank account', f'''
{code('withdraw')}
{entries([('Ada · NGN', '20,050.00', ''), ('@FlutterwaveClearing', '', '20,000.00'), ('@FeeIncome', '', '50.00')], 'Reserved when created; posted on Confirm; released on Cancel or after 24 hours.')}
''', 'Money out is pending until the payout provider confirms it.')

section('card', 'Card payments', f'''
{code('card')}
{entries([('Ada · NGN', '17,250.50', ''), ('@MerchantPayable', '', '17,250.50')], 'Only the captured amount posts. The rest is released back to Available.')}
''', 'Authorise first, capture what the merchant actually charges, release the rest.')

section('fx', 'Exchange currency', f'''
{code('fx')}
{entries([('Ada · USD', '101.00', ''), ('@FXPool · USD', '', '100.00'), ('@FeeIncome · USD', '', '1.00'), ('@FXPool · NGN', '153,240.00', ''), ('Ada · NGN', '', '153,240.00')],
 'Both legs post in one atomic batch. The pool balances are your open position per currency.')}
<p>Give <code>Buy</code> instead of <code>Sell</code> to land an exact amount (“receive exactly ₦100,000”). The bought amount rounds down and a computed sell amount rounds up, so rounding never costs you.</p>
''', 'Ada converts dollars to naira at the rate you quoted.')

section('crypto', 'Crypto', f'''
{code('crypto')}
<p>Crypto uses each chain’s smallest unit (satoshi, wei), so ledger balances match the chain exactly. For stablecoins on several chains, note that decimals differ by chain (USDT is 6 on Tron and Ethereum, 18 on BNB Chain): keep one precision per ledger currency and convert at the edge, or give each chain its own currency code.</p>
''', 'Exact satoshi accounting, with display that never overstates a balance.')

# --- Commerce
group('Commerce')
section('marketplace', 'Marketplace orders', f'''
{code('marketplace')}
{entries([('@PaystackClearing', '25,000.00', ''), ('@FeeIncome', '', '1,250.00'), ('Tunde · NGN (rider)', '', '1,500.00'), ('Chidi Stores · NGN', '', '22,250.00')])}
''', 'One payment, several recipients, exact to the kobo.')

section('escrow', 'Escrow (buyer protection)', f'''
{code('escrow')}
{entries([('Ada · NGN', '80,000.00', ''), ('Escrow account', '', '80,000.00')], 'On Open.')}
{entries([('Escrow account', '80,000.00', ''), ('Chidi Stores · NGN', '', '80,000.00')], 'On Release.')}
<p>Each escrow is its own account, so what’s still held is simply its balance, and Blnk itself refuses to release more than is left.</p>
''', 'Hold the buyer’s money until delivery is confirmed.')

section('dispute', 'Disputes and chargebacks', f'''
{code('dispute')}
<p>Opening a dispute places a hold in the seller’s account pointed back at the payer, so the money can’t be spent while it is investigated. For a payment that carried a fee, the library disputes the leg that paid the seller, never the fee.</p>
''', 'Freeze the disputed money until the case is decided.')

section('refund', 'Refunds', f'''
{code('refund')}
<p>Partial refunds are linked to the original with <code>meta_data.refund_of</code>, and the original keeps a running refunded total, so over-refunding is refused. If refunds for the same transaction can run concurrently, serialise them (one worker or a row lock).</p>
''', 'Return part of a payment when an item is out of stock.')

# --- Savings & payroll
group('Savings and payroll')
section('pots', 'Savings pots and standing orders', f'''
{code('pots')}
<p>A pot is a sub-account in the same currency and identity, with an optional goal. Blnk has no native recurrence, so a standing order is a set of scheduled transfers sharing <code>meta_data.recurring_reference</code>; calling <code>Schedule</code> again with the same input is safe.</p>
''', 'Ada saves for rent automatically every month.')

section('payroll', 'Payroll', f'''
{code('payroll')}
<p><code>Atomic</code> makes the batch all-or-nothing. Up to 10,000 transfers per batch; <code>Async</code> processes in the background with the outcome sent as a <code>bulk_transaction.*</code> webhook.</p>
''', 'A merchant pays its staff in one batch.')

# --- Controls
group('Controls')
section('fees', 'Fees', f'''
<p>Every chargeable call (deposit, withdrawal, transfer, exchange, standing order, bulk item and their quotes) must say what it charges, so a fee can never be forgotten. Either pass <code>Fee</code> on the call (<code>ledger.NoFee</code> for free), or set <code>Config.Fees</code> to price them centrally:</p>
{code('pricing', 'pricing.go')}
{table(['FeeRule', 'Example', 'Meaning'], [
 ['<code>Percent</code>', '<code>"1.5"</code>', '1.5% of the amount'],
 ['<code>Fixed</code>', '<code>"10"</code>', 'flat ₦10 (in the transaction’s currency)'],
 ['<code>Min</code>, <code>Max</code>', '<code>"10"</code>, <code>"2000"</code>', 'floor and cap'],
 ['<code>Tiers</code>', '≤ ₦5,000: ₦10, above: 0.1%', 'bands by amount'],
 ['<code>Rounding</code>', '<code>RoundHalfUp</code>', 'or <code>RoundUp</code> / <code>RoundDown</code>'],
])}
<p>A fee is always in the transaction’s own currency. To price “$0.50 everywhere”, convert inside your function with <code>r.Convert(ledger.Amount("0.50", "USD"), rate, ledger.RoundUp)</code>. A fee passed on the call always wins over the function, which is how promotions and waivers work.</p>
''')

section('limits', 'Limits and KYC tiers', f'''
{code('limits')}
<p>Limits apply to money leaving customer accounts: transfers, withdrawals, holds, splits, exchanges and escrow. A pending hold counts while held and comes back if cancelled. Days and months reset in <code>Config.Location</code>.</p>
''', 'Per-tier caps, checked before any money moves.')

# --- Reporting
group('Reporting')
section('statement', 'Statements', f'''
{code('statement')}
{raw(csv_excerpt, caption='WriteCSV output (excerpt, from the run above)')}
<p>The statement lists posted entries only, dated by when they took effect, so a standing order appears on the day it posted. The closing balance is checked against Blnk’s own balance at the end of the period: a statement that doesn’t add up is an error, never a wrong document. Fees appear as their own lines, as on a bank statement.</p>
''', 'A downloadable month for Ada, checked against the ledger.')

section('trial', 'Trial balance', f'''
{code('trial')}
<p>In double entry every currency nets to zero. Run this nightly and alert when <code>Balanced</code> is false; <code>CustomerFunds</code> is the total you owe customers, the figure your safeguarding account must cover.</p>
''', 'One call to prove the books add up.')

# --- Back office
group('Back office')
section('settlement', 'Provider settlement', f'''
{code('settlement')}
<p>Each provider has its own clearing account, so after recording settlements its balance is exactly what that provider still owes you (or you owe it) and can be ticked off against its report. Use <code>SettlementPayout</code> for payout providers you prefund.</p>
''', 'Clear Paystack’s receivable when the money reaches your bank.')

section('suspense', 'Unidentified money', code('suspense'), 'Park money you can’t match yet; allocate it once support knows whose it is.')
section('adjust', 'Manual corrections', f'''
{code('adjust')}
<p>Every adjustment records its reason, requester and approver in metadata, and the same person can’t do both.</p>
''', 'Corrections with a second pair of eyes.')

section('watch', 'Balance alerts', code('watch'), 'Get told when a float runs high or low.')

section('reconcile', 'Reconciliation', f'''
{code('reconcile')}
<p>Without <code>RuleIDs</code>, a rule matching reference, currency and amount exactly is created once and reused. Up to 10,000 records go inline; larger exports upload as a CSV with <code>File</code>. Blnk reports counts, not the matched lines.</p>
{note('Payments that carried a fee are stored by Blnk as legs (<code>ref-1</code>, <code>ref-2</code>), so they don’t match a provider line one-to-one. Try <code>Strategy: "one_to_many"</code> with <code>GroupingCriteria: "parent_transaction"</code>, and check the counts against your own Core version before relying on them.', 'warn')}
''', 'Match Paystack’s report against the ledger.')

# --- Integration
group('Integration')
section('webhooks', 'Webhooks', f'''
{code('webhook')}
<p>Signatures (<code>X-Blnk-Signature</code>, HMAC-SHA256 of <code>timestamp.body</code>) and a five-minute timestamp window are checked. Blnk delivers at least once and retries on errors, so make handlers idempotent, for example keyed on <code>e.Receipt.TransactionID</code> plus <code>e.Name</code>. An expired hold arrives as <code>transaction.void</code>.</p>
''', 'React to what happens in the ledger.')

section('observe', 'Logging, metrics and tracing', f'''
{code('observe')}
<p><code>Observer</code> receives every call’s name, context and error, which is all you need for metrics or one OpenTelemetry span per call. Business refusals (insufficient funds, limits) log at Warn, failures at Error.</p>
''')

section('testing', 'Testing your app', f'''
{code('testing', 'rent_test.go')}
<p><code>ledgertest</code> is an in-memory Blnk Core: your tests run the real <code>Service</code> code, so they exercise the same requests production sends. It behaves like Core 0.15 where that matters (fee legs, stored ids, required descriptions) and lets you move time forward to expire holds and run scheduled transfers. To check against a real Core (it writes test data):</p>
{raw("BLNK_LIVE_URL=http://localhost:5001 go test ./ledger -run TestLiveCore -v")}
''', 'Fast tests with no Docker, using the real request code.')

# --- Reference
group('Reference')
section('versions', 'Blnk versions and known limits', f'''
{table(['Needs Core', 'Feature'], [
 ['0.8.4', 'Historical balances (<code>BalanceAt</code>, <code>Statement</code>), global webhooks'],
 ['0.10.0', 'Inline reconciliation'],
 ['0.13.2', 'Filters: history, account search, limits, trial balance, <code>ExternalID</code>'],
 ['0.14.1', 'Auto-capture with <code>CaptureAt</code> (inflight commit queue enabled)'],
 ['0.14.2', '<code>ConfirmMany</code> / <code>CancelMany</code>'],
 ['0.15.0', 'Structured error codes (older versions fall back to HTTP status)'],
 ['0.15.3', '<code>CoreDryRun</code> quotes, refund descriptions'],
])}
<ul class="limits">
<li><strong>Fee legs.</strong> A movement with a fee is stored as legs (<code>ref-1</code>, <code>ref-2</code>) with no parent record. The library rebuilds the parent for lookups and retries, and disputes or refunds pick the recipient’s leg, but raw Blnk queries and reconciliation see legs.</li>
<li><strong>System account ids.</strong> Blnk stores <code>@FeeIncome</code>-style accounts by balance id; receipts and history show the id, statements show the name.</li>
<li><strong>Concurrent partial refunds</strong> of one transaction can both pass the remaining-amount check; serialise them.</li>
<li><strong>Balance alerts</strong> fire after every update while the condition holds; debounce them.</li>
<li><strong>Dry-run quotes</strong> need Core 0.15.3; older Core ignores <code>dry_run</code> and posts, which is why <code>CoreDryRun</code> is off by default.</li>
</ul>
<p>Full signatures and types: <a href="CONTRACT.md">CONTRACT.md</a>. Behaviour notes: <a href="README.md">README.md</a>.</p>
''')

# ---------------------------------------------------------------- page
navhtml = ''.join(
    f'<div class="navgroup"><p>{g}</p>' + ''.join(f'<a href="#{i}">{t}</a>' for i, t in items) + '</div>'
    for g, items in nav)

page = f'''<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>humanblnk Guide</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Schibsted+Grotesk:wght@500;700&family=Source+Sans+3:wght@400;600&family=JetBrains+Mono:wght@400;500&display=swap">
<style>
/* Layout: fixed left index of tasks, one reading column; code and ledger entries sit beside the prose they illustrate. */
:root {{
  --bg: #fbfaf7; --panel: #f2efe8; --ink: #1d2226; --muted: #5f676e; --rule: #e1ddd3;
  --accent: #0a6b52; --accent-soft: #e2f0eb; --warn: #8a5a00; --warn-soft: #fbf0d9;
  --code-bg: #1f2428; --code-ink: #e6e6e3; --c-com: #8b949e; --c-str: #a5d6a7; --c-kw: #ff9e7a; --c-type: #8ec7ff; --c-num: #f2cc60; --c-bi: #d7a8ff;
  --display: "Schibsted Grotesk", "Helvetica Neue", Arial, sans-serif;
  --body: "Source Sans 3", "Helvetica Neue", Arial, sans-serif;
  --mono: "JetBrains Mono", ui-monospace, Menlo, monospace;
  color-scheme: light;
}}
@media (prefers-color-scheme: dark) {{ :root:not([data-theme="light"]) {{
  --bg: #14181b; --panel: #1b2024; --ink: #e8eae6; --muted: #a2aaa6; --rule: #2b3236;
  --accent: #5fd0a8; --accent-soft: #163329; --warn: #f0c060; --warn-soft: #33290f;
  --code-bg: #0f1214; color-scheme: dark }} }}
:root[data-theme="dark"] {{
  --bg: #14181b; --panel: #1b2024; --ink: #e8eae6; --muted: #a2aaa6; --rule: #2b3236;
  --accent: #5fd0a8; --accent-soft: #163329; --warn: #f0c060; --warn-soft: #33290f;
  --code-bg: #0f1214; color-scheme: dark }}
* {{ box-sizing: border-box }}
html {{ scroll-padding-top: 20px }}
body {{ margin: 0; background: var(--bg); color: var(--ink); font: 17px/1.6 var(--body); }}
a {{ color: var(--accent) }}
a:focus-visible {{ outline: 2px solid var(--accent); outline-offset: 2px }}
.shell {{ display: grid; grid-template-columns: 270px minmax(0, 1fr); min-height: 100vh }}
nav.side {{ position: sticky; top: 0; height: 100vh; overflow-y: auto; border-right: 1px solid var(--rule); background: var(--panel); padding: 28px 22px 40px }}
.brand {{ font: 700 20px var(--display); letter-spacing: -.01em }}
nav.side summary {{ display: none; cursor: pointer; margin-top: 14px; font: 600 14px var(--body); color: var(--accent) }}
.brand small {{ display: block; font: 400 13px var(--body); color: var(--muted); margin-top: 2px }}
.navgroup {{ margin-top: 22px; display: grid; gap: 2px }}
.navgroup p {{ margin: 0 0 4px; font: 600 11.5px var(--body); text-transform: uppercase; letter-spacing: .08em; color: var(--muted) }}
.navgroup a {{ color: var(--ink); text-decoration: none; font-size: 15px; padding: 3px 8px; border-radius: 4px }}
.navgroup a:hover {{ background: var(--accent-soft); color: var(--accent) }}
main {{ min-width: 0; padding: 48px max(24px, env(safe-area-inset-right, 0px)) 96px max(24px, env(safe-area-inset-left, 0px)); max-width: 900px }}
header.hero h1 {{ font: 700 clamp(30px, 4vw, 42px)/1.1 var(--display); letter-spacing: -.02em; margin: 0 0 10px; text-wrap: balance }}
header.hero p {{ margin: 0; color: var(--muted); max-width: 62ch }}
section {{ padding-top: 44px; margin-top: 44px; border-top: 1px solid var(--rule) }}
h2 {{ font: 700 25px/1.25 var(--display); letter-spacing: -.01em; margin: 0 0 6px; text-wrap: balance }}
h2 a {{ color: inherit; text-decoration: none }}
h2 a:hover::after {{ content: " #"; color: var(--muted) }}
.lede {{ margin: 0 0 16px; color: var(--muted); font-size: 18px }}
p, ul {{ max-width: 68ch }}
li {{ margin: 6px 0 }}
code {{ font: 500 .86em var(--mono); background: var(--panel); border: 1px solid var(--rule); padding: 1px 5px; border-radius: 4px }}
figure.code {{ margin: 18px 0; border-radius: 8px; overflow: hidden; background: var(--code-bg) }}
figure.code figcaption {{ font: 500 12px var(--mono); color: var(--c-com); padding: 10px 16px 0 }}
figure.code pre {{ margin: 0; padding: 14px 16px 16px; overflow-x: auto }}
figure.code code {{ background: none; border: 0; padding: 0; color: var(--code-ink); font: 13.5px/1.6 var(--mono); white-space: pre; tab-size: 4 }}
.c {{ color: var(--c-com); font-style: italic }} .s {{ color: var(--c-str) }} .k {{ color: var(--c-kw) }}
.t {{ color: var(--c-type) }} .n {{ color: var(--c-num) }} .b {{ color: var(--c-bi) }}
.tablewrap, .entries {{ overflow-x: auto; margin: 16px 0 }}
table {{ border-collapse: collapse; width: 100%; font-size: 15px }}
th {{ text-align: left; font: 600 12px var(--body); text-transform: uppercase; letter-spacing: .06em; color: var(--muted); padding: 8px 10px; border-bottom: 2px solid var(--ink) }}
td {{ padding: 8px 10px; border-bottom: 1px solid var(--rule); vertical-align: top }}
td code {{ white-space: nowrap }}
.num {{ text-align: right; font-family: var(--mono); font-variant-numeric: tabular-nums; white-space: nowrap }}
.entries {{ border: 1px solid var(--rule); border-radius: 8px; padding: 4px 8px 8px; background: var(--panel); max-width: 560px }}
.entries table {{ font-size: 14px }}
.entries th {{ border-bottom-width: 1px }}
.entries tr:last-child td {{ border-bottom: 0 }}
.tnote {{ font-size: 13.5px; color: var(--muted); margin: 6px 4px 0 }}
aside.note, aside.warn {{ border-radius: 8px; padding: 12px 16px; margin: 16px 0; font-size: 15.5px; max-width: 68ch }}
aside.note {{ background: var(--accent-soft) }}
aside.warn {{ background: var(--warn-soft); color: var(--ink) }}
aside.warn::before {{ content: "Limitation  "; font: 600 12px var(--body); text-transform: uppercase; letter-spacing: .06em; color: var(--warn) }}
.layers {{ display: grid; gap: 4px; margin: 20px 0; max-width: 520px }}
.layers > div:not(.arrow) {{ border: 1px solid var(--rule); background: var(--panel); border-radius: 8px; padding: 10px 14px; display: grid }}
.layers span {{ font: 700 15px var(--display) }}
.layers small {{ color: var(--muted); font-size: 14px }}
.layers .arrow {{ font-size: 13px; color: var(--muted); padding-left: 14px }}
ul.limits li {{ margin: 10px 0 }}
@media (max-width: 860px) {{
  .shell {{ grid-template-columns: minmax(0, 1fr) }}
  nav.side {{ position: static; height: auto; border-right: 0; border-bottom: 1px solid var(--rule);
    padding-left: max(16px, env(safe-area-inset-left, 0px)); padding-right: max(16px, env(safe-area-inset-right, 0px)) }}
  .navgroup {{ grid-template-columns: repeat(2, minmax(0, 1fr)) }}
  .navgroup p {{ grid-column: 1 / -1 }}
  nav.side summary {{ display: list-item }}
  main {{ padding-left: max(16px, env(safe-area-inset-left, 0px)); padding-right: max(16px, env(safe-area-inset-right, 0px)) }}
}}
@media (prefers-reduced-motion: no-preference) {{ html {{ scroll-behavior: smooth }} }}
</style>
</head>
<body>
<div class="shell">
<nav class="side" aria-label="Contents">
  <div class="brand">humanblnk<small>Wallet operations on Blnk, in Go</small></div>
  <details id="toc" open><summary>Contents</summary>{navhtml}</details>
</nav>
<main>
<header class="hero">
  <h1>Build a wallet on Blnk</h1>
  <p>A practical guide to the <code>ledger</code> package, told through Oja, a wallet app serving customers in Nigeria: naira, dollars and bitcoin, from signup to month-end close.</p>
</header>
{''.join(sections)}
</main>
</div>
<script>
// On phones the contents start collapsed and close after a jump.
(function () {{
  var toc = document.getElementById("toc"), narrow = window.matchMedia("(max-width: 860px)");
  function sync() {{ toc.open = !narrow.matches; }}
  sync();
  narrow.addEventListener("change", sync);
  toc.addEventListener("click", function (e) {{ if (e.target.tagName === "A" && narrow.matches) toc.open = false; }});
}})();
</script>
</body>
</html>
'''
open(out_path, 'w').write(page)
missing = [k for k in samples if k not in page and False]
print(f"{out_path}: {len(page)//1024} KB, {len(samples)} samples, {sum(len(i) for _, i in nav)} sections")
