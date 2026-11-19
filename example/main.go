// Command example serves a sign-up form protected by a proof-of-work check.
package main

import (
	"html/template"
	"net/http"
	"os"
	"strconv"

	"github.com/flamego/flamego"

	"github.com/flamego/altcha"
)

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>altcha example</title>
<style>
body { font: 15px/1.5 system-ui, sans-serif; max-width: 420px; margin: 48px auto; color: #1f2328; }
label { display: block; margin: 12px 0 4px; }
input[type=text] { width: 100%; padding: 8px; box-sizing: border-box; border: 1px solid #d0d7de; border-radius: 6px; }
button { margin-top: 16px; padding: 8px 16px; border: 0; border-radius: 6px; background: #1f883d; color: #fff; font-size: 15px; }
.result { padding: 12px; border-radius: 6px; margin-bottom: 16px; }
.ok { background: #dafbe1; } .bad { background: #ffebe9; }
</style></head><body>
<h2>Sign up</h2>
{{if .Result}}<div class="result {{.Class}}">{{.Result}}</div>{{end}}
<form method="post">
  <label for="name">Name</label>
  <input type="text" id="name" name="name" value="Ada">
  <p></p>
  {{.Widget}}
  <button type="submit">Sign up</button>
</form>
</body></html>`))

// styles shows three ways a site can style the widget.
var styles = template.Must(template.New("styles").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>altcha styling</title>
<style>
body { font: 15px/1.5 system-ui, sans-serif; max-width: 460px; margin: 32px auto; color: #1f2328; }
h3 { margin: 24px 0 8px; font-size: 14px; color: #656d76; }

/* 2. Default markup, recolored with the CSS custom properties. */
.dark { padding: 16px; border-radius: 12px; background: #0d1117; }
.dark .flamego-altcha {
  --altcha-color: #e6edf3; --altcha-muted: #8b949e; --altcha-background: #161b22;
  --altcha-border: #30363d; --altcha-check-background: #0d1117; --altcha-check-border: #6e7681;
  --altcha-accent: #a371f7; --altcha-success: #a371f7; --altcha-radius: 999px;
}

/* 3. The site's own markup, styled by data-state, with defaults turned off. */
.pill { display: inline-flex; align-items: center; gap: 8px; padding: 8px 16px; border: 0;
  border-radius: 6px; background: #ddf4ff; color: #0969da; font: 600 14px system-ui, sans-serif; cursor: pointer; }
.pill::before { content: "\1F6E1"; }
[data-state="verifying"] .pill { background: #fff8c5; color: #9a6700; cursor: progress; }
[data-state="verifying"] .pill::before { content: "\23F3"; }
[data-state="verified"] .pill { background: #dafbe1; color: #1a7f37; cursor: default; }
[data-state="verified"] .pill::before { content: "\2714"; }
</style></head><body>
<h3>1. Default: {{"{{"}}.Widget{{"}}"}}</h3>
<form>{{.Widget}}</form>

<h3>2. Default markup, themed with CSS variables</h3>
<form><div class="dark">{{.Widget}}</div></form>

<h3>3. Own markup with data-unstyled</h3>
<form>
  <div class="flamego-altcha" data-unstyled data-challenge-url="/.altcha/challenge.json">
    <button type="button" class="altcha-check pill"><span class="altcha-label">Prove you're human</span></button>
  </div>
</form>
</body></html>`))

func main() {
	f := flamego.Classic()
	// ALTCHA_MAX_COUNTER sets the difficulty, such as 20000 to watch it work.
	maxCounter, _ := strconv.Atoi(os.Getenv("ALTCHA_MAX_COUNTER"))
	// ALTCHA_SECRET must be a long random value, such as from `openssl rand -hex 32`.
	f.Use(altcha.Challenger(altcha.Options{Secret: []byte(os.Getenv("ALTCHA_SECRET")), MaxCounter: maxCounter}))

	render := func(w http.ResponseWriter, c altcha.Challenge, result, class string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, map[string]any{"Widget": c.HTML(), "Result": result, "Class": class})
	}
	f.Get("/", func(w http.ResponseWriter, c altcha.Challenge) { render(w, c, "", "") })
	f.Get("/styles", func(w http.ResponseWriter, c altcha.Challenge) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = styles.Execute(w, map[string]any{"Widget": c.HTML()})
	})
	f.Post("/", func(w http.ResponseWriter, r *http.Request, c altcha.Challenge) {
		if err := c.Verify(r.FormValue(altcha.FieldName)); err != nil {
			w.WriteHeader(http.StatusForbidden)
			render(w, c, "Rejected: "+err.Error(), "bad")
			return
		}
		render(w, c, "Welcome, "+r.FormValue("name")+"! The proof of work checked out.", "ok")
	})

	addr := ":2830"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	_ = http.ListenAndServe(addr, f)
}
