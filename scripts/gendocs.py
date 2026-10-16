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
<p><strong>humanblnk</strong> is a wallet and financial-operations domain layer for Go, built on top of <a href="https://docs.blnkfinance.com">Blnk</a>, the open-source double-entry ledger.</p>
<p>A wallet business has two jobs. It moves its customers’ money: sign-ups, top-ups, transfers, card payments, withdrawals, refunds. And it runs its own books: fees earned, money held by payment providers, settlements, unmatched payments, corrections, month-end close. Blnk is the ledger underneath both, and deliberately knows nothing about either: it has no idea what a customer, a fee, a KYC limit, a frozen account or a settlement is. humanblnk is the layer that does. Your app calls it in the business’s own words; it applies the business rules and turns each call into correct Blnk entries.</p>
<p>This guide follows <strong>Oja</strong>, a Nigerian wallet business. Ada keeps naira, dollars and bitcoin with Oja, pays friends and merchants, and saves for rent. Chidi Stores sells through Oja's marketplace. Oja's finance team settles with Paystack, matches stray payments and closes the books. Every code sample on this page is compiled and run against the library's test suite.</p>
<div class="layers">
  <div><span>Your app</span><small>HTTP handlers, jobs, provider webhooks</small></div>
  <div class="arrow">↓ depends on <code>ledger.Ledger</code></div>
  <div><span>humanblnk domain layer</span><small>wallet and financial-operations domains: rules, money maths, fees, limits, status, approvals, reports</small></div>
  <div class="arrow">↓ calls</div>
  <div><span>blnk client</span><small>typed HTTP, retries, structured errors, Blnk Cloud</small></div>
  <div class="arrow">↓ REST</div>
  <div><span>Blnk Core</span><small>the ledger of record (or <code>ledgertest</code>, in memory, in tests)</small></div>
