package commands

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// Page models. Everything the templates read is assembled first, so the
// templates stay free of Store calls and the whole page can be rendered once and
// cached as bytes.

type indexPage struct {
	All         bool
	Issues      []indexRow
	OpenCount   int
	ClosedCount int
}

type indexRow struct {
	ID      string
	Title   string
	Status  string
	Labels  []string
	Updated time.Time
}

type issuePage struct {
	ID          string
	Title       string
	Status      string
	Ref         string
	Created     time.Time
	Updated     time.Time
	Labels      []string
	Description template.HTML
	Commits     []string
	Branches    []string
	Relations   []linkSection
	Comments    []commentRow
	Attachments []attachmentRow
}

// linkSection is one of meta.tony's issue-to-issue relations, named as `git
// issue show` names it.
type linkSection struct {
	Title string
	Links []issueLink
}

type issueLink struct {
	ID     string
	Href   string
	Title  string
	Status string
	Found  bool
}

type commentRow struct {
	Path  string
	When  time.Time
	HasTS bool
	Body  template.HTML
}

type attachmentRow struct {
	Name string
	Href string
}

type errorPageData struct {
	Status  int
	Message string
}

// markdown renders issue text. Descriptions and comments are written by whoever
// filed or discussed the issue, and they are served from the same origin as
// every other page here, so raw HTML must not reach the browser. goldmark's
// renderer is therefore left at its default: with Unsafe off it replaces raw
// HTML with a placeholder comment and refuses javascript:/vbscript:/data: link
// targets. Nothing below ever turns that off.
//
// This is why the dependency is here at all rather than a hand-rolled subset:
// the escaping rules are exactly the part that is easy to get subtly wrong, and
// goldmark was already in this module's build graph as an indirect dependency
// of golang.org/x/tools, so taking it directly adds no new module.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		// Fall back to the literal text rather than dropping content; the
		// escaping here is the same guarantee goldmark would have given.
		return template.HTML("<pre>" + template.HTMLEscapeString(src) + "</pre>")
	}
	return template.HTML(buf.String())
}

// stripTitleLine drops description.md's leading "# Title", which the page
// already shows as its heading.
func stripTitleLine(desc string) string {
	line, rest, found := strings.Cut(desc, "\n")
	if !found || !strings.HasPrefix(line, "# ") {
		return desc
	}
	return strings.TrimLeft(rest, "\n")
}

// stripCommentHeader drops a comment's leading "<!-- <ts> -->" header. The page
// renders that timestamp itself, and with raw HTML disabled the header would
// otherwise show up as goldmark's omitted-HTML placeholder.
func stripCommentHeader(content string) string {
	loc := commentHeaderRe.FindStringIndex(content)
	if loc == nil || strings.TrimSpace(content[:loc[0]]) != "" {
		return content
	}
	return strings.TrimLeft(content[loc[1]:], "\n")
}

// escapePathSegments percent-encodes each segment of a tree-relative path while
// leaving the separators alone, so an attachment named with a space or a '#'
// still produces a working href.
func escapePathSegments(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

// contentDisposition builds the download header for an attachment. The name
// comes out of a git tree and is not trusted to stay on its own header line:
// control characters are dropped, and anything non-ASCII is carried in the
// RFC 5987 filename* form with a plain ASCII fallback for clients that ignore
// it.
func contentDisposition(name string) string {
	var ascii strings.Builder
	nonASCII := false
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			// dropped
		case r > 0x7f:
			nonASCII = true
			ascii.WriteByte('_')
		case r == '"' || r == '\\':
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(r)
		}
	}
	fallback := ascii.String()
	if fallback == "" {
		fallback = "attachment"
	}
	if !nonASCII {
		return fmt.Sprintf("attachment; filename=%q", fallback)
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallback, url.PathEscape(name))
}

var templateFuncs = template.FuncMap{
	"fmtTime": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Local().Format("2006-01-02 15:04 MST")
	},
}

