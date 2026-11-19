# altcha

[![GitHub Workflow Status](https://img.shields.io/github/checks-status/flamego/altcha/main?logo=github&style=for-the-badge)](https://github.com/flamego/altcha/actions?query=branch%3Amain)
[![GoDoc](https://img.shields.io/badge/GoDoc-Reference-blue?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/flamego/altcha?tab=doc)

Package altcha is a middleware that provides self-hosted [ALTCHA](https://altcha.org) proof-of-work captchas for [Flamego](https://github.com/flamego/flamego).

## Installation

```zsh
go get github.com/flamego/altcha
```

## Getting started

```html
<!-- templates/home.tmpl -->
<form method="POST">
  {{.AltchaHTML}}
  <button>Submit</button>
</form>
```

```go
package main

import (
	"net/http"
	"os"

	"github.com/flamego/altcha"
	"github.com/flamego/flamego"
	"github.com/flamego/template"
)

func main() {
	f := flamego.Classic()
	f.Use(template.Templater())
	f.Use(altcha.Challenger(
		altcha.Options{
			Secret: []byte(os.Getenv("ALTCHA_SECRET")),
		},
	))
	f.Get("/", func(t template.Template, data template.Data, c altcha.Challenge) {
		data["AltchaHTML"] = c.HTML()
		t.HTML(http.StatusOK, "home")
	})
	f.Post("/", func(w http.ResponseWriter, r *http.Request, c altcha.Challenge) {
		if err := c.Verify(r.PostFormValue(altcha.FieldName)); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Verified!"))
	})

	f.Run()
}
```

## Getting help

- Read [documentation and examples](https://flamego.dev/middleware/altcha.html).
- Please [file an issue](https://github.com/flamego/flamego/issues) or [start a discussion](https://github.com/flamego/flamego/discussions) on the [flamego/flamego](https://github.com/flamego/flamego) repository.

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