</div>
<p>The interface has one handle per domain, and each handle's methods live in the files with its prefix (<code>wallet_*.go</code>, <code>ops_*.go</code>; shared code is in <code>core_*.go</code>):</p>
{table(['Handle', 'For', 'Files'], [
 ['<code>l.Wallet()</code>', 'what customers do: open accounts, add money, pay, cash out, hold, save, buy, dispute', '<code>wallet_*.go</code>'],
 ['<code>l.Ops()</code>', 'what the business does: provision ledgers, freeze and close, run its own accounts, settle with providers, match unknown money, correct, prove the books, reconcile', '<code>ops_*.go</code>'],
])}
{raw("""wallet := l.Wallet()
wallet.OpenCustomer, GetCustomer, OpenPot, ListPots                       // wallet_customers.go
wallet.OpenAccount, GetAccount, FindAccounts, History                     // wallet_accounts.go
wallet.Statement, BalanceAt                                               // wallet_statements.go
wallet.LimitUsage                                                         // wallet_limits.go
wallet.Deposit, Withdraw, Transfer, Bulk                                  // wallet_payments.go
wallet.Split / Exchange / Schedule                                        // wallet_split.go, wallet_fx.go, wallet_recurring.go
wallet.QuoteDeposit, QuoteWithdraw, QuoteTransfer, QuoteExchange          // wallet_quote.go
wallet.Hold, Confirm, Cancel, ConfirmMany, CancelMany                     // wallet_pending.go
wallet.GetTransaction, GetTransactionByReference, Refund, RefundPartial   // wallet_transactions.go
wallet.OpenEscrow, GetEscrow, ReleaseEscrow, RefundEscrow                 // wallet_escrow.go
wallet.OpenDispute, ResolveDispute                                        // wallet_dispute.go

ops := l.Ops()
ops.CreateLedger, GetLedger, ListLedgers                                  // ops_ledgers.go
ops.FreezeAccount, UnfreezeAccount, CloseAccount, FreezeLedger, UnfreezeLedger // ops_controls.go
ops.GetHouseAccount, ListHouseAccounts, HouseHistory, HouseBalanceAt, HouseStatement // ops_house.go
ops.RecordSettlement                                                      // ops_settlement.go
ops.HoldInSuspense, ClaimFromSuspense, ReturnFromSuspense                 // ops_suspense.go
ops.Adjust                                                                // ops_adjust.go
ops.TrialBalance                                                          // ops_reports.go
ops.WatchBalance, UnwatchBalance                                          // ops_watch.go
ops.CreateMatchingRule, StartReconciliation, GetReconciliation            // ops_reconcile.go""", 'go')}
<p>Hand each part of your application only the handle it needs. A customer-facing server that gets a <code>ledger.Wallet</code> can’t freeze or close accounts, adjust balances or record settlements: those methods aren’t on it.</p>
''')

section('what', 'What a domain layer is', f'''
<p>Every wallet built on Blnk needs the same code between the product and the ledger: check the account isn’t frozen, check the customer’s limit, price the fee, split the payment into its entries, hold it until the provider answers, refuse a second approval by the same person. That code is the <em>domain layer</em>: it knows the business, where Blnk knows the bookkeeping. humanblnk is that layer, written once and tested against real Blnk Core.</p>
{table(['', 'Your app', 'humanblnk', 'Blnk Core'], [
 ['<strong>Talks to</strong>', 'customers, staff, payment providers', 'your app and Blnk', 'humanblnk'],
 ['<strong>Knows about</strong>', 'screens, sessions, providers’ APIs, KYC checks', 'customers, accounts, fees, limits, holds, disputes, settlements, house accounts', 'balances and transactions'],
 ['<strong>Decides</strong>', 'what the customer asked for, and when', 'whether it’s allowed, what it costs, which entries it becomes', 'whether the entries balance and the money is there'],
 ['<strong>Stores</strong>', 'your own users and records', 'nothing of its own: everything lives in Blnk', 'every balance and entry'],
])}
<h3>One withdrawal, through the layers</h3>
{table(['', 'Layer', 'What happens'], [
 ['1', 'Your app', 'Ada taps “Withdraw ₦20,000 to GTBank”. The app makes a reference once, shows <code>wallet.QuoteWithdraw</code>, and calls <code>wallet.Withdraw</code> when she confirms.'],
 ['2', 'humanblnk', 'Refuses if her account or its ledger is frozen or closed, or she is over her tier’s limit. Prices the fee with your <code>Config.Fees</code> (₦50 here). Builds the entries: Ada → <code>@FlutterwaveClearing</code> ₦20,000 and Ada → <code>@FeeIncome</code> ₦50, held inflight for at most 24 hours.'],
 ['3', 'Blnk Core', 'Checks ₦20,050 is available, reserves it and returns the inflight transaction.'],
 ['4', 'Your app', 'Calls Flutterwave with the transaction id as the reference.'],
 ['5', 'Your app → humanblnk', 'Flutterwave’s webhook: SUCCESSFUL → <code>wallet.Confirm</code>; FAILED → <code>wallet.Cancel</code>, and the money is Ada’s again.'],
 ['6', 'humanblnk → Blnk', 'Commits or voids the transaction. A confirm is retried only when Blnk certainly didn’t act, so it can’t post twice.'],
 ['7', 'Finance, later', '<code>ops.RecordSettlement</code> clears <code>@FlutterwaveClearing</code> against the bank when Flutterwave settles; <code>ops.TrialBalance</code> proves the books still add up.'],
])}
<h3>What humanblnk is not</h3>
<ul>
<li><strong>Not a ledger.</strong> It stores nothing itself; Blnk is the record. It can be restarted or scaled out without losing a cent.</li>
<li><strong>Not a payment gateway.</strong> It never calls Paystack, Flutterwave or a bank. Your app does, and tells humanblnk what happened.</li>
<li><strong>Not a KYC system.</strong> You decide a customer’s tier; humanblnk applies the limits you set for it.</li>
<li><strong>Not a full Blnk SDK.</strong> The <code>blnk</code> package covers what the domain needs. Anything else you call on Blnk directly, and then the domain rules don’t apply to it.</li>
</ul>
''', 'The business rules between your product and the ledger.')

section('domain', 'What the domain layer adds', f'''
<p>Blnk gives you primitives and leaves the rules to you. These are the rules humanblnk owns, so every app built on it gets them the same way:</p>
{table(['Concern', 'Blnk on its own', 'With humanblnk'], [
 ['Vocabulary', 'balances, transactions, indicators, inflight', 'customers, accounts, deposits, withdrawals, holds, pots, escrows, disputes, house accounts'],
 ['Money', 'float <code>amount</code> or integer <code>precise_amount</code>', 'exact decimals typed as the user wrote them; fiat and crypto precision built in; rounding that never costs the house'],
 ['Idempotency', 'per transaction reference', 'every operation takes a <code>Reference</code>; ledgers by <code>Key</code>; customers and accounts by <code>ExternalID</code>'],
 ['Status', 'stored, not enforced', 'frozen, closed and frozen-ledger states refuse money on every movement'],
 ['Fees', 'none', 'every chargeable operation states its fee, or <code>Config.Fees</code> prices it'],
 ['Limits', 'none', 'per KYC tier and currency: per transaction, daily, monthly'],
 ['Approvals', 'none', 'closing an account and manual adjustments need two people: <code>RequestedBy</code> and an <code>ApprovedBy</code> who is someone else'],
 ['Kinds of object', 'one balance type for everything', 'ledgers, customer accounts and house accounts are separate types; customer code and business operations are separate handles'],
 ['Chart of accounts', 'free-form <code>@</code> indicators', '<code>ledger.Chart</code>: each house account has a type and a normal side'],
 ['Previews', '<code>dry_run</code>', '<code>Quote*</code>: fee, total, balance after, and Blnk’s own yes/no'],
 ['Reports', 'balances and history', 'statements checked against Blnk, a trial balance, total customer funds'],
])}
<p>Ledgers, customer accounts and house accounts each have their own id type: <code>LedgerID</code>, <code>AccountID</code> and <code>HouseRef</code>. passing a ledger id to <code>ops.FreezeAccount</code> doesn’t compile; a plain string converted to the wrong type is refused at run time with <code>ErrWrongKind</code>, before anything is written.</p>
''', 'The business rules Blnk leaves to you, in one place.')

section('install', 'Install and run Blnk', f'''
{raw("go get github.com/otyang/humanblnk@latest", caption="Add the module (Go 1.27+, no third-party dependencies)")}
<p>You need <strong>Blnk Core 0.15.4 or later</strong>, self-hosted or on <a href="#cloud">Blnk Cloud</a>. Every release is tested against Core 0.15.6 and 0.15.4.</p>
<p>Import the module root for everything in one place, or the packages directly. They are the same types, so mix them freely; this guide uses <code>ledger</code>.</p>
{raw("""import "github.com/otyang/humanblnk"

l := humanblnk.New("http://localhost:5001", humanblnk.Config{LedgerID: "ldg_..."})
_, err := l.Wallet().Transfer(ctx, humanblnk.TransferInput{ /* … */ })
if errors.Is(err, humanblnk.ErrInsufficientFunds) { /* … */ }""", 'go', caption="One import")}
<p>Locally, Blnk Core runs in Docker (server, worker, Postgres, Redis) and listens on <code>http://localhost:5001</code>; see Blnk's install guide. To try the library first with no setup at all, run the bundled walkthrough against the in-memory emulator:</p>
{raw("""go run ./cmd/example                                          # in-memory emulator
BLNK_URL=http://localhost:5001 go run ./cmd/example           # your local Blnk Core""")}
''')

section('setup', 'Set up the service', f'''
<p>Account and ledger status are always enforced: every movement reads the customer accounts it touches (one extra request each) and refuses frozen, blocked or closed ones, and money leaving a frozen ledger. Blnk itself ignores status, so this is what makes <code>Freeze</code> and <code>Close</code> mean something.</p>
<p>Create one <code>Service</code> at startup and share it; it is safe for concurrent use. Depend on the <code>ledger.Ledger</code> interface, or on just <code>ledger.Wallet</code> or <code>ledger.Ops</code>, in the rest of your code. <code>Check</code> confirms at startup that the client is configured correctly, Core is up and reaches its database, and <code>LedgerID</code> exists.</p>
{code('setup', 'main.go')}
<p>Then take the two handles, one per domain:</p>
{code('handles')}
{table(['Setting', 'What it does'], [
 ['<code>LedgerID</code>', 'The Blnk ledger customer accounts are opened in. It must exist: call <code>svc.Check(ctx)</code> at startup to find out now. See <a href="#ledgers">Ledgers</a>.'],
 ['<code>Fees</code>', 'Prices every chargeable call that doesn’t set <code>Fee</code> itself. See <a href="#fees">Fees</a>.'],
 ['<code>Limits</code>, <code>Location</code>', 'Per-tier spending caps and the time zone their days reset in. See <a href="#limits">Limits</a>.'],
 ['<code>LedgerStatusTTL</code>', 'How long a ledger’s frozen/active status is cached (default 30s). A freeze made by another process applies here within this time.'],
 ['<code>Currencies</code>', 'Change built-in currencies (every ISO 4217 code and major crypto) or add your own: name, symbol, precision and how amounts are shown. Never change a precision once money has moved. See <a href="#money">Money</a>.'],
 ['<code>UnknownCurrency</code>', 'Unknown codes are refused by default; <code>ledger.AllowUnknown</code> accepts them at <code>DefaultPrecision</code>.'],
 ['<code>WithdrawalHoldTTL</code>', 'Pending withdrawals cancel themselves after this (default 24h).'],
 ['<code>UseQueue</code>', 'Route writes through Blnk’s queue for very hot balances; results then arrive by webhook.'],
 ['<code>FeeIncomeAccount</code>, …', 'Rename house accounts if you already have money on other indicators.'],
])}
''')

section('cloud', 'Blnk Cloud', f'''
{code('cloud', 'main.go')}
<p>On Blnk Cloud you call Blnk’s proxy with a Cloud API key; every request carries your <code>instance_id</code>. A proxy URL without an instance ID, or one that doesn’t start with <code>instance_</code>, is a configuration error that <code>Check</code> reports. For every client, self-hosted or Cloud, redirects to another host or from https to http are refused, so your API key never leaves the host you configured.</p>
''', 'The same library against a Core hosted by Blnk.')

section('ledgers', 'Provision ledgers', f'''
{code('ledgers')}
<p>Blnk has no idempotency for ledgers and doesn’t require unique names, so <code>ops.CreateLedger</code> provides it: it looks for a ledger carrying your <code>Key</code> first and returns that one with <code>Replayed</code> set. The same key with a different name is <code>ErrDuplicateReference</code>. Calls in one process with the same key run one at a time; if two processes still race and both create, the oldest ledger wins everywhere and the other is marked <code>duplicate</code>, which <code>wallet.OpenAccount</code> refuses. Create ledgers when you provision, not on the request path.</p>
<p><code>wallet.OpenAccount</code> checks the ledger exists before opening anything (<code>ErrLedgerNotFound</code> otherwise), and never opens customer accounts in Blnk’s general ledger, where house accounts live.</p>
''', 'One ledger per market, created once however many times provisioning runs.')

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
<li><code>wallet.Confirm</code> and <code>Cancel</code> carry no reference, so the client never repeats them blindly after a timeout: look the transaction up before trying again.</li>
</ul>
''')