func renderPage(name string, data any) ([]byte, error) {
	tmpl, ok := pageTemplates[name]
	if !ok {
		return nil, fmt.Errorf("unknown page template %q", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to render %s page: %w", name, err)
	}
	return buf.Bytes(), nil
}

var pageTemplates = map[string]*template.Template{
	"index": mustPage(indexBody),
	"issue": mustPage(issueBody),
	"error": mustPage(errorBody),
}

// mustPage builds one page as the shared shell plus that page's "pagetitle" and
// "content" blocks.
func mustPage(body string) *template.Template {
	return template.Must(template.New("shell").Funcs(templateFuncs).Parse(shellHTML + body))
}

// Styles are inline because this serves from a git tree, not a directory of
// static files, and one document per response keeps it that way.
const shellHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{template "pagetitle" .}}</title>
<style>
:root { color-scheme: light dark; --fg:#1a1a1a; --dim:#666; --bg:#fff; --line:#e0e0e0;
        --accent:#0b5fa5; --open:#1a7f37; --closed:#8250df; --chip:#f0f0f0; --code:#f6f6f6; }
@media (prefers-color-scheme: dark) {
  :root { --fg:#e6e6e6; --dim:#9a9a9a; --bg:#16181c; --line:#2e3238;
          --accent:#6cb0f5; --open:#4ac26b; --closed:#c297ff; --chip:#24272c; --code:#1d2025; }
}
* { box-sizing: border-box; }
body { margin:0; padding:2rem 1rem 4rem; background:var(--bg); color:var(--fg);
       font:16px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; }
main { max-width: 52rem; margin: 0 auto; }
a { color: var(--accent); }
h1 { font-size:1.6rem; line-height:1.3; margin:0 0 .4rem; }
h2 { font-size:1.05rem; text-transform:uppercase; letter-spacing:.06em; color:var(--dim);
     margin:2rem 0 .6rem; font-weight:600; }
header.top { display:flex; justify-content:space-between; align-items:baseline; gap:1rem;
             border-bottom:1px solid var(--line); padding-bottom:.75rem; margin-bottom:1.5rem; }
header.top .brand { font-weight:600; }
header.top .brand a { color:inherit; text-decoration:none; }
.meta { color:var(--dim); font-size:.875rem; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.status { font-size:.75rem; font-weight:600; text-transform:uppercase; letter-spacing:.05em;
          border-radius:999px; padding:.1rem .55rem; border:1px solid currentColor; }
.status.open { color:var(--open); }
.status.closed { color:var(--closed); }
.chip { display:inline-block; background:var(--chip); border-radius:999px;
        padding:.05rem .5rem; font-size:.78rem; margin-right:.3rem; }
ul.issues { list-style:none; margin:0; padding:0; }
ul.issues li { border-bottom:1px solid var(--line); padding:.7rem 0;
               display:flex; gap:.75rem; align-items:baseline; flex-wrap:wrap; }
ul.issues li .title { flex:1 1 20rem; }
ul.plain { list-style:none; margin:0; padding:0; }
ul.plain li { padding:.15rem 0; }
.prose { overflow-wrap:break-word; }
.prose :first-child { margin-top:0; }
.prose pre { background:var(--code); padding:.75rem 1rem; border-radius:6px; overflow-x:auto; }
.prose code { background:var(--code); padding:.1rem .3rem; border-radius:4px; font-size:.9em; }
.prose pre code { background:none; padding:0; }
.prose blockquote { margin:0; padding:.1rem 1rem; border-left:3px solid var(--line); color:var(--dim); }
.prose table { border-collapse:collapse; display:block; overflow-x:auto; }
.prose th, .prose td { border:1px solid var(--line); padding:.3rem .6rem; }
.prose img { max-width:100%; }
.comment { border:1px solid var(--line); border-radius:6px; margin:0 0 1rem; }
.comment .when { border-bottom:1px solid var(--line); padding:.4rem .9rem;
                 font-size:.82rem; color:var(--dim); }
.comment .prose { padding:.2rem .9rem .8rem; }
footer { margin-top:3rem; border-top:1px solid var(--line); padding-top:.75rem;
         color:var(--dim); font-size:.82rem; }
</style>
</head>
<body>
<main>
<header class="top">
  <span class="brand"><a href="/">git issue</a></span>
  <span class="meta">read-only</span>
</header>
{{template "content" .}}
<footer>Served by <span class="mono">git issue serve</span>. This view is read-only &mdash;
issues are edited with the <span class="mono">git issue</span> CLI.</footer>
</main>
</body>
</html>
`

const indexBody = `
{{define "pagetitle"}}git issue{{end}}
{{define "content"}}
<h1>Issues</h1>
<p class="meta">
  {{.OpenCount}} open{{if .All}}, {{.ClosedCount}} closed{{end}} &middot;
  {{if .All}}<a href="/">open only</a>{{else}}<a href="/?all=1">include closed</a>{{end}}
</p>
{{if .Issues}}
<ul class="issues">
{{range .Issues}}
  <li>
    <span class="status {{.Status}}">{{.Status}}</span>
    <span class="title"><a href="/i/{{.ID}}">{{if .Title}}{{.Title}}{{else}}(untitled){{end}}</a>
      {{range .Labels}}<span class="chip">{{.}}</span>{{end}}
    </span>
    <span class="meta mono">{{.ID}}</span>
  </li>
{{end}}
</ul>
{{else}}
<p class="meta">No issues found.</p>
{{end}}
{{end}}
`

const issueBody = `
{{define "pagetitle"}}{{if .Title}}{{.Title}}{{else}}{{.ID}}{{end}} &middot; git issue{{end}}
{{define "content"}}
<h1>{{if .Title}}{{.Title}}{{else}}(untitled){{end}}</h1>
<p class="meta">
  <span class="status {{.Status}}">{{.Status}}</span>
  <span class="mono">{{.ID}}</span>
  {{range .Labels}}<span class="chip">{{.}}</span>{{end}}
</p>
<p class="meta">
  opened {{fmtTime .Created}}{{with fmtTime .Updated}} &middot; updated {{.}}{{end}}
  &middot; <span class="mono">{{.Ref}}</span>
</p>

<div class="prose">{{.Description}}</div>

{{range .Relations}}{{if .Links}}
<h2>{{.Title}}</h2>
<ul class="plain">
{{range .Links}}
  <li>{{if .Found}}<span class="status {{.Status}}">{{.Status}}</span> {{end}}
      <a href="{{.Href}}" class="mono">{{.ID}}</a>
      {{if .Found}}{{.Title}}{{else}}<span class="meta">(not found)</span>{{end}}</li>
{{end}}
</ul>
{{end}}{{end}}

{{if .Commits}}
<h2>Linked commits</h2>
<ul class="plain">{{range .Commits}}<li class="mono">{{.}}</li>{{end}}</ul>
{{end}}

{{if .Branches}}
<h2>Linked branches</h2>
<ul class="plain">{{range .Branches}}<li class="mono">{{.}}</li>{{end}}</ul>
{{end}}

{{if .Comments}}
<h2>Discussion</h2>
{{range .Comments}}
<article class="comment">
  <div class="when">{{if .HasTS}}{{fmtTime .When}}{{else}}<span class="mono">{{.Path}}</span>{{end}}</div>
  <div class="prose">{{.Body}}</div>
</article>
{{end}}
{{end}}

{{if .Attachments}}
<h2>Attachments</h2>
<ul class="plain">
{{range .Attachments}}<li><a href="{{.Href}}" class="mono">{{.Name}}</a></li>{{end}}
</ul>
{{end}}
{{end}}
`

const errorBody = `
{{define "pagetitle"}}{{.Status}} &middot; git issue{{end}}
{{define "content"}}
<h1>{{.Status}}</h1>
<p class="meta">{{.Message}}</p>
<p><a href="/">Back to the issue list</a></p>
{{end}}
`
