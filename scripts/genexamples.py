#!/usr/bin/env python3
"""Builds examples/*.html from examples/source.

Each page is examples/source/<page>.md (front matter, then a small Markdown
subset) plus the samples in examples/source/<page>_test.go. A sample sits
between "// sample: <page>/<name>" and "// end"; "@sample <name>" in the
Markdown shows its code and the output it printed when the tests ran.

Run scripts/gendocs.sh (make docs) rather than this file directly.
"""
import glob, html, json, os, re, subprocess, sys, tempfile, textwrap

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, 'examples', 'source')
OUT = os.path.join(ROOT, 'examples')

GROUPS = [
    ('setup', 'Setup and integration', 'Connect to Blnk, handle money and errors, and wire the library into your app.'),
    ('wallet', 'Wallet', 'What customers do: open accounts, add money, pay, cash out, hold, save, buy, dispute.'),
    ('ops', 'Financial operations', 'What the business does: ledgers, its own accounts, controls, settlement and the books.'),
]

# ---------------------------------------------------------------- run the samples
def run_samples():
    fd, path = tempfile.mkstemp(suffix='.json')
    os.close(fd)
    env = dict(os.environ, EXAMPLES_OUT=path)
    r = subprocess.run(['go', 'test', './examples/source', '-count=1'], cwd=ROOT, env=env,
                       capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit('genexamples: the example tests failed:\n' + r.stdout + r.stderr)
    outputs = json.load(open(path))
    os.remove(path)
    return outputs

def block_end(src, start):
    """Index just past the brace block that opens at or after start."""
    depth, i = 0, src.index('{', start)
    while i < len(src):
        c = src[i]
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0:
                return i + 1
        elif c in '"`':
            j = i + 1
            while src[j] != c:
                j += 2 if (c == '"' and src[j] == '\\') else 1
            i = j
        i += 1
    return len(src)

def load_helpers():
    """Small functions and closures the samples call, by file, so a page can show them."""
    helpers = {}  # file -> {name: code}
    for f in sorted(glob.glob(os.path.join(SRC, '*_test.go'))):
        src = open(f).read()
        found = {}
        for m in re.finditer(r'^func ([a-z]\w*)\(', src, re.M):
            if m.group(1) in ('check', 'begin', 'end', 'setup'):
                continue
            doc = re.search(r'((?:^//[^\n]*\n)*)\Z', src[:m.start()], re.M).group(1)
            found[m.group(1)] = (doc + src[m.start():block_end(src, m.start())]).strip()
        for m in re.finditer(r'^\t(\w+) := func\(', src, re.M):
            found[m.group(1)] = textwrap.dedent(src[m.start():block_end(src, m.start())]).strip()
        helpers[os.path.basename(f)] = found
    return helpers

def load_samples():
    samples = {}
    for f in sorted(glob.glob(os.path.join(SRC, '*_test.go'))):
        src = open(f).read()
        for m in re.finditer(r'^[ \t]*// sample: (\S+)\n(.*?)^[ \t]*// end\b', src, re.M | re.S):
            key = m.group(1)
            if key in samples:
                sys.exit(f'genexamples: sample {key} is defined twice')
            samples[key] = (textwrap.dedent(m.group(2)).strip('\n'), os.path.basename(f))
    return samples

# ---------------------------------------------------------------- highlighting
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
        elif kind == 'id' and text in KW:
            out.append(f'<span class="k">{esc}</span>')
        elif kind == 'id' and text in BUILTIN:
            out.append(f'<span class="b">{esc}</span>')
        elif kind == 'id' and text[0].isupper():
            out.append(f'<span class="t">{esc}</span>')
        else:
            out.append(esc)
    return ''.join(out)

# ---------------------------------------------------------------- markdown subset
def inline(text):
    codes = []
    def keep(m):
        codes.append(f'<code>{html.escape(m.group(1))}</code>')
        return f'\x00{len(codes) - 1}\x00'
    text = re.sub(r'`([^`]+)`', keep, text)
    text = html.escape(text, quote=False)
    text = re.sub(r'\*\*(.+?)\*\*', r'<strong>\1</strong>', text)
    text = re.sub(r'(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?![\w*])', r'<em>\1</em>', text)
    text = re.sub(r'\[([^\]]+)\]\(([^)\s]+)\)', r'<a href="\2">\1</a>', text)
    return re.sub(r'\x00(\d+)\x00', lambda m: codes[int(m.group(1))], text)

def slug(text):
    return re.sub(r'[^a-z0-9]+', '-', re.sub(r'<[^>]+>', '', text).lower()).strip('-')

def render(body, page, samples, outputs, toc):
    out, lines, i = [], body.split('\n'), 0
    used = set()
    while i < len(lines):
        line = lines[i]
        if not line.strip():
            i += 1
            continue
        if line.startswith('```'):
            lang = line[3:].strip()
            j = i + 1
            while j < len(lines) and not lines[j].startswith('```'):
                j += 1
            code = '\n'.join(lines[i + 1:j])
            body_html = highlight(code) if lang == 'go' else html.escape(code)
            out.append(f'<figure class="code"><pre><code>{body_html}</code></pre></figure>')
            i = j + 1
            continue
        if line.startswith('@sample '):
            arg = line[len('@sample '):].strip()
            name, _, caption = arg.partition(' | ')
            key = name if '/' in name else f'{page}/{name}'
            if key not in samples:
                sys.exit(f'genexamples: {page}.md uses missing sample {key}')
            used.add(key)
            cap = f'<figcaption>{inline(caption)}</figcaption>' if caption else ''
            out.append(f'<figure class="code">{cap}<pre><code>{highlight(samples[key])}</code></pre></figure>')
            got = outputs.get(key, '').rstrip('\n')
            if got:
                out.append(f'<figure class="output"><figcaption>Output</figcaption><pre>{html.escape(got)}</pre></figure>')
            i += 1
            continue
        m = re.match(r'(#{2,3}) (.+)', line)
        if m:
            level, text = len(m.group(1)), inline(m.group(2))
            sid = slug(text)
            if level == 2:
                toc.append((sid, text))
                out.append(f'<h2 id="{sid}"><a href="#{sid}">{text}</a></h2>')
            else:
                out.append(f'<h3 id="{sid}">{text}</h3>')
            i += 1
            continue
        if line.startswith('|'):
            rows = []
            while i < len(lines) and lines[i].startswith('|'):
                rows.append([c.strip() for c in lines[i].strip().strip('|').split('|')])
                i += 1
            head, rest = rows[0], [r for r in rows[1:] if not re.match(r'^:?-+:?$', r[0] or '-')]
            th = ''.join(f'<th>{inline(c)}</th>' for c in head)
            tr = ''.join('<tr>' + ''.join(f'<td>{inline(c)}</td>' for c in r) + '</tr>' for r in rest)
            out.append(f'<div class="tablewrap"><table><thead><tr>{th}</tr></thead><tbody>{tr}</tbody></table></div>')
            continue
        m = re.match(r'> (note|warn|tip): (.*)', line)
        if m:
            kind, text = m.group(1), [m.group(2)]
            i += 1
            while i < len(lines) and lines[i].startswith('> '):
                text.append(lines[i][2:])
                i += 1
            out.append(f'<aside class="{kind}">{inline(" ".join(text))}</aside>')
            continue
        if re.match(r'(- |\d+\. )', line):
            ordered = bool(re.match(r'\d+\. ', line))
            items = []
            while i < len(lines) and (re.match(r'(- |\d+\. )', lines[i]) or (lines[i].startswith('  ') and items)):
                if lines[i].startswith('  ') and items:
                    items[-1] += ' ' + lines[i].strip()
                else:
                    items.append(re.sub(r'^(- |\d+\. )', '', lines[i]))
                i += 1
            tag = 'ol' if ordered else 'ul'
            out.append(f'<{tag}>' + ''.join(f'<li>{inline(t)}</li>' for t in items) + f'</{tag}>')
            continue
        para = []
        while i < len(lines) and lines[i].strip() and not re.match(r'(#{2,3} |```|@sample |\||> (note|warn|tip): |- |\d+\. )', lines[i]):
            para.append(lines[i].strip())
            i += 1
        out.append(f'<p>{inline(" ".join(para))}</p>')
    return '\n'.join(out), used

# ---------------------------------------------------------------- pages
def load_pages():
    pages = []
    for f in sorted(glob.glob(os.path.join(SRC, '*.md'))):
        name = os.path.basename(f)[:-3]
        text = open(f).read()
        head, sep, body = text.partition('\n---\n')
        if not sep:
            sys.exit(f'genexamples: {name}.md needs front matter ending in ---')
        meta = dict(re.match(r'(\w+):\s*(.*)', l).groups() for l in head.strip().split('\n'))
        for k in ('title', 'group', 'order', 'lead'):
            if k not in meta:
                sys.exit(f'genexamples: {name}.md is missing "{k}"')
        if meta['group'] not in [g for g, _, _ in GROUPS]:
            sys.exit(f'genexamples: {name}.md has unknown group {meta["group"]}')
        pages.append(dict(meta, name=name, body=body, order=int(meta['order'])))
    pages.sort(key=lambda p: ([g for g, _, _ in GROUPS].index(p['group']), p['order']))
    return pages

HEAD = '''<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>{title}</title>
<meta name="description" content="{desc}">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Schibsted+Grotesk:wght@500;700&family=Source+Sans+3:wght@400;600&family=JetBrains+Mono:wght@400;500&display=swap">
<link rel="stylesheet" href="style.css">
</head>
<body>
<header class="top"><div class="wrap">
  <a class="brand" href="index.html">humanblnk <span>examples</span></a>
  <nav><a href="index.html">All examples</a><a href="../docs.html">Guide</a><a href="https://github.com/otyang/humanblnk">GitHub</a></nav>
</div></header>
'''
FOOT = '''<footer class="foot"><div class="wrap">
  <p><strong>humanblnk</strong>: a wallet and financial-operations domain layer for Go, built on top of Blnk. By <a href="https://github.com/otyang">Otyang</a>.</p>
  <p>Every sample on this page runs against the library's in-memory Blnk Core when the tests run; the output shown is what it printed. In the samples, <code>check(err)</code> stands for your own error handling, and <code>ledger</code> and <code>currency</code> are <code>github.com/otyang/humanblnk/ledger</code> and <code>github.com/otyang/humanblnk/currency</code>.</p>
</div></footer>
</body>
</html>
'''

def helpers_for(used, samples, helpers):
    """Helper code the page's samples call, shared helpers first."""
    shared = helpers.get('helpers_test.go', {})
    out, seen = [], set()
    def visit(code):
        for name in sorted(set(re.findall(r'\b([a-z]\w*)\(', code))):
            if name in seen:
                continue
            for src_file in [f for f in helpers if f != 'helpers_test.go'] + ['helpers_test.go']:
                if name in helpers[src_file] and src_file in owners:
                    seen.add(name)
                    out.append(helpers[src_file][name])
                    visit(helpers[src_file][name])
                    break
    owners = {'helpers_test.go'} | {samples[k][1] for k in used}
    for k in sorted(used):
        visit(samples[k][0])
    shared_first = [c for c in out if any(c == v for v in shared.values())] + [c for c in out if not any(c == v for v in shared.values())]
    return shared_first

def page_html(p, prev, nxt, samples, outputs, helpers):
    toc = []
    code_only = {k: v[0] for k, v in samples.items()}
    body, used = render(p['body'], p['name'], code_only, outputs, toc)
    extra = helpers_for(used, samples, helpers)
    helper_html = ''
    if extra:
        helper_html = ('<details class="helpers"><summary>Helpers these samples use</summary>'
                       '<p>Small functions the samples on this page call, shown in full. <code>check(err)</code> stands for your own error handling.</p>'
                       + ''.join(f'<figure class="code"><pre><code>{highlight(c)}</code></pre></figure>' for c in extra)
                       + '</details>')
    group = next(t for g, t, _ in GROUPS if g == p['group'])
    toc_html = ''.join(f'<a href="#{sid}">{t}</a>' for sid, t in toc)
    pn = ''
    if prev:
        pn += f'<a class="prev" href="{prev["name"]}.html"><small>Previous</small>{html.escape(prev["title"])}</a>'
    if nxt:
        pn += f'<a class="next" href="{nxt["name"]}.html"><small>Next</small>{html.escape(nxt["title"])}</a>'
    doc = HEAD.format(title=html.escape(p['title'] + ' · humanblnk'), desc=html.escape(p['lead'])) + f'''<main class="wrap page">
<p class="crumb"><a href="index.html">Examples</a> · {group}</p>
<h1>{html.escape(p["title"])}</h1>
<p class="lead">{inline(p["lead"])}</p>
{f'<nav class="toc" aria-label="On this page"><span>On this page</span>{toc_html}</nav>' if toc else ''}
{helper_html}
<article>
{body}
</article>
<nav class="prevnext">{pn}</nav>
</main>
''' + FOOT
    return doc, used

def index_html(pages):
    parts = []
    for g, title, blurb in GROUPS:
        cards = ''.join(f'<a class="card" href="{p["name"]}.html"><b>{html.escape(p["title"])}</b><span>{inline(p["lead"])}</span></a>'
                        for p in pages if p['group'] == g)
        parts.append(f'<section class="group" id="{g}"><h2>{title}</h2><p class="blurb">{blurb}</p><div class="cards">{cards}</div></section>')
    return HEAD.format(title='humanblnk examples', desc='Detailed, tested examples of every humanblnk use case.') + f'''<main class="wrap">
<h1>Examples</h1>
<p class="lead">Every use case of humanblnk, a wallet and financial-operations domain layer for Go built on top of Blnk, with full code and the real output. Each page shows every way of doing the task, the options, the entries it makes in Blnk, and the errors to expect.</p>
<p class="lead small">The code calls two handles: <code>wallet := svc.Wallet()</code> for what customers do and <code>ops := svc.Ops()</code> for what the business does. New here? Start with <a href="setup.html">Setup</a>, then <a href="customer-accounts.html">Create customer accounts</a>.</p>
{''.join(parts)}
</main>
''' + FOOT

CSS = '''/* humanblnk examples: one reading column, code and its output stacked beside the prose. Matches the guide's palette. */
:root {
  --bg: #fbfaf7; --panel: #f2efe8; --ink: #1d2226; --muted: #5f676e; --rule: #e1ddd3;
  --accent: #0a6b52; --accent-soft: #e2f0eb; --warn: #8a5a00; --warn-soft: #fbf0d9;
  --code-bg: #1f2428; --code-ink: #e6e6e3; --out-bg: #f6f4ee; --out-ink: #2b3136;
  --c-com: #8b949e; --c-str: #a5d6a7; --c-kw: #ff9e7a; --c-type: #8ec7ff; --c-num: #f2cc60; --c-bi: #d7a8ff;
  --display: "Schibsted Grotesk", "Helvetica Neue", Arial, sans-serif;
  --body: "Source Sans 3", "Helvetica Neue", Arial, sans-serif;
  --mono: "JetBrains Mono", ui-monospace, Menlo, monospace;
  color-scheme: light;
}
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) {
  --bg: #14181b; --panel: #1b2024; --ink: #e8eae6; --muted: #a2aaa6; --rule: #2b3236;
  --accent: #5fd0a8; --accent-soft: #163329; --warn: #f0c060; --warn-soft: #33290f;
  --code-bg: #0f1214; --out-bg: #191e22; --out-ink: #d6dad6; color-scheme: dark } }
:root[data-theme="dark"] {
  --bg: #14181b; --panel: #1b2024; --ink: #e8eae6; --muted: #a2aaa6; --rule: #2b3236;
  --accent: #5fd0a8; --accent-soft: #163329; --warn: #f0c060; --warn-soft: #33290f;
  --code-bg: #0f1214; --out-bg: #191e22; --out-ink: #d6dad6; color-scheme: dark }
* { box-sizing: border-box }
html { scroll-padding-top: 16px }
body { margin: 0; background: var(--bg); color: var(--ink); font: 17px/1.6 var(--body) }
a { color: var(--accent) }
a:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px }
.wrap { max-width: 920px; margin: 0 auto; padding-left: max(16px, env(safe-area-inset-left, 0px)); padding-right: max(16px, env(safe-area-inset-right, 0px)) }
header.top { border-bottom: 1px solid var(--rule); background: var(--panel) }
header.top .wrap { display: flex; flex-wrap: wrap; gap: 8px 24px; align-items: baseline; justify-content: space-between; padding-block: 14px }
.brand { font: 700 19px var(--display); color: var(--ink); text-decoration: none; letter-spacing: -.01em }
.brand span { color: var(--muted); font-weight: 500 }
header.top nav { display: flex; flex-wrap: wrap; gap: 4px 18px; font-size: 15px }
header.top nav a { text-decoration: none }
main { padding-block: 36px 72px }
h1 { font: 700 clamp(30px, 4.4vw, 42px)/1.1 var(--display); letter-spacing: -.02em; margin: 0 0 10px; text-wrap: balance }
h2 { font: 700 25px/1.25 var(--display); letter-spacing: -.01em; margin: 44px 0 8px; padding-top: 28px; border-top: 1px solid var(--rule); text-wrap: balance }
h2 a { color: inherit; text-decoration: none }
h2 a:hover::after { content: " #"; color: var(--muted) }
h3 { font: 700 19px/1.3 var(--display); margin: 28px 0 6px; text-wrap: balance }
.crumb { margin: 0 0 6px; font-size: 14px; color: var(--muted) }
.crumb a { color: var(--muted) }
.lead { margin: 0 0 14px; color: var(--muted); font-size: 19px; max-width: 64ch }
.lead.small { font-size: 16.5px }
p, ul, ol { max-width: 70ch }
li { margin: 5px 0 }
code { font: 500 .86em var(--mono); background: var(--panel); border: 1px solid var(--rule); padding: 1px 5px; border-radius: 4px }
nav.toc { display: flex; flex-wrap: wrap; gap: 6px 16px; margin: 18px 0 8px; padding: 12px 16px; border: 1px solid var(--rule); border-radius: 8px; font-size: 15px; background: var(--panel) }
nav.toc span { width: 100%; font: 600 11.5px var(--body); text-transform: uppercase; letter-spacing: .08em; color: var(--muted) }
nav.toc a { text-decoration: none }
figure { margin: 16px 0 }
figure.code { border-radius: 8px; overflow: hidden; background: var(--code-bg) }
figure.code + figure.output { margin-top: -10px }
figure figcaption { font: 500 12px var(--mono); color: var(--c-com); padding: 10px 16px 0 }
figure.code pre { margin: 0; padding: 14px 16px 16px; overflow-x: auto }
figure.code code { background: none; border: 0; padding: 0; color: var(--code-ink); font: 13.5px/1.6 var(--mono); white-space: pre; tab-size: 4 }
figure.output { background: var(--out-bg); border: 1px solid var(--rule); border-top: 0; border-radius: 0 0 8px 8px }
figure.output figcaption { color: var(--muted); text-transform: uppercase; letter-spacing: .08em; font-family: var(--body); font-weight: 600 }
figure.output pre { margin: 0; padding: 6px 16px 14px; overflow-x: auto; font: 13px/1.55 var(--mono); color: var(--out-ink) }
.c { color: var(--c-com); font-style: italic } .s { color: var(--c-str) } .k { color: var(--c-kw) }
.t { color: var(--c-type) } .n { color: var(--c-num) } .b { color: var(--c-bi) }
.tablewrap { overflow-x: auto; margin: 16px 0 }
table { border-collapse: collapse; width: 100%; font-size: 15px }
th { text-align: left; font: 600 12px var(--body); text-transform: uppercase; letter-spacing: .06em; color: var(--muted); padding: 8px 10px; border-bottom: 2px solid var(--ink) }
td { padding: 8px 10px; border-bottom: 1px solid var(--rule); vertical-align: top }
td code { white-space: nowrap }
aside { border-radius: 8px; padding: 12px 16px; margin: 16px 0; font-size: 15.5px; max-width: 70ch }
aside.note, aside.tip { background: var(--accent-soft) }
aside.warn { background: var(--warn-soft) }
aside.tip::before, aside.warn::before { font: 600 12px var(--body); text-transform: uppercase; letter-spacing: .06em; margin-right: 8px }
aside.tip::before { content: "Tip"; color: var(--accent) }
aside.warn::before { content: "Watch out"; color: var(--warn) }
details.helpers { margin: 14px 0 4px; border: 1px solid var(--rule); border-radius: 8px; padding: 10px 16px; background: var(--panel) }
details.helpers summary { cursor: pointer; font-weight: 600; color: var(--accent) }
details.helpers p { font-size: 15px; color: var(--muted); margin: 8px 0 }
nav.prevnext { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; margin-top: 56px }
nav.prevnext a { display: grid; gap: 2px; padding: 14px 16px; border: 1px solid var(--rule); border-radius: 8px; text-decoration: none; color: var(--ink); font-weight: 600 }
nav.prevnext a:hover { border-color: var(--accent) }
nav.prevnext a.next { text-align: right }
nav.prevnext small { font-weight: 400; color: var(--muted); font-size: 13px }
section.group h2 { margin-top: 40px }
.blurb { color: var(--muted); margin: 0 0 14px }
.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px }
.card { display: grid; gap: 4px; align-content: start; padding: 14px 16px; border: 1px solid var(--rule); border-radius: 8px; background: var(--panel); text-decoration: none; color: var(--ink); min-width: 0 }
.card:hover { border-color: var(--accent) }
.card b { font: 700 16.5px var(--display) }
.card span { font-size: 14.5px; color: var(--muted) }
footer.foot { border-top: 1px solid var(--rule); color: var(--muted); font-size: 14.5px }
footer.foot .wrap { padding-block: 24px 40px }
footer.foot p { margin: 4px 0 }
@media (prefers-reduced-motion: no-preference) { html { scroll-behavior: smooth } }
'''

def main():
    outputs = run_samples()
    samples = load_samples()
    helpers = load_helpers()
    pages = load_pages()
    used_all = set()
    for k in samples:
        if k not in outputs:
            pass  # samples that print nothing are fine
    for i, p in enumerate(pages):
        doc, used = page_html(p, pages[i - 1] if i else None, pages[i + 1] if i + 1 < len(pages) else None, samples, outputs, helpers)
        used_all |= used
        open(os.path.join(OUT, p['name'] + '.html'), 'w').write(doc)
    unused = sorted(set(samples) - used_all)
    if unused:
        sys.exit('genexamples: samples not shown on any page: ' + ', '.join(unused))
    keep = {p['name'] + '.html' for p in pages} | {'index.html'}
    for f in glob.glob(os.path.join(OUT, '*.html')):
        if os.path.basename(f) not in keep:
            os.remove(f)
    open(os.path.join(OUT, 'index.html'), 'w').write(index_html(pages))
    open(os.path.join(OUT, 'style.css'), 'w').write(CSS)
    print(f'examples/: {len(pages)} pages, {len(samples)} samples')

main()