section('accounts-model', 'Accounts and the chart of accounts', f'''
<p>Customer accounts are where users’ money lives; they sit in your own ledgers. House accounts, the business’s own (“house” is the business itself), named with <code>@</code>, are the other side of every entry and are what finance reconciles. Blnk creates each house account on first use, per currency, in its general ledger. The table is <code>ledger.Chart</code>; add your own indicators to it at startup.</p>
{table(['Account', 'Type', 'Normal', 'Role'], [
 ['<code>@PaystackClearing</code> (any <code>@&lt;Provider&gt;Clearing</code>)', 'asset', 'debit', 'Money a provider collected for you, or paid out for you, not yet settled with your bank'],
 ['<code>@DepositClearing</code>', 'asset', 'debit', 'Deposit source when no provider is named'],
 ['<code>@PayoutClearing</code>', 'liability', 'credit', 'Withdrawal destination when no provider is named'],
 ['<code>@FeeIncome</code>', 'income', 'credit', 'Every fee'],
 ['<code>@BankOperating</code>', 'asset', 'debit', 'Your bank account'],
 ['<code>@ProviderFees</code>', 'expense', 'debit', 'Fees providers keep at settlement'],
 ['<code>@FXPool</code>', 'asset', 'debit', 'Your open position per currency from exchanges'],
 ['<code>@MerchantPayable</code>', 'liability', 'credit', 'Captured card payments owed to the card network'],
 ['<code>@Suspense</code>', 'liability', 'credit', 'Money that arrived but can’t be matched to a customer yet'],
 ['<code>@Adjustments</code>', 'equity', 'credit', 'Counterparty of approved manual corrections'],
])}
<p>Blnk debits the source and credits the destination, so asset and expense accounts run negative and liability, income and equity accounts run positive. House accounts are their own type, <code>HouseAccount</code>, read through <code>ops.GetHouseAccount</code> and <code>ops.ListHouseAccounts</code>: it carries the account’s <code>Type</code> and <code>Normal</code> side from <code>ledger.Chart</code>, and <code>Natural</code>, the balance on its normal side, so an asset that is owed to you reads positive. Provider clearing accounts default to assets; add a payout-only provider to <code>Chart</code> as a liability if you prefer. Customer accounts are <code>Account</code>, whose <code>Kind</code> is <code>customer</code>, <code>pot</code> or <code>escrow</code>.</p>
{code('house')}
''')

