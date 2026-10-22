// Command site builds the tutorial website in docs/ from the chapters in
// docs/site/chapters and the example programs in docs/examples, which are
// the source of truth for every line of code the site shows.
//
//	go run ./docs/site            # rebuild docs/*.html
//	go run ./docs/site -record    # also rerun every example against $BLNK_URL
//	                              # and save its output for the Output tabs
//
// A chapter is Markdown with three directives, each on a line of its own:
//
//	{{code 05-transfers/main.go transfer}}  the region of a file between
//	                                        "// snippet:transfer" and "// end-snippet"
//	{{file 07-airtime/airtime/airtime.go}}  a whole file
//	{{run 05-transfers}}                    the example's main.go and its
//	                                        recorded output, in tabs
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// repo is where the examples link to their source.
const repo = "https://github.com/otyang/humanblnk/blob/main/docs/examples/"

// chapter is one page of the site, in order.
type chapter struct {
	File, Slug, Title, Section string
}

var chapters = []chapter{
	{"00-index.md", "index", "Introduction", ""},
	{"01-setup.md", "setup", "Setting up", "Foundations"},
	{"02-customers-and-accounts.md", "customers-and-accounts", "Customers and accounts", "Foundations"},
	{"03-money.md", "money", "Money", "Foundations"},
	{"04-deposits.md", "deposits", "Deposits", "Moving money"},
	{"05-transfers-and-fees.md", "transfers-and-fees", "Transfers and fees", "Moving money"},
	{"06-payouts-and-holds.md", "payouts-and-holds", "Payouts and holds", "Moving money"},
	{"07-building-a-service-airtime.md", "building-a-service", "Building a service: airtime", "Moving money"},
	{"08-refunds-and-reversals.md", "refunds-and-reversals", "Refunds and reversals", "Moving money"},
	{"09-currency-exchange.md", "currency-exchange", "Currency exchange", "Moving money"},
	{"10-watches-and-webhooks.md", "watches-and-webhooks", "Watches and webhooks", "Running it"},
	{"11-reconciliation.md", "reconciliation", "Reconciliation", "Running it"},
	{"12-errors-and-retries.md", "errors-and-retries", "Errors and retries", "Running it"},
	{"13-testing.md", "testing", "Testing your service", "Running it"},
	{"14-cheat-sheet.md", "cheat-sheet", "Cheat sheet", "Running it"},
}

// navGroup is one section of the sidebar.
type navGroup struct {
	Name  string
	Items []navItem
}

type navItem struct {
	Slug, Title string
	Number      int
	Current     bool
}

// nav is the sidebar, with current marked.
func nav(current string) []navGroup {
	var groups []navGroup
	for i, c := range chapters {
		if len(groups) == 0 || groups[len(groups)-1].Name != c.Section {
			groups = append(groups, navGroup{Name: c.Section})
		}
		g := &groups[len(groups)-1]
		g.Items = append(g.Items, navItem{Slug: c.Slug, Title: c.Title, Number: i, Current: c.Slug == current})
	}
	return groups
}

func main() {
	root := flag.String("root", "docs", "the docs folder")
	record := flag.Bool("record", false, "rerun every example and save its output (needs a Blnk at $BLNK_URL)")
	flag.Parse()
	if *record {
		if err := recordOutputs(*root); err != nil {
			log.Fatal(err)
		}
	}
	if err := build(*root, *root); err != nil {
		log.Fatal(err)
	}
}

// build renders every chapter from root into out.
func build(root, out string) error {
	layout, err := template.ParseFiles(filepath.Join(root, "site", "layout.html"))
	if err != nil {
		return err
	}
	for i, c := range chapters {
		src, err := os.ReadFile(filepath.Join(root, "site", "chapters", c.File))
		if err != nil {
			return err
		}
		body, err := render(root, string(src))
		if err != nil {
			return fmt.Errorf("%s: %w", c.File, err)
		}
		page := struct {
			Chapter    chapter
			Number     int
			Body       template.HTML
			Nav        []navGroup
			Prev, Next *chapter
		}{Chapter: c, Number: i, Body: template.HTML(body), Nav: nav(c.Slug)}
		if i > 0 {
			page.Prev = &chapters[i-1]
		}
		if i+1 < len(chapters) {
			page.Next = &chapters[i+1]
		}
		var buf bytes.Buffer
		if err := layout.Execute(&buf, page); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, c.Slug+".html"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	for _, a := range []string{"style.css", "app.js"} {
		b, err := os.ReadFile(filepath.Join(root, "site", "assets", a))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(out, "assets"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, "assets", a), b, 0o644); err != nil {
			return err
		}
	}
	// GitHub Pages: serve the files as they are.
	return os.WriteFile(filepath.Join(out, ".nojekyll"), nil, 0o644)
}

