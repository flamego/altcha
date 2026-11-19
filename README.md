# altcha

[![GitHub Workflow Status](https://img.shields.io/github/checks-status/flamego/altcha/main?logo=github&style=for-the-badge)](https://github.com/flamego/altcha/actions?query=branch%3Amain)
[![GoDoc](https://img.shields.io/badge/GoDoc-Reference-blue?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/flamego/altcha?tab=doc)

Package altcha is a middleware for [Flamego](https://github.com/flamego/flamego) that tells people from bots with a proof-of-work check instead of an image to read. The browser spends a moment searching for a number, and the server checks the answer with two HMACs. There is no third-party service, no cookie, and no tracking.

Challenges and solutions use the [ALTCHA](https://altcha.org) v3 format with `PBKDF2/SHA-256`, so you can use either the small widget this package serves or the official `altcha-widget`.

## Installation

```zsh
go get github.com/flamego/altcha
```

## Getting started

```go
package main

import (
	"html/template"
	"net/http"
	"os"

	"github.com/flamego/flamego"

	"github.com/flamego/altcha"
)

func main() {
	f := flamego.Classic()
	f.Use(altcha.Challenger(altcha.Options{
		Secret: []byte(os.Getenv("ALTCHA_SECRET")), // At least 16 random bytes.
	}))

	f.Get("/", func(w http.ResponseWriter, c altcha.Challenge) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, c.HTML())
	})
	f.Post("/", func(w http.ResponseWriter, r *http.Request, c altcha.Challenge) {
		if err := c.Verify(r.FormValue(altcha.FieldName)); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("Welcome!"))
	})
	f.Run()
}

var page = template.Must(template.New("").Parse(`<form method="post">
  {{.}}
  <button type="submit">Sign up</button>
</form>`))
```

`Challenger` serves these files under `URLPrefix` (`/.altcha/` by default):

| Path | What it is |
| --- | --- |
| `challenge.json` | A new signed challenge on every request. |
| `widget.js` | The widget script that `HTML()` loads. |
| `worker.js` | The Web Worker the widget runs to solve challenges. |

`HTML()` renders the widget. When the person clicks the checkbox, or submits the form before doing so, the widget fetches a challenge, solves it in Web Workers (one per CPU core, up to 8), and writes the solution to a hidden form field named `altcha` (`altcha.FieldName`). A solution works only once, so the widget throws it away when the form is submitted again, when the form is reset, and when the challenge is about to expire, and solves a new challenge before that submit goes through. A submit the site cancels, such as for its own validation, still counts, so the next one solves again. Forms that submit with `fetch` get the same behavior, because the widget holds back the `submit` event until it has a solution. Submit listeners on `document` or `window` in the capture phase run before the widget's and can still see the form without one.

`Verify` returns `nil` for a good solution, or one of these errors:

| Error | Meaning |
| --- | --- |
| `ErrInvalid` | The payload is malformed, was not signed by this server, had its parameters changed, or does not solve its challenge. |
| `ErrExpired` | The challenge is older than `Expiry`. |
| `ErrReplayed` | This solution was already accepted once. |

Any other error comes from the `ReplayStore`.

## Options

| Option | Default | Description |
| --- | --- | --- |
| `Secret` | required | Signs challenges. At least 16 bytes of random data, such as `openssl rand -hex 32`, and the same on every server that verifies solutions. Anyone who learns it can mint solved challenges. |
| `URLPrefix` | `/.altcha/` | Where the challenge and scripts are served. A trailing slash is added when missing. |
| `Cost` | `5000` | PBKDF2 iterations for each counter the browser tries. |
| `MaxCounter` | `500` | The largest counter a challenge can hide, up to 4294967295. |
| `Expiry` | `5m` | How long a challenge can be solved and submitted. |
| `Store` | in memory | Remembers accepted solutions so each one works once. |

### Difficulty

The browser tries `MaxCounter / 2` counters on average, and each try runs `Cost` PBKDF2 iterations, so the expected work is about `Cost × MaxCounter / 2` iterations, split over the browser's cores. Raising either option makes each solution cost more, both for people and for bots. Verifying costs the server the same small amount whatever the difficulty, because it checks the answer with HMACs and never runs PBKDF2. Issuing a challenge does run PBKDF2 once at `Cost` iterations (about 3 ms of CPU at the default), so a high `Cost` makes `challenge.json` expensive to serve; see [Rate limiting](#rate-limiting).

At the defaults, a desktop browser with 8 workers solved a challenge in about 0.7 s. Phones with fewer, slower cores take longer, so measure on the devices your visitors use before raising the difficulty, or lower it if they wait too long:

```go
altcha.Options{
	Secret:     secret,
	Cost:       5000,
	MaxCounter: 200, // Lower for an easier check, higher for a harder one.
}
```

Mount a single `Challenger` with `f.Use`. A challenge only verifies with the `Cost` and `URLPrefix` that issued it, so changing either invalidates challenges already handed out.

### Rate limiting

Anyone can request `challenge.json` without solving anything, and each request costs the server one PBKDF2 run. Rate-limit that path per client, as you would a login form, especially when you raise `Cost`.

### Running more than one server

The default store lives in memory, so a solution accepted by one process could be accepted again by another. When several processes verify solutions, give them the same `Secret` and a shared `Store`, such as one backed by Redis:

```go
type ReplayStore interface {
	// Use marks key as used until expiresAt. It returns false when key is
	// already marked and has not expired.
	Use(key string, expiresAt time.Time) (bool, error)
}
```

`Use` must check and mark the key in one atomic step, for example with Redis `SET key 1 NX EXAT expiresAt`. The `expiresAt` it receives is already a minute past the challenge's expiry, so a request that passes the expiry check at the last moment still finds the key.

## Styling the widget

`HTML()` renders the default look. There are three ways to change it.

**1. Override the default rules.** Every built-in rule is wrapped in `:where()`, which has zero specificity, so any rule of yours wins without `!important`:

```css
.flamego-altcha .altcha-box { border-radius: 0; }
```

**2. Theme it with CSS custom properties:**

```css
.flamego-altcha {
  --altcha-font: 14px/1.4 system-ui, sans-serif;
  --altcha-color: #e6edf3;
  --altcha-muted: #8b949e;
  --altcha-background: #161b22;
  --altcha-border: #30363d;
  --altcha-radius: 999px;
  --altcha-check-background: #0d1117;
  --altcha-check-border: #6e7681;
  --altcha-accent: #a371f7;   /* Focus ring and spinner. */
  --altcha-success: #1a7f37;
  --altcha-error: #cf222e;
}
```

**3. Write your own markup.** Add `data-unstyled` to turn the default rules off, and put any element with class `altcha-check` inside the container; it becomes the control. The container's `data-state` is `idle`, `verifying`, `verified` or `error`, so you can style each state. Elements with class `altcha-label` and `altcha-note` are optional: the widget writes status text into them, and keeps the idle text you wrote, which also covers translation.

```html
<div class="flamego-altcha" data-unstyled data-challenge-url="/.altcha/challenge.json">
  <button type="button" class="altcha-check"><span class="altcha-label">Prove you're human</span></button>
</div>
<script src="/.altcha/widget.js" defer></script>
```

```css
.flamego-altcha[data-state="verifying"] .altcha-check { opacity: .6; }
.flamego-altcha[data-state="verified"] .altcha-check { background: #dafbe1; }
```

## Using the official ALTCHA widget

The official widget (`altcha` 3.x on npm) works with this server without changes. Skip `HTML()` and point the widget at the challenge URL; it writes its solution to the same `altcha` field:

```html
<script type="module" src="https://cdn.jsdelivr.net/npm/altcha@3/dist/main/altcha.min.js"></script>
<form method="post">
  <altcha-widget challenge="/.altcha/challenge.json"></altcha-widget>
  <button type="submit">Sign up</button>
</form>
```

Only the v3 format is supported. The v2 widget and other v1 clients cannot solve these challenges.

## How it works

1. The server picks a random 16-byte nonce and salt, and a random counter from 0 to `MaxCounter`. It derives a 32-byte key with `PBKDF2-SHA256(password = nonce ‖ uint32BE(counter), salt, Cost)`.
2. The challenge contains the first 16 bytes of that key as `keyPrefix`, an HMAC of the whole key as `keySignature`, and `expiresAt`. All parameters are signed with an HMAC of their JSON.
3. The browser tries counters until it derives a key that starts with `keyPrefix`, and sends back the challenge with the counter and the whole key.
4. The server checks the parameter signature and the `Cost`, checks the key against `keySignature`, checks the expiry, and records the challenge in the `ReplayStore`.

The signing keys are derived from `Secret` and `URLPrefix`, one for each purpose.

Proof of work makes abuse cost CPU time; it does not prove that a person is present. A determined attacker with enough compute can still solve challenges, so combine it with rate limiting for forms that matter.

## Getting help

- Please [file an issue](https://github.com/flamego/flamego/issues) or [start a discussion](https://github.com/flamego/flamego/discussions) on the [flamego/flamego](https://github.com/flamego/flamego) repository.

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