section('status', 'Transaction status', f'''
{table(['EntryStatus', 'Blnk status', 'Meaning'], [
 ['<code>posted</code>', 'APPLIED', 'In the balances'],
 ['<code>pending</code>', 'INFLIGHT', 'Reserved: counts against Available, not yet moved. Finish with <code>wallet.Confirm</code> or <code>Cancel</code>'],
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
 ['<code>ErrLedgerFrozen</code>', 'The account’s ledger is frozen: nothing can leave it', 'Route to support'],
 ['<code>ErrWrongKind</code>', 'An id of another kind: a ledger id or house account passed to <code>wallet.GetAccount</code>, a balance id to <code>ops.GetLedger</code>…', 'A bug in the caller'],
 ['<code>ErrLedgerNotFound</code>', 'Opening an account in a ledger that doesn’t exist (also matches <code>ErrNotFound</code>)', 'Fix the configuration'],
 ['<code>ErrInvalidInput</code>', 'Bad amount, missing fee or reference, wrong currency…', 'A bug in the caller'],
 ['<code>ErrDuplicateReference</code>', 'Reference reused for a different movement, or a ledger <code>Key</code> reused with a different name', 'Investigate; don’t retry'],
 ['<code>ErrNotPending</code>, <code>ErrAlreadyFinalized</code>', 'Confirm/Cancel on something already finished', 'Usually safe to ignore'],
 ['<code>ErrAmountExceeded</code>', 'Capturing or refunding more than is left', 'A bug in the caller'],
 ['<code>ErrFeePricing</code>', 'Your <code>Config.Fees</code> function failed or returned nonsense', 'Retry later; alert'],
 ['<code>ErrNotFound</code>', 'No such ledger, account or transaction', '—'],
 ['<code>ErrConflict</code>, <code>ErrUnavailable</code>', 'Contention or Blnk unreachable (after retries)', '“Try again shortly.”'],
])}
''')