// recordOutputs runs each example and saves what it printed.
func recordOutputs(root string) error {
	dirs, err := filepath.Glob(filepath.Join(root, "examples", "[0-9]*"))
	if err != nil {
		return err
	}
	for _, d := range dirs {
		cmd := exec.Command("go", "run", "./"+filepath.ToSlash(d))
		cmd.Stdin = nil
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w\n%s", d, err, out.String())
		}
		if err := os.WriteFile(filepath.Join(d, "output.txt"), out.Bytes(), 0o644); err != nil {
			return err
		}
		log.Println("recorded", d)
	}
	return nil
}

var directive = regexp.MustCompile(`^\{\{(code|file|run) ([^ }]+)(?: ([^ }]+))?\}\}$`)

// render turns a chapter's Markdown into HTML.
func render(root, src string) (string, error) {
	var out strings.Builder
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			i++
		case directive.MatchString(trimmed):
			m := directive.FindStringSubmatch(trimmed)
			h, err := codeDirective(root, m[1], m[2], m[3])
			if err != nil {
				return "", err
			}
			out.WriteString(h)
			i++
		case strings.HasPrefix(trimmed, "```"):
			lang := strings.TrimPrefix(trimmed, "```")
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) != "```" {
				j++
			}
			out.WriteString(codeBlock("", "", lang, strings.Join(lines[i+1:j], "\n")))
			i = j + 1
		case strings.HasPrefix(trimmed, "#"):
			level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			text := strings.TrimSpace(trimmed[level:])
			id := slug(text)
			fmt.Fprintf(&out, "<h%d id=\"%s\"><a class=\"anchor\" href=\"#%s\">%s</a></h%d>\n", level, id, id, inline(text), level)
			i++
		case strings.HasPrefix(trimmed, "|"):
			j := i
			for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
				j++
			}
			out.WriteString(table(lines[i:j]))
			i = j
		case strings.HasPrefix(trimmed, "> "):
			j := i
			var parts []string
			for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), ">") {
				parts = append(parts, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[j]), ">")))
				j++
			}
			fmt.Fprintf(&out, "<aside class=\"note\"><p>%s</p></aside>\n", inline(strings.Join(parts, " ")))
			i = j
		case listItem(line) != "":
			kind := listItem(line)
			tag := map[string]string{"ul": "ul", "ol": "ol"}[kind]
			fmt.Fprintf(&out, "<%s>\n", tag)
			for i < len(lines) && listItem(lines[i]) == kind {
				text := strings.TrimSpace(listMarker.ReplaceAllString(lines[i], ""))
				i++
				for i < len(lines) && strings.HasPrefix(lines[i], "  ") && strings.TrimSpace(lines[i]) != "" && listItem(lines[i]) == "" {
					text += " " + strings.TrimSpace(lines[i])
					i++
				}
				fmt.Fprintf(&out, "<li>%s</li>\n", inline(text))
			}
			fmt.Fprintf(&out, "</%s>\n", tag)
		default:
			j := i
			var parts []string
			for j < len(lines) && strings.TrimSpace(lines[j]) != "" && !startsBlock(lines[j]) {
				parts = append(parts, strings.TrimSpace(lines[j]))
				j++
			}
			fmt.Fprintf(&out, "<p>%s</p>\n", inline(strings.Join(parts, " ")))
			i = j
		}
	}
	return out.String(), nil
}

var listMarker = regexp.MustCompile(`^\s*(?:[-*]|\d+\.)\s+`)

// listItem is "ul" or "ol" if line starts a list item, else "".
func listItem(line string) string {
	t := strings.TrimSpace(line)
	if line != strings.TrimLeft(line, " ") {
		return "" // continuation, not a new item
	}
	switch {
	case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
		return "ul"
	case regexp.MustCompile(`^\d+\. `).MatchString(t):
		return "ol"
	}
	return ""
}

func startsBlock(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "|") ||
		strings.HasPrefix(t, "> ") || listItem(line) != "" || directive.MatchString(t)
}

func table(rows []string) string {
	var out strings.Builder
	out.WriteString("<div class=\"table\"><table>\n")
	cells := func(r string) []string {
		r = strings.TrimSpace(r)
		r = strings.TrimSuffix(strings.TrimPrefix(r, "|"), "|")
		// a "|" inside `code` isn't a cell boundary
		var cs []string
		var cur strings.Builder
		inCode := false
		for _, ch := range r {
			switch {
			case ch == '`':
				inCode = !inCode
				cur.WriteRune(ch)
			case ch == '|' && !inCode:
				cs = append(cs, strings.TrimSpace(cur.String()))
				cur.Reset()
			default:
				cur.WriteRune(ch)
			}
		}
		return append(cs, strings.TrimSpace(cur.String()))
	}
	for i, r := range rows {
		if i == 1 {
			continue // the --- separator
		}
		tag := "td"
		if i == 0 {
			tag = "th"
			out.WriteString("<thead>")
		}
		out.WriteString("<tr>")
		for _, c := range cells(r) {
			fmt.Fprintf(&out, "<%s>%s</%s>", tag, inline(c), tag)
		}
		out.WriteString("</tr>")
		if i == 0 {
			out.WriteString("</thead><tbody>")
		}
		out.WriteString("\n")
	}
	out.WriteString("</tbody></table></div>\n")
	return out.String()
}

