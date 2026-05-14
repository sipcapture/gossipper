// Package reporthtml renders a self-contained HTML report from a stats.Summary value.
package reporthtml

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"time"

	"github.com/sipcapture/gossipper/internal/stats"
)

// WriteFile writes a standalone UTF-8 HTML document for the given summary.
func WriteFile(path string, s stats.Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := Write(f, s); err != nil {
		return err
	}
	return nil
}

// Write renders the HTML report to w.
func Write(w io.Writer, s stats.Summary) error {
	tmpl := template.Must(template.New("report").Funcs(template.FuncMap{
		"hasTime": func(t time.Time) bool { return !t.IsZero() },
		"barWidth": func(x float64) float64 {
			if x < 0 {
				return 0
			}
			if x > 1 {
				return 100
			}
			return x * 100
		},
		"dur": func(d time.Duration) string {
			if d <= 0 {
				return d.String()
			}
			if d < time.Microsecond {
				return d.String()
			}
			return d.Round(time.Microsecond).String()
		},
		"pct": func(x float64) string {
			return fmt.Sprintf("%.2f", x*100)
		},
		"flt": func(x float64) string {
			return fmt.Sprintf("%.6f", x)
		},
	}).Parse(reportTemplate))
	return tmpl.Execute(w, s)
}

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Gossipper run report</title>
<style>
:root { --ok:#0a7; --bad:#c33; --muted:#555; --bg:#f6f7f9; --card:#fff; --border:#ddd; }
body { font-family: system-ui, Segoe UI, Roboto, sans-serif; margin:0; background:var(--bg); color:#111; line-height:1.45; }
header { background:#1a1d24; color:#eee; padding:1.25rem 1.5rem; }
header h1 { margin:0; font-size:1.25rem; font-weight:600; }
header .meta { margin-top:.4rem; font-size:.85rem; color:#aab; }
.wrap { max-width:1100px; margin:0 auto; padding:1rem 1.25rem 2rem; }
.grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(160px,1fr)); gap:.75rem; margin:1rem 0; }
.card { background:var(--card); border:1px solid var(--border); border-radius:8px; padding:.85rem 1rem; }
.card .label { font-size:.75rem; color:var(--muted); text-transform:uppercase; letter-spacing:.03em; }
.card .val { font-size:1.35rem; font-weight:600; margin-top:.2rem; }
.bar { height:10px; border-radius:5px; background:var(--border); margin-top:.5rem; overflow:hidden; }
.bar > i { display:block; height:100%; background:var(--ok); border-radius:5px; }
section { margin-top:1.5rem; }
section h2 { font-size:1rem; margin:0 0 .6rem; color:#222; }
table { width:100%; border-collapse:collapse; background:var(--card); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
th, td { text-align:left; padding:.5rem .75rem; border-bottom:1px solid var(--border); font-size:.9rem; }
th { background:#eef0f3; font-weight:600; }
tr:last-child td { border-bottom:none; }
.health { padding:.75rem 1rem; border-radius:8px; margin:1rem 0; font-weight:500; }
.health.pass { background:#e6f7ef; border:1px solid #8c8; color:#064; }
.health.fail { background:#fdeaea; border:1px solid #eaa; color:#600; }
.health.off { background:#eee; border:1px solid var(--border); color:var(--muted); }
ul.findings { margin:0; padding-left:1.2rem; }
ul.findings li { margin:.35rem 0; }
.muted { color:var(--muted); font-size:.85rem; }
</style>
</head>
<body>
<header>
  <h1>Gossipper run report</h1>
  <div class="meta">
    {{if .ToolVersion}}{{.ToolVersion}} · {{end}}
    {{if .SchemaVersion}}schema {{.SchemaVersion}}{{end}}
    <br/>
    {{if hasTime .StartedAt}}Started {{.StartedAt.Format "2006-01-02 15:04:05"}}{{end}}
    {{if hasTime .FinishedAt}} · Finished {{.FinishedAt.Format "2006-01-02 15:04:05"}}{{end}}
    · Wall {{dur .Duration}}
  </div>
</header>
<div class="wrap">
{{if .Health}}
  {{if .Health.Active}}
    {{if .Health.Pass}}
    <div class="health pass">Health: PASS</div>
    {{else}}
    <div class="health fail">Health: FAIL{{if .Health.Reasons}} — {{range $i, $r := .Health.Reasons}}{{if $i}}; {{end}}{{$r}}{{end}}{{end}}</div>
    {{end}}
  {{end}}
{{else}}
  <div class="health off">Health checks were not enabled for this run.</div>
{{end}}

<div class="grid">
  <div class="card"><div class="label">Total calls</div><div class="val">{{.TotalCalls}}</div></div>
  <div class="card"><div class="label">Success</div><div class="val">{{.SuccessCalls}}</div></div>
  <div class="card"><div class="label">Failed</div><div class="val">{{.FailedCalls}}</div></div>
  <div class="card"><div class="label">Success ratio</div><div class="val">{{pct .SuccessRatio}}%</div>
    <div class="bar"><i style="width:{{barWidth .SuccessRatio}}%"></i></div>
  </div>
  <div class="card"><div class="label">CPS (avg)</div><div class="val">{{printf "%.2f" .CallsPerSecond}}</div></div>
  <div class="card"><div class="label">Timeouts</div><div class="val">{{.Timeouts}}</div></div>
  <div class="card"><div class="label">Retransmits</div><div class="val">{{.Retransmits}}</div></div>
  <div class="card"><div class="label">Avg call</div><div class="val">{{dur .AverageCallLatency}}</div></div>
  <div class="card"><div class="label">Avg invite RTT</div><div class="val">{{dur .AverageInviteRTT}}</div></div>
</div>

{{if .Findings}}
<section>
  <h2>Findings</h2>
  <ul class="findings">{{range .Findings}}<li>{{.}}</li>{{end}}</ul>
</section>
{{end}}

<section>
  <h2>Media</h2>
  <table>
    <tr><th>RTP sent pkts</th><td>{{.Media.RTPPacketsSent}}</td></tr>
    <tr><th>RTP recv pkts</th><td>{{.Media.RTPPacketsReceived}}</td></tr>
    <tr><th>RTCP RR blocks</th><td>{{.Media.RTCPReceptionReports}}</td></tr>
    <tr><th>Max fraction lost</th><td>{{if .Media.RTCPMaxFractionLost}}{{flt .Media.RTCPMaxFractionLost}}{{else}}—{{end}}</td></tr>
    <tr><th>Max jitter (ts)</th><td>{{.Media.RTCPMaxJitterTS}}</td></tr>
    <tr><th>Min jitter (ts)</th><td>{{if .Media.RTCPMinJitterTS}}{{.Media.RTCPMinJitterTS}}{{else}}—{{end}}</td></tr>
    <tr><th>Avg jitter (ts)</th><td>{{if .Media.RTCPAvgJitterTS}}{{printf "%.2f" .Media.RTCPAvgJitterTS}}{{else}}—{{end}}</td></tr>
    <tr><th>Calls w/ RTP recv</th><td>{{.Media.CallsWithRTPReceived}}</td></tr>
    <tr><th>Per-call min RTP recv</th><td>{{if .Media.PerCallMinRTPPacketsReceived}}{{.Media.PerCallMinRTPPacketsReceived}}{{else}}—{{end}}</td></tr>
  </table>
  <p class="muted">Jitter values are in RTP timestamp units (RFC 3550), not milliseconds.</p>
</section>

{{if .FailureClasses}}
<section>
  <h2>Failure classes</h2>
  <table><tr><th>Class</th><th>Count</th></tr>{{range $k, $v := .FailureClasses}}<tr><td>{{$k}}</td><td>{{$v}}</td></tr>{{end}}</table>
</section>
{{end}}

{{if .RTD}}
<section>
  <h2>RTD timers</h2>
  <table>
    <tr><th>Name</th><th>Count</th><th>Avg</th><th>Min</th><th>Max</th><th>Stddev</th></tr>
    {{range $name, $ls := .RTD}}
    <tr>
      <td>{{$name}}</td>
      <td>{{$ls.Count}}</td>
      <td>{{dur $ls.Average}}</td>
      <td>{{dur $ls.Min}}</td>
      <td>{{dur $ls.Max}}</td>
      <td>{{dur $ls.StdDev}}</td>
    </tr>
    {{end}}
  </table>
</section>
{{end}}

{{if .CallLength}}
<section>
  <h2>Call length distribution</h2>
  <table>
    <tr><th>Count</th><td>{{.CallLength.Count}}</td></tr>
    <tr><th>Average</th><td>{{dur .CallLength.Average}}</td></tr>
    <tr><th>Min / Max</th><td>{{dur .CallLength.Min}} / {{dur .CallLength.Max}}</td></tr>
    <tr><th>Stddev</th><td>{{dur .CallLength.StdDev}}</td></tr>
  </table>
  {{if .CallLength.Buckets}}
  <table style="margin-top:.5rem"><tr><th>Bucket</th><th>Count</th></tr>{{range .CallLength.Buckets}}<tr><td>{{.Label}}</td><td>{{.Count}}</td></tr>{{end}}</table>
  {{end}}
</section>
{{end}}

{{if .InviteRTT}}
<section>
  <h2>Invite RTT distribution</h2>
  <table>
    <tr><th>Count</th><td>{{.InviteRTT.Count}}</td></tr>
    <tr><th>Average</th><td>{{dur .InviteRTT.Average}}</td></tr>
    <tr><th>Min / Max</th><td>{{dur .InviteRTT.Min}} / {{dur .InviteRTT.Max}}</td></tr>
  </table>
</section>
{{end}}

{{if .Counters}}
<section>
  <h2>Counters</h2>
  <table><tr><th>Name</th><th>Count</th></tr>{{range $k, $v := .Counters}}<tr><td>{{$k}}</td><td>{{$v}}</td></tr>{{end}}</table>
</section>
{{end}}

<p class="muted">Generated by gossipper report-html / -summary_html — no external assets; safe to archive offline.</p>
</div>
</body>
</html>
`