# --- Customers
group('Wallet · Customers and accounts')
section('onboard', 'Onboard a customer', f'''
{code('onboard')}
<p>This creates a Blnk identity and one account per currency. With <code>ExternalID</code> set (your own user id), calling it again after a timeout returns the same accounts and opens any new currencies, so signup is safe to retry. The <code>tier</code> metadata drives limits and pricing.</p>
''', 'Ada signs up and gets naira, dollar and bitcoin accounts in one call.')

section('balance', 'Read a balance', f'''
{code('balance')}
<p><code>Available</code> is what the app should let Ada spend: the balance minus money reserved by card holds and pending withdrawals.</p>
''')

section('statement', 'Statements', f'''
{code('statement')}
{raw(csv_excerpt, caption='WriteCSV output (excerpt, from the run above)')}
<p><code>ops.HouseStatement</code> builds the same statement for a house account such as <code>@FeeIncome</code>. The statement lists posted entries only, dated by when they took effect, so a standing order appears on the day it posted. The closing balance is checked against Blnk’s own balance at the end of the period: a statement that doesn’t add up is an error, never a wrong document. Fees appear as their own lines, as on a bank statement.</p>
''', 'A downloadable month for Ada, checked against the ledger.')


# --- Money in & out
group('Wallet · Money in and out')
section('topup', 'Top up through Paystack', f'''
{code('topup')}
{entries([('@PaystackClearing', '150,000.00', ''), ('@FeeIncome', '', '2,000.00'), ('Ada · NGN', '', '148,000.00')],
 'The fee leg and the customer leg post together. Paystack now owes you ₦150,000 until it settles.')}
<p>Set <code>Pending: true</code> to hold a deposit for checks (say, a first large top-up) and approve it later with <code>wallet.Confirm</code>.</p>
''', 'Paystack confirms a payment; credit Ada exactly once.')

