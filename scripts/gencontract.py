#!/usr/bin/env python3
"""Builds CONTRACT.md from the Go source, so the contract can't drift.

Run scripts/gendocs.sh (or `make docs`) rather than this file directly.
Every exported type, function, variable and constant group of package ledger
appears; curated sections order them, and anything new lands in "Other".
"""
import glob
import re
import sys

root = sys.argv[1] if len(sys.argv) > 1 else '.'
files = sorted(f for f in glob.glob(f'{root}/ledger/*.go') if not f.endswith('_test.go'))
src = '\n'.join(open(f).read() for f in files)
blnk_src = '\n'.join(open(f).read() for f in sorted(glob.glob(f'{root}/blnk/*.go')) if not f.endswith('_test.go'))


def with_doc(text, start):
    """Extend a match backwards over its doc comment."""
    lines = text[:start].split('\n')
    doc = []
    for line in reversed(lines[:-1]):
        if line.startswith('//'):
            doc.append(line)
        else:
            break
    return '\n'.join(reversed(doc)) + ('\n' if doc else '')


def type_block(name, text=src):
    for pat in (r'^type ' + name + r' (?:struct|interface) \{\n.*?^\}\n', r'^type ' + name + r'\b[^\n]*\n'):
        m = re.search(pat, text, re.M | re.S)
        if m:
            return with_doc(text, m.start()) + m.group(0)
    return None


def const_blocks_for(typ):
    out = []
    for m in re.finditer(r'^const \(\n(.*?)^\)\n', src, re.M | re.S):
        if re.search(r'^\t\w+\s+' + typ + r'\b', m.group(1), re.M):
            out.append(with_doc(src, m.start()) + m.group(0))
    return out


ns = open(f'{root}/ledger/namespaces.go').read()
interface = ns[ns.index('// Ledger is the interface'):ns.index('// The namespace implementations')].rstrip() + '\n'

groups = [
    ('Money and display', ['Money', 'DisplayRule', 'Rounding']),
    ('Accounts and customers', ['OpenAccountInput', 'Account', 'AccountKind', 'AccountQuery', 'AccountPage',
                                'CloseInput', 'CustomerInput', 'Customer', 'PotInput', 'Pot']),
    ('Moving money', ['DepositInput', 'WithdrawInput', 'TransferInput', 'SplitInput', 'SplitPart',
                      'ExchangeInput', 'ExchangeReceipt', 'HoldInput', 'BulkTransferInput', 'BulkReceipt',
                      'RecurringInput', 'Interval', 'ConfirmItem', 'BatchResult']),
    ('Refunds, disputes, escrow', ['RefundInput', 'PartialRefundInput', 'DisputeInput', 'DisputeOutcome',
                                   'EscrowInput', 'EscrowSettleInput', 'Escrow']),
    ('Fees, quotes, limits', ['FeeFunc', 'FeeRequest', 'FeeKind', 'FeeRule', 'FeeTier', 'Quote', 'Limit', 'LimitUsage']),
    ('Results and reads', ['Receipt', 'Leg', 'EntryStatus', 'HistoryQuery', 'Direction', 'HistoryPage',
                           'Statement', 'StatementLine', 'TrialBalance']),
    ('Back office', ['SettlementInput', 'SettlementDirection', 'SettlementReceipt', 'SuspenseInput', 'AllocateInput',
                     'AdjustInput', 'AdjustmentType', 'WatchInput', 'Watch', 'ReconcileInput', 'ExternalRecord',
                     'Reconciliation', 'MatchingRule', 'MatchingCriterion']),
    ('Webhooks and observability', ['Event', 'BulkEvent', 'WebhookConfig', 'Observer', 'ObserverFunc']),
    ('Service and configuration', ['Service', 'Config', 'OpError']),
]
namespace_types = {'Ledger', 'Accounts', 'Customers', 'Payments', 'Pending', 'Transactions', 'Disputes',
                   'Escrows', 'Operations', 'Reconciliations'}
all_types = set(re.findall(r'^type ([A-Z]\w*)', src, re.M))
covered = {n for _, names in groups for n in names} | namespace_types
groups.append(('Other', sorted(all_types - covered)))

out = [f'''# Ledger service contract

Generated from the Go source by `scripts/gencontract.py` (run `make docs`); the
source is authoritative. It lists the `Ledger` interface, its namespaces, and
every exported type, function, value and constant they use. For explanations
and real-world examples, see the guide at https://otyang.github.io/humanblnk/.

## Importing

```go
import "github.com/otyang/humanblnk"         // everything below, re-exported, plus humanblnk.New
import "github.com/otyang/humanblnk/ledger"  // the same types, defined here
```

`humanblnk` re-exports package `ledger` with type aliases (`humanblnk.Money`
is `ledger.Money`), renames `ledger.New` to `humanblnk.NewService`, and adds:

```go
// New connects to the Blnk Core at blnkURL and returns the ledger service.
func New(blnkURL string, cfg Config, opts ...Option) *Service
```

## Interface

```go
{interface}```
''']

seen_consts = set()
for title, names in groups:
    if not names:
        continue
    parts = []
    for n in names:
        b = type_block(n)
        if b is None:
            b = type_block(n, blnk_src)
            if b is None:
                sys.exit(f'gencontract: type {n} not found')
            b = f'// in package blnk (aliased in ledger)\n{b}'
        parts.append(b.rstrip('\n'))
        for c in const_blocks_for(n):
            if c not in seen_consts:
                seen_consts.add(c)
                parts.append(c.rstrip('\n'))
    out.append(f'\n## {title}\n\n```go\n' + '\n\n'.join(parts) + '\n```\n')

# Untyped constant groups (system accounts, event names) and package-level values.
extra = []
for m in re.finditer(r'^const \(\n(.*?)^\)\n', src, re.M | re.S):
    block = with_doc(src, m.start()) + m.group(0)
    if block not in seen_consts and re.search(r'^\t[A-Z]', m.group(1), re.M):
        extra.append(block.rstrip('\n'))
out.append('\n## Constants\n\n```go\n' + '\n\n'.join(extra) + '\n```\n')

values = []
for m in re.finditer(r'^var \(\n(.*?)^\)\n', src, re.M | re.S):
    if re.search(r'^\t[A-Z]', m.group(1), re.M):
        values.append((with_doc(src, m.start()) + m.group(0)).rstrip('\n'))
for m in re.finditer(r'^var ([A-Z]\w*) = [^\n]*\n', src, re.M):
    values.append((with_doc(src, m.start()) + m.group(0)).rstrip('\n'))
out.append('\n## Values and errors\n\n```go\n' + '\n\n'.join(values) + '\n```\n')

funcs = []
for m in re.finditer(r'^func ([A-Z]\w*)\((.*?)\) ?([^{\n]*)\{', src, re.M):
    funcs.append((with_doc(src, m.start()) + f'func {m.group(1)}({m.group(2)}) {m.group(3).strip()}').rstrip())
out.append('\n## Functions\n\n```go\n' + '\n\n'.join(funcs) + '\n```\n')

out.append('\nEvery money-movement method requires `Reference`. Resending the same reference returns the original with `Replayed: true` instead of posting twice.\n')
open(f'{root}/CONTRACT.md', 'w').write(''.join(out))
print(f'CONTRACT.md: {len(all_types)} types, {len(funcs)} functions')