var (
	codeSpan = regexp.MustCompile("`([^`]+)`")
	bold     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	link     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

// inline renders `code`, **bold** and [links](…) in escaped text.
func inline(s string) string {
	// Pull code spans out first so nothing inside them is touched.
	var spans []string
	s = codeSpan.ReplaceAllStringFunc(s, func(m string) string {
		spans = append(spans, "<code>"+html.EscapeString(m[1:len(m)-1])+"</code>")
		return fmt.Sprintf("\x00%d\x00", len(spans)-1)
	})
	s = html.EscapeString(s)
	s = bold.ReplaceAllString(s, "<strong>$1</strong>")
	s = link.ReplaceAllStringFunc(s, func(m string) string {
		p := link.FindStringSubmatch(m)
		href := html.UnescapeString(p[2])
		for _, c := range chapters {
			href = strings.Replace(href, c.File, c.Slug+".html", 1)
		}
		ext := ""
		if strings.HasPrefix(href, "http") {
			ext = ` target="_blank" rel="noopener"`
		}
		return fmt.Sprintf(`<a href="%s"%s>%s</a>`, html.EscapeString(href), ext, p[1])
	})
	for i, sp := range spans {
		s = strings.Replace(s, fmt.Sprintf("\x00%d\x00", i), sp, 1)
	}
	return s
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = codeSpan.ReplaceAllString(s, "$1")
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// codeDirective renders {{code}}, {{file}} and {{run}}.
func codeDirective(root, kind, path, name string) (string, error) {
	switch kind {
	case "code":
		src, err := os.ReadFile(filepath.Join(root, "examples", path))
		if err != nil {
			return "", err
		}
		snip, err := snippet(string(src), name)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		return codeBlock(path, repo+path, "go", snip), nil
	case "file":
		src, err := os.ReadFile(filepath.Join(root, "examples", path))
		if err != nil {
			return "", err
		}
		return codeBlock(path, repo+path, "go", clean(string(src))), nil
	case "run":
		src, err := os.ReadFile(filepath.Join(root, "examples", path, "main.go"))
		if err != nil {
			return "", err
		}
		output, err := os.ReadFile(filepath.Join(root, "examples", path, "output.txt"))
		if err != nil {
			return "", fmt.Errorf("%s has no recorded output: run go run ./docs/site -record", path)
		}
		file := path + "/main.go"
		return fmt.Sprintf(`<div class="example">
<div class="tabs" role="tablist"><button role="tab" class="tab active" data-tab="code">Code</button><button role="tab" class="tab" data-tab="output">Output</button><span class="cmd"><code>go run ./docs/examples/%s</code></span></div>
<div class="panel" data-panel="code">%s</div>
<div class="panel" data-panel="output" hidden>%s</div>
</div>
`, html.EscapeString(path), codeBlock(file, repo+file, "go", clean(string(src))), codeBlock("output", "", "text", strings.TrimRight(string(output), "\n"))), nil
	}
	return "", fmt.Errorf("unknown directive %q", kind)
}

// snippet is the region of src marked "// snippet:name" … "// end-snippet",
// dedented.
func snippet(src, name string) (string, error) {
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "// snippet:"+name {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no snippet %q", name)
	}
	var body []string
	for _, l := range lines[start:] {
		if strings.TrimSpace(l) == "// end-snippet" {
			return dedent(strings.TrimRight(strings.Join(dropMarkers(body), "\n"), "\n")), nil
		}
		body = append(body, l)
	}
	return "", fmt.Errorf("snippet %q has no end", name)
}

// clean is a whole file without its snippet markers.
func clean(src string) string {
	return strings.TrimRight(strings.Join(dropMarkers(strings.Split(src, "\n")), "\n"), "\n")
}

func dropMarkers(lines []string) []string {
	var out []string
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "// snippet:") || t == "// end-snippet" {
			// gofmt puts a blank line between a func and a marker after it
			if t == "// end-snippet" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "" {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, l)
	}
	return out
}

func dedent(s string) string {
	lines := strings.Split(s, "\n")
	min := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, "\t"))
		if min < 0 || n < min {
			min = n
		}
	}
	for i, l := range lines {
		if len(l) >= min {
			lines[i] = l[min:]
		}
	}
	return strings.Join(lines, "\n")
}

// codeBlock is a highlighted, copyable block. A title links to its source.
func codeBlock(title, href, lang, code string) string {
	code = strings.ReplaceAll(code, "\t", "    ")
	head := ""
	if title != "" {
		name := html.EscapeString(title)
		if href != "" {
			name = fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener">%s</a>`, html.EscapeString(href), name)
		}
		head = fmt.Sprintf(`<span class="file">%s</span>`, name)
	}
	if lang == "" {
		lang = "text"
	}
	return fmt.Sprintf(`<figure class="code"><figcaption>%s<button class="copy" type="button" aria-label="Copy code">Copy</button></figcaption><pre><code class="language-%s">%s</code></pre></figure>
`, head, lang, html.EscapeString(code))
}