section('p2p', 'Send money to a friend', f'''
{code('p2p')}
{entries([('Ada · NGN', '12,510.00', ''), ('Tunde · NGN', '', '12,500.00'), ('@FeeIncome', '', '10.00')])}
<p>A quote runs the same checks as the real transfer (fee, limits, frozen accounts and ledgers) and then has Blnk dry-run it, so <code>Allowed</code> is Blnk’s own answer and what Ada confirms is what she is charged. Nothing posts.</p>
<p><code>From</code> and <code>To</code> are <code>AccountID</code>s. A transfer can also name a house account, written <code>ledger.AccountID("@Name")</code>; deposits, withdrawals, holds and exchanges accept customer accounts only and refuse anything else with <code>ErrWrongKind</code>.</p>
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
<p><code>QuoteExchange</code> shows the amounts, fee and balance after, with Blnk dry-running both legs, before anything moves. Give <code>Buy</code> instead of <code>Sell</code> to land an exact amount (“receive exactly ₦100,000”). The bought amount rounds down and a computed sell amount rounds up, so rounding never costs you.</p>
''', 'Ada converts dollars to naira at the rate you quoted.')

section('crypto', 'Crypto', f'''
{code('crypto')}
<p>Crypto uses each chain’s smallest unit (satoshi, wei), so ledger balances match the chain exactly. For stablecoins on several chains, note that decimals differ by chain (USDT is 6 on Tron and Ethereum, 18 on BNB Chain): keep one precision per ledger currency and convert at the edge, or give each chain its own currency code.</p>
''', 'Exact satoshi accounting, with display that never overstates a balance.')

# --- Commerce
group('Wallet · Commerce')
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
<p>Partial refunds are linked to the original with <code>meta_data.refund_of</code>, and they are added up before each new one, so over-refunding is refused, and a retried refund replays instead of counting twice. A transaction can be reversed in full with <code>Refund</code> or in parts with <code>RefundPartial</code>, not both. If refunds for the same transaction can run concurrently, serialise them (one worker or a row lock).</p>
''', 'Return part of a payment when an item is out of stock.')

# --- Savings & payroll
group('Wallet · Savings and payroll')
section('pots', 'Savings pots and standing orders', f'''
{code('pots')}
<p>A pot is a sub-account in the same currency and identity, with an optional goal. Blnk has no native recurrence, so a standing order is a set of scheduled transfers sharing <code>meta_data.recurring_reference</code>; calling <code>Schedule</code> again with the same input is safe.</p>
''', 'Ada saves for rent automatically every month.')

section('payroll', 'Payroll', f'''
{code('payroll')}
<p><code>Atomic</code> makes the batch all-or-nothing. Up to 10,000 transfers per batch; <code>Async</code> processes in the background with the outcome sent as a <code>bulk_transaction.*</code> webhook.</p>
''', 'A merchant pays its staff in one batch.')

# --- Fees and limits
group('Wallet · Fees and limits')
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
<p>Limits apply to money leaving customer accounts: transfers, withdrawals, holds, splits, exchanges and escrow (bulk batches are not checked). A pending hold counts while held and comes back if cancelled. Days and months reset in <code>Config.Location</code>.</p>
''', 'Per-tier caps, checked before any money moves.')

# --- Reporting
group('Financial ops · Books')
section('trial', 'Trial balance', f'''
{code('trial')}
<p>In double entry every currency nets to zero. Run this nightly and alert when <code>Balanced</code> is false; <code>CustomerFunds</code> is the total you owe customers, the figure your safeguarding account must cover. <code>House</code> lists every house account as a <code>HouseAccount</code>, with its type and <code>Natural</code> balance.</p>
''', 'One call to prove the books add up.')

