package main

import "html/template"

// Shared visual shell for the fixture pages. Near-grayscale, thin borders —
// consistent with the Waypoint product language; no build step, no assets.
const pageStyle = `
:root { color-scheme: light; }
body { font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
       background: oklch(0.984 0.002 260); color: oklch(0.245 0.006 260);
       margin: 0; padding: 24px 20px 64px; line-height: 1.55; }
main { max-width: 640px; margin: 0 auto; }
h1 { font-size: 20px; font-weight: 600; letter-spacing: -0.01em; margin: 0 0 4px; }
h2 { font-size: 13px; font-weight: 500; text-transform: uppercase; letter-spacing: 0.06em;
     color: oklch(0.505 0.009 260); margin: 24px 0 8px; }
p { margin: 6px 0; }
a { color: oklch(0.52 0.11 252); }
.muted { color: oklch(0.505 0.009 260); font-size: 13px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; word-break: break-all; }
.card { border: 1px solid oklch(0.906 0.004 260); background: oklch(0.998 0 0);
        border-radius: 10px; padding: 16px; margin-top: 12px; }
.fault { border: 1px solid oklch(0.80 0.09 75); background: oklch(0.97 0.03 85);
         border-radius: 10px; padding: 10px 14px; margin-top: 12px; font-size: 13px; }
.notice { border: 1px solid oklch(0.80 0.06 40); background: oklch(0.975 0.02 40);
          border-radius: 10px; padding: 10px 14px; margin-top: 12px; font-size: 13px; }
.ok { color: oklch(0.46 0.09 155); } .bad { color: oklch(0.50 0.13 25); }
.warn { color: oklch(0.52 0.10 75); } .mutedc { color: oklch(0.505 0.009 260); }
.status { font-weight: 600; letter-spacing: 0.01em; }
button, .btn { font: inherit; font-size: 14px; display: inline-block; padding: 8px 14px;
        border-radius: 10px; border: 1px solid oklch(0.30 0.006 260);
        background: oklch(0.28 0.006 260); color: oklch(0.985 0 0);
        text-decoration: none; cursor: pointer; }
.btn.secondary { background: oklch(0.998 0 0); color: oklch(0.245 0.006 260);
        border: 1px solid oklch(0.845 0.005 260); }
select, input[type=number] { font: inherit; font-size: 14px; padding: 7px 10px;
        border-radius: 10px; border: 1px solid oklch(0.845 0.005 260);
        background: oklch(0.998 0 0); }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th { text-align: left; font-weight: 500; color: oklch(0.505 0.009 260);
     font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; }
th, td { padding: 6px 8px 6px 0; border-bottom: 1px solid oklch(0.94 0.003 260);
         vertical-align: top; }
pre { background: oklch(0.984 0.002 260); border: 1px solid oklch(0.906 0.004 260);
      border-radius: 8px; padding: 10px; overflow-x: auto; font-size: 11.5px; }
.event { border-top: 1px solid oklch(0.94 0.003 260); padding: 8px 0; }
.row { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
`

var catalogTmpl = template.Must(template.New("catalog").Funcs(templateFuncs).Parse(pageStyle + `
<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Maple Supply Co. — checkout fixture</title></head><body><main>
<h1>Maple Supply Co.</h1>
<p class="muted">Waypoint V2 merchant checkout fixture · PayPal <strong>Sandbox</strong> only.
Amounts are computed server-side from this catalog; the browser never supplies a price.</p>

{{if eq .Fault "F1"}}
<div class="fault"><strong>Fault F1 active.</strong> The amount-formatting defect is enabled:
the provider payload will carry a float-formatted value (for example <span class="mono">12.340000</span>)
and the real PayPal Sandbox will reject the checkout. This is a deliberately injected fixture defect,
not a PayPal outage.</div>
{{end}}
{{if not .Configured}}
<div class="notice"><strong>PayPal credentials are not configured.</strong> Checkout will report
NOT_CONFIGURED and attempt nothing.</div>
{{end}}

<h2>Catalog</h2>
{{range .Products}}
<div class="card">
  <div class="row" style="justify-content: space-between;">
    <div>
      <div style="font-weight:600;">{{.Name}}</div>
      <div class="muted">{{.Description}}</div>
    </div>
    <div class="mono">${{usd .PriceMinor}}</div>
  </div>
  <form method="post" action="/checkout" class="row" style="margin-top:10px;">
    <input type="hidden" name="product_id" value="{{.ID}}">
    <label class="muted" for="qty-{{.ID}}">Quantity</label>
    <input id="qty-{{.ID}}" type="number" name="quantity" value="1" min="1" max="10" style="width:72px;">
    <button type="submit">Check out with PayPal Sandbox</button>
  </form>
</div>
{{end}}
<p class="muted" style="margin-top:20px;"><a href="/orders/">Recent orders</a> —
after creating one you will land on its status page, where buyer approval in PayPal Sandbox is required before any capture.</p>
</main></body></html>`))

