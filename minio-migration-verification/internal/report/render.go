package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"
)

// WriteJSON writes the report as indented JSON to path.
func WriteJSON(r *Report, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText prints the human-readable summary (the format in the handoff doc).
func WriteText(r *Report, w io.Writer) {
	line := strings.Repeat("=", 28)
	fmt.Fprintf(w, "\nMinIO Migration Verification\n%s\n\n", line)
	fmt.Fprintf(w, "Source: %s (%s)\nTarget: %s (%s)\nGenerated: %s\n\n",
		r.Source.Label, r.Source.Endpoint, r.Target.Label, r.Target.Endpoint, r.GeneratedAt.Format("2006-01-02 15:04:05 MST"))

	for _, c := range r.Checks {
		fmt.Fprintf(w, "%-22s %-7s %s\n", c.Name, c.Status, checkDetail(c))
	}
	for _, b := range r.Buckets {
		fmt.Fprintf(w, "\nBucket: %s -> %s", b.SourceBucket, b.TargetBucket)
		if b.Prefix != "" {
			fmt.Fprintf(w, " (prefix %q)", b.Prefix)
		}
		fmt.Fprintf(w, "\n%s\n", strings.Repeat("-", 28))
		for _, c := range b.Checks {
			fmt.Fprintf(w, "%-22s %-7s %s\n", c.Name, c.Status, checkDetail(c))
		}
		s := b.Summary
		fmt.Fprintf(w, "\nSource Objects     %d (%s)\nTarget Objects     %d (%s)\nMatched Objects    %d\nMissing Objects    %d\nExtra Objects      %d\nMismatched Objects %d\n",
			s.SourceObjects, humanBytes(s.SourceBytes), s.TargetObjects, humanBytes(s.TargetBytes), s.MatchedObjects, s.MissingObjects, s.ExtraObjects, s.MismatchedObjects)
		if s.ErrorObjects > 0 {
			fmt.Fprintf(w, "Error Objects      %d\n", s.ErrorObjects)
		}
		if s.ETagInconclusive > 0 {
			fmt.Fprintf(w, "ETag Inconclusive  %d\n", s.ETagInconclusive)
		}
		if s.DeepVerified > 0 {
			fmt.Fprintf(w, "Deep Verified      %d (%s downloaded)\n", s.DeepVerified, humanBytes(s.DeepBytes))
		}
		if s.SourceVersions > 0 || s.TargetVersions > 0 {
			fmt.Fprintf(w, "Versions           %d / %d (mismatched keys: %d)\n", s.SourceVersions, s.TargetVersions, s.VersionMismatches)
		}
		if len(b.Objects) > 0 {
			fmt.Fprintf(w, "\nObject details:\n")
			for _, o := range b.Objects {
				if o.Status == Pass {
					fmt.Fprintf(w, "  [%s] %s  %s etag=%s %s  %s\n", o.Status, o.Key, humanBytes(o.Source.Size), o.Source.ETag, o.Source.ContentType, o.Detail)
					continue
				}
				fmt.Fprintf(w, "  [%s] %s  %s  %s\n", o.Status, o.Key, strings.Join(o.Reasons, ","), o.Detail)
			}
		}
		fmt.Fprintf(w, "\nBucket Status      %s\n", b.Status)
	}
	fmt.Fprintf(w, "\n%s\nOverall            %s   (%s)\n%s\n", line, r.Status, r.Elapsed, line)
	if len(r.Limitations) > 0 {
		fmt.Fprintf(w, "\nNotes / limitations:\n")
		for _, l := range r.Limitations {
			fmt.Fprintf(w, "  - %s\n", l)
		}
	}
}

// checkDetail renders detail plus observed values (source -> target) when present.
func checkDetail(c Check) string {
	if c.Source == "" && c.Target == "" {
		return c.Detail
	}
	vals := c.Source
	if c.Source != c.Target {
		vals = c.Source + " -> " + c.Target
	}
	if c.Detail == "" {
		return vals
	}
	return c.Detail + " [" + vals + "]"
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// WriteHTML writes a self-contained single-file HTML report.
func WriteHTML(r *Report, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	t := template.Must(template.New("report").Funcs(template.FuncMap{
		"bytes": humanBytes,
		"join":  strings.Join,
		"lower": func(v any) string { return strings.ToLower(fmt.Sprint(v)) },
		"json": func(v any) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
		},
	}).Parse(htmlTemplate))
	return t.Execute(f, r)
}

const htmlTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>MinIO Migration Verification — {{.Status}}</title>
<style>
:root{--pass:#1a7f37;--fail:#c62828;--warn:#b26a00;--na:#666;--bg:#fff;--fg:#1b1b1b;--muted:#666;--line:#e3e3e3;--code:#f4f4f4}
@media(prefers-color-scheme:dark){:root{--bg:#121417;--fg:#e6e6e6;--muted:#9a9a9a;--line:#2b2f36;--code:#1c2027;--pass:#3fb950;--fail:#f85149;--warn:#d29922;--na:#8b949e}}
body{font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;background:var(--bg);color:var(--fg);margin:0;padding:24px;max-width:1100px}
h1{font-size:22px;margin:0 0 4px}h2{font-size:17px;margin:28px 0 8px;border-bottom:1px solid var(--line);padding-bottom:4px}
.muted{color:var(--muted)}.badge{display:inline-block;padding:2px 10px;border-radius:12px;font-weight:600;color:#fff;font-size:12px;letter-spacing:.3px}
.s-pass{background:var(--pass)}.s-fail{background:var(--fail)}.s-error{background:var(--fail)}.s-warn{background:var(--warn)}.s-n\/a,.s-skipped{background:var(--na)}
.hero{display:flex;align-items:center;gap:16px;margin:12px 0 20px}.hero .big{font-size:28px;padding:6px 18px;border-radius:8px}
table{border-collapse:collapse;width:100%;margin:8px 0}th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);vertical-align:top}th{font-weight:600;color:var(--muted);font-size:12px;text-transform:uppercase}
code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;background:var(--code);border-radius:4px}code{padding:1px 4px}pre{padding:8px;overflow:auto;max-height:280px;margin:0}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:10px;margin:10px 0}.tile{border:1px solid var(--line);border-radius:8px;padding:10px}.tile .v{font-size:22px;font-weight:600}.tile .k{font-size:12px;color:var(--muted)}
details{margin:6px 0}summary{cursor:pointer}ul.lim{margin:4px 0 0 18px}
</style></head><body>
<h1>MinIO Migration Verification</h1>
<div class="muted">{{.Tool}} {{.Version}} · generated {{.GeneratedAt.Format "2006-01-02 15:04:05 MST"}} · elapsed {{.Elapsed}}</div>
<div class="hero"><span class="badge big s-{{lower .Status}}">{{.Status}}</span>
<div><div><b>Source:</b> {{.Source.Label}} <code>{{.Source.Endpoint}}</code></div><div><b>Target:</b> {{.Target.Label}} <code>{{.Target.Endpoint}}</code></div></div></div>

<div class="grid">
<div class="tile"><div class="v">{{.Totals.SourceObjects}}</div><div class="k">source objects ({{bytes .Totals.SourceBytes}})</div></div>
<div class="tile"><div class="v">{{.Totals.TargetObjects}}</div><div class="k">target objects ({{bytes .Totals.TargetBytes}})</div></div>
<div class="tile"><div class="v">{{.Totals.MatchedObjects}}</div><div class="k">matched</div></div>
<div class="tile"><div class="v">{{.Totals.MissingObjects}}</div><div class="k">missing on target</div></div>
<div class="tile"><div class="v">{{.Totals.ExtraObjects}}</div><div class="k">extra on target</div></div>
<div class="tile"><div class="v">{{.Totals.MismatchedObjects}}</div><div class="k">mismatched</div></div>
{{if .Totals.DeepVerified}}<div class="tile"><div class="v">{{.Totals.DeepVerified}}</div><div class="k">deep verified ({{bytes .Totals.DeepBytes}})</div></div>{{end}}
</div>

<h2>Global checks</h2>
<table><tr><th>Check</th><th>Status</th><th>Detail</th></tr>
{{range .Checks}}<tr><td>{{.Name}}</td><td><span class="badge s-{{lower .Status}}">{{.Status}}</span></td><td>{{.Detail}}</td></tr>{{end}}
</table>

{{range .Buckets}}
<h2>Bucket <code>{{.SourceBucket}}</code> → <code>{{.TargetBucket}}</code>{{if .Prefix}} <span class="muted">prefix <code>{{.Prefix}}</code></span>{{end}} <span class="badge s-{{lower .Status}}">{{.Status}}</span></h2>
<table><tr><th>Check</th><th>Status</th><th>Source</th><th>Target</th><th>Detail</th></tr>
{{range .Checks}}<tr><td>{{.Name}}</td><td><span class="badge s-{{lower .Status}}">{{.Status}}</span></td><td>{{.Source}}</td><td>{{.Target}}</td><td>{{.Detail}}</td></tr>{{end}}
</table>
<div class="grid">
<div class="tile"><div class="v">{{.Summary.SourceObjects}}</div><div class="k">source objects</div></div>
<div class="tile"><div class="v">{{.Summary.TargetObjects}}</div><div class="k">target objects</div></div>
<div class="tile"><div class="v">{{.Summary.MatchedObjects}}</div><div class="k">matched</div></div>
<div class="tile"><div class="v">{{.Summary.MissingObjects}}</div><div class="k">missing</div></div>
<div class="tile"><div class="v">{{.Summary.ExtraObjects}}</div><div class="k">extra</div></div>
<div class="tile"><div class="v">{{.Summary.MismatchedObjects}}</div><div class="k">mismatched</div></div>
{{if .Summary.ETagInconclusive}}<div class="tile"><div class="v">{{.Summary.ETagInconclusive}}</div><div class="k">etag inconclusive</div></div>{{end}}
{{if .Summary.ErrorObjects}}<div class="tile"><div class="v">{{.Summary.ErrorObjects}}</div><div class="k">errors</div></div>{{end}}
{{if or .Summary.SourceVersions .Summary.TargetVersions}}<div class="tile"><div class="v">{{.Summary.SourceVersions}} / {{.Summary.TargetVersions}}</div><div class="k">versions src / tgt</div></div>{{end}}
</div>
{{if .Objects}}
<h3>Object details ({{len .Objects}})</h3>
<table><tr><th>Status</th><th>Key</th><th>Reasons</th><th>Detail</th><th>Source</th><th>Target</th></tr>
{{range .Objects}}<tr><td><span class="badge s-{{lower .Status}}">{{.Status}}</span></td><td><code>{{.Key}}</code></td><td>{{join .Reasons ", "}}</td><td>{{.Detail}}</td>
<td>{{if .Source}}<details><summary>{{bytes .Source.Size}} · <code>{{.Source.ETag}}</code></summary><pre>{{json .Source}}</pre></details>{{else}}<span class="muted">absent</span>{{end}}</td>
<td>{{if .Target}}<details><summary>{{bytes .Target.Size}} · <code>{{.Target.ETag}}</code></summary><pre>{{json .Target}}</pre></details>{{else}}<span class="muted">absent</span>{{end}}</td></tr>{{end}}
</table>{{end}}
{{if .Versions}}
<h3>Version history differences ({{len .Versions}})</h3>
<table><tr><th>Key</th><th>Src versions</th><th>Tgt versions</th><th>Src delete markers</th><th>Tgt delete markers</th><th>Reasons</th></tr>
{{range .Versions}}<tr><td><code>{{.Key}}</code></td><td>{{.SourceVersion}}</td><td>{{.TargetVersion}}</td><td>{{.SourceDeleteM}}</td><td>{{.TargetDeleteM}}</td><td>{{join .Reasons ", "}}</td></tr>{{end}}
</table>{{end}}
{{end}}

{{if .Limitations}}<h2>Notes and limitations</h2><ul class="lim">{{range .Limitations}}<li>{{.}}</li>{{end}}</ul>{{end}}
<h2>Run options</h2><pre>{{json .Options}}</pre>
</body></html>`