# --- Back office
group('Financial ops · Back office')
section('settlement', 'Provider settlement', f'''
{code('settlement')}
<p>Each provider has its own clearing account, so after recording settlements its balance is exactly what that provider still owes you (or you owe it) and can be ticked off against its report. Use <code>SettlementPayout</code> for payout providers you prefund.</p>
''', 'Clear Paystack’s receivable when the money reaches your bank.')

section('suspense', 'Unidentified money', code('suspense'), 'Hold money you can’t match yet; claim it for its owner once support knows whose it is, or return it to the provider.')
section('adjust', 'Manual corrections', f'''
{code('adjust')}
<p>Every adjustment records its reason, requester and approver in metadata, and the same person can’t do both.</p>
''', 'Corrections with a second pair of eyes.')

section('watch', 'Balance alerts', f'''
{code('watch')}
<p>Watch a house account with <code>House</code>, as here, or a customer account with <code>AccountID</code>; set one, not both.</p>
''', 'Get told when a float runs high or low.')

section('reconcile', 'Reconciliation', f'''
{code('reconcile')}
<p>Without <code>RuleIDs</code>, a rule matching reference, currency and amount exactly is created once and reused. Up to 10,000 records go inline; larger exports upload as a CSV with <code>File</code>. Blnk reports counts, not the matched lines.</p>
{note('Payments that carried a fee are stored by Blnk as legs (<code>ref-1</code>, <code>ref-2</code>), so they don’t match a provider line one-to-one. Try <code>Strategy: "one_to_many"</code> with <code>GroupingCriteria: "parent_transaction"</code>, and check the counts against your own Core version before relying on them.', 'warn')}
''', 'Match Paystack’s report against the ledger.')

# --- Controls
group('Financial ops · Controls')
section('freeze', 'Freeze and close', f'''
{code('freeze')}
<p>A whole ledger can be frozen too. Then no money can leave any of its customer accounts, while deposits into them and postings to house accounts carry on:</p>
{code('freezeledger')}
<p><code>ops.CloseAccount</code> is final: a closed account is never reopened (open a new one). It needs a <code>Reason</code>, a <code>RequestedBy</code> and an <code>ApprovedBy</code> who is someone else, and refuses while anything is pending or scheduled, while a pot under the account is open, or while an escrow it pays into or out of still holds money. Then it moves any remaining balance to <code>SweepTo</code> and marks the account closed. A frozen account can still be closed, but nothing can be swept out of a frozen ledger, and a closed account can’t be frozen or unfrozen. Money sent to a closed account later (a late provider refund, say) is refused with <code>ErrAccountClosed</code>; hold it with <code>HoldInSuspense</code>, then claim it for the customer’s new account or return it to the provider.</p>
<p>Each <code>Service</code> caches a ledger’s status for <code>Config.LedgerStatusTTL</code> (30 seconds by default), so a freeze made by another process applies within that time. Pending transactions created before the freeze can still be confirmed.</p>
''')


# --- Integration
group('Integration')
section('webhooks', 'Webhooks', f'''
{code('webhook')}
<p><code>balance.created</code> sets <code>e.Account</code> for a customer account and <code>e.House</code> for a house account. With <code>Secret</code> set, signatures (<code>X-Blnk-Signature</code>, HMAC-SHA256 of <code>timestamp.body</code>) and a five-minute timestamp window are checked; without it nothing is verified, so make sure it is set in production. Blnk delivers at least once and retries on errors, so make handlers idempotent, for example keyed on <code>e.Receipt.TransactionID</code> plus <code>e.Name</code>. An expired hold arrives as <code>transaction.void</code>.</p>
''', 'React to what happens in the ledger.')

section('observe', 'Logging, metrics and tracing', f'''
{code('observe')}
<p><code>Observer</code> receives every call’s name, context and error, which is all you need for metrics or one OpenTelemetry span per call. Business refusals (insufficient funds, limits) log at Warn, failures at Error.</p>
''')