var ordersTmpl = template.Must(template.New("orders").Funcs(templateFuncs).Parse(pageStyle + `
<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Maple Supply Co. — orders</title></head><body><main>
<h1>Orders</h1>
<p class="muted">Fixture checkout orders, newest first. Each order page carries the durable
operation journal and the provider evidence trail. <a href="/">Back to catalog</a></p>
{{if .Fault}}<div class="fault"><strong>Fault {{.Fault}} active</strong> — checkouts are fault-injected.</div>{{end}}
<h2>Recent orders</h2>
{{if not .Orders}}<div class="card muted">No orders yet. Create one from the catalog.</div>{{end}}
{{range .Orders}}
<div class="card">
  <div class="row" style="justify-content: space-between;">
    <a class="mono" href="/orders/{{.ID}}">{{.ID}}</a>
    <span class="status {{tone .Status}}">{{.Status}}</span>
  </div>
  <div class="muted" style="margin-top:4px;">{{.ProductID}} · qty {{.Quantity}} ·
    <span class="mono">${{usd .TotalMinor}} {{.Currency}}</span></div>
</div>
{{end}}
</main></body></html>`))

var orderTmpl = template.Must(template.New("order").Funcs(templateFuncs).Parse(pageStyle + `
<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Order {{.Order.ID}} — Maple Supply Co.</title></head><body><main>
<p><a href="/">← Back to catalog</a></p>
<h1>Order <span class="mono">{{.Order.ID}}</span></h1>
<p class="mono muted">trace {{.Order.TraceID}}</p>

{{if .Fault}}{{if ne .Fault "none"}}
<div class="fault"><strong>Fault {{.Fault}} active</strong> on this fixture instance.</div>
{{end}}{{end}}
{{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}

<div class="card">
  <div class="row" style="justify-content: space-between;">
    <div class="status {{tone .Order.Status}}">{{.Order.Status}}</div>
    <div class="mono">${{usd .Order.TotalMinor}} {{.Order.Currency}}</div>
  </div>
  <p class="muted">{{.Order.ProductID}} × {{.Order.Quantity}}</p>
  {{if .Order.ProviderOrderID}}
  <p class="mono">provider order {{.Order.ProviderOrderID}}</p>
  {{end}}
  {{if .Order.ApprovalURL}}
  <p><a class="btn" href="{{.Order.ApprovalURL}}" target="_blank" rel="noopener">Open PayPal approval page (Sandbox buyer)</a></p>
  <p class="muted">Approve there with your sandbox buyer account, then come back and refresh the provider status.
  Approval alone is not payment completion.</p>
  {{end}}
  <div class="row" style="margin-top:10px;">
    {{if .Order.ProviderOrderID}}
    <form method="post" action="/orders/{{.Order.ID}}/refresh"><button class="btn secondary" type="submit">Refresh provider status</button></form>
    {{end}}
    {{if .CanResolve}}
    <form method="post" action="/orders/{{.Order.ID}}/resolve-create"><button class="btn secondary" type="submit">Resolve create outcome (same request id)</button></form>
    {{end}}
    {{if or (eq .Order.Status "APPROVED") (eq .Order.Status "CAPTURE_OUTCOME_UNKNOWN")}}
    <form method="post" action="/orders/{{.Order.ID}}/capture"><button type="submit">Capture payment (server-side)</button></form>
    {{end}}
  </div>
</div>

{{if .Incident}}
<h2>Incident</h2>
<div class="card">
  <p class="muted">This order has a failed or unresolved provider operation. The redacted incident artifact
  (correlation, provider diagnostics, timeline) is available as JSON — exactly what an investigation starts from.</p>
  <p><a class="mono" href="/api/orders/{{.Order.ID}}/incident">/api/orders/{{.Order.ID}}/incident</a></p>
</div>
{{end}}

<h2>Durable operations</h2>
<table>
<tr><th>Type</th><th>Request id</th><th>Status</th><th>Attempts</th></tr>
{{range .Operations}}
<tr>
  <td>{{.Type}}</td>
  <td class="mono">{{.ProviderRequestID}}</td>
  <td class="{{tone .Status}}">{{.Status}}</td>
  <td>{{.AttemptCount}}</td>
</tr>
{{end}}
</table>

<h2>Event journal (append-only)</h2>
{{range .Events}}
<div class="event">
  <div class="row" style="justify-content: space-between;">
    <div><strong>{{.Type}}</strong> <span class="muted">· {{.Source}}</span></div>
    <div class="muted mono">{{.Time}} · tier {{.Tier}}</div>
  </div>
  <details><summary class="muted">details</summary><pre class="mono">{{.Detail}}</pre></details>
</div>
{{end}}
</main></body></html>`))

var errorTmpl = template.Must(template.New("error").Funcs(templateFuncs).Parse(pageStyle + `
<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Checkout fixture — error</title></head><body><main>
<h1>Something in this checkout did not work</h1>
<div class="card"><p>{{.Message}}</p></div>
<p><a href="/">← Back to catalog</a></p>
</main></body></html>`))