section('testing', 'Testing your app', f'''
{code('testing', 'rent_test.go')}
<p><code>ledgertest</code> is an in-memory Blnk Core: your tests run the real <code>Service</code> code, so they exercise the same requests production sends. <code>ledgertest.New</code> creates a ledger for the test and sets <code>Config.LedgerID</code> to it. It behaves like Core 0.15 where that matters (fee legs, stored ids, required descriptions) and lets you move time forward to expire holds and run scheduled transfers. To check against a real Core (it writes test data):</p>
{raw("BLNK_LIVE_URL=http://localhost:5001 go test ./ledger -run TestLiveCore -v")}
''', 'Fast tests with no Docker, using the real request code.')

# --- Reference
group('Reference')
section('versions', 'Blnk versions and known limits', f'''
{table(['', 'Blnk Core'], [
 ['Minimum', '<strong>0.15.4</strong>: dry run (0.15.3) for every quote, and the current error codes'],
 ['Tested on', '<strong>0.15.6</strong> and 0.15.4, built from source in CI with Postgres 16 and Redis 7'],
])}
<p>Core before 0.15.3 ignores <code>dry_run</code> and would post a quoted transaction, so it isn’t supported.</p>
<ul class="limits">
<li><strong>Fee legs.</strong> A movement with a fee is stored as legs (<code>ref-1</code>, <code>ref-2</code>) with no parent record. The library rebuilds the parent for lookups and retries, and disputes or refunds pick the recipient’s leg, but raw Blnk queries and reconciliation see legs. A pending one is confirmed in full (or one leg at a time), never partially.</li>
<li><strong>House account ids.</strong> Blnk stores <code>@FeeIncome</code>-style accounts by balance id; receipts and history show the id, statements show the name. Read them through <code>ops.GetHouseAccount</code>; <code>wallet.GetAccount</code> refuses them.</li>
<li><strong>Ledger keys.</strong> Blnk has no unique ledger names, so two processes creating the same <code>Key</code> at once can both succeed; the oldest then wins and the other is marked <code>duplicate</code>. Create ledgers at provisioning time.</li>
<li><strong>Account and ledger status</strong> are enforced by this library, not by Blnk, so movements Blnk posts by itself (scheduled transfers, auto-capture, hold expiry) and calls made to Blnk directly aren’t checked. Each process caches a ledger’s status for <code>LedgerStatusTTL</code>.</li>
<li><strong>Concurrent partial refunds</strong> of one transaction can both pass the remaining-amount check; serialise them.</li>
<li><strong>Balance alerts</strong> fire after every update while the condition holds; debounce them.</li>
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
<meta name="description" content="humanblnk: a wallet and financial-operations domain layer for Go, built on top of Blnk.">
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
.brand small.by {{ margin-top: 6px; font-size: 13px }}
header.hero p.byline {{ margin-top: 12px; font-size: 15px }}
footer.foot {{ margin-top: 64px; padding-top: 24px; border-top: 1px solid var(--rule); color: var(--muted); font-size: 15px }}
footer.foot p {{ margin: 4px 0 }}
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
  <div class="brand">humanblnk<small>Wallet &amp; financial-operations domain layer on Blnk</small><small class="by">by <a href="https://github.com/otyang">Otyang</a></small></div>
  <details id="toc" open><summary>Contents</summary>{navhtml}</details>
</nav>
<main>
<header class="hero">
  <h1>Wallets and financial operations on Blnk</h1>
  <p>A practical guide to humanblnk, the domain layer between your product and Blnk, told through Oja, a wallet business in Nigeria: naira, dollars and bitcoin, from a customer’s signup to the finance team’s month-end close.</p>
  <p class="byline">By <a href="https://github.com/otyang">Otyang</a> · <a href="https://github.com/otyang/humanblnk">github.com/otyang/humanblnk</a> · <a href="examples/">Detailed examples for every use case</a></p>
</header>
{''.join(sections)}
<footer class="foot">
  <p><strong>humanblnk</strong> is created and maintained by <a href="https://github.com/otyang">Otyang</a>. Source: <a href="https://github.com/otyang/humanblnk">github.com/otyang/humanblnk</a>.</p>
  <p>An independent project, not affiliated with or endorsed by Blnk Finance.</p>
</footer>
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
