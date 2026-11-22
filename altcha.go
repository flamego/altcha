// Copyright 2026 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package altcha is a middleware that tells people from bots with a proof-of-work
// challenge: the browser spends a moment searching for a counter whose PBKDF2
// key starts with a prefix signed by the server, and the server checks the
// answer with two HMACs. Nothing is shown to read and no third party is
// involved.
//
// Challenges and solutions use the ALTCHA v3 format (https://altcha.org), so the
// official altcha-widget works as well as the widget this package serves.
package altcha

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/flamego/flamego"
)

// FieldName is the name of the form field the widget writes the solution to.
const FieldName = "altcha"

const (
	algorithm = "PBKDF2/SHA-256"
	keyLength = 32 // Bytes of the derived key.

	// maxPayload is far above a real payload, about 700 bytes, and stops
	// oversized input before any decoding work.
	maxPayload = 4096
	// replayMargin keeps a used solution recorded past its expiry, so a request
	// that passed the expiry check just before it cannot be accepted again.
	replayMargin = time.Minute
)

var (
	// ErrInvalid is returned when a solution is malformed, is not signed by this
	// server, or does not solve its challenge.
	ErrInvalid = errors.New("altcha: invalid solution")
	// ErrExpired is returned when a solution is valid but its challenge expired.
	ErrExpired = errors.New("altcha: challenge expired")
	// ErrReplayed is returned when a solution was already accepted once.
	ErrReplayed = errors.New("altcha: solution already used")
)

// Challenge is a proof-of-work service that renders the widget and verifies
// solutions.
type Challenge interface {
	// HTML returns the HTML that renders the widget. Put it inside a form; the
	// widget writes its solution to the form field named FieldName. The
	// optional Messages replace the widget's English texts, such as with
	// translations from the i18n middleware.
	HTML(msgs ...Messages) template.HTML
	// Verify verifies a solution submitted by the widget. Each solution is
	// accepted at most once. It returns ErrInvalid, ErrExpired, ErrReplayed, or
	// an error from the ReplayStore.
	Verify(payload string) error
}

// Messages are the texts the widget shows in each state. An empty field keeps
// the English default.
type Messages struct {
	// Idle is shown before verifying. Default is "I'm not a robot".
	Idle string
	// Verifying is shown while the browser solves the challenge. Default is
	// "Verifying…".
	Verifying string
	// Verified is shown once the challenge is solved. Default is "Verified".
	Verified string
	// Error is shown when verifying fails. Default is "Verification failed".
	Error string
	// Retry is shown under Error to tell the user how to try again. Default
	// is "Click to try again".
	Retry string
}

// ReplayStore remembers which solutions were already accepted.
type ReplayStore interface {
	// Use marks key as used until expiresAt. It returns false when key is
	// already marked and has not expired.
	Use(key string, expiresAt time.Time) (bool, error)
}

// Options contains options for the altcha.Challenger middleware.
type Options struct {
	// URLPrefix is the URL path prefix for serving challenges and the widget
	// scripts. A trailing slash is added when missing. Default is "/.altcha/".
	URLPrefix string
	// Secret is the key used to sign challenges. It is required, must be at
	// least 16 bytes of random data, and must be the same on every server that
	// verifies solutions.
	Secret []byte
	// Cost is the number of PBKDF2 iterations for each counter the browser
	// tries. Default is 5000.
	Cost int
	// MaxCounter is the largest counter a challenge can hide, up to
	// math.MaxUint32. The browser tries MaxCounter/2 counters on average.
	// Default is 5000, which takes about 1 second on average and 2 seconds at
	// most in Chrome on an Apple M4 Pro.
	MaxCounter int
	// Expiry is how long a challenge can be solved and submitted. Default is 5
	// minutes.
	Expiry time.Duration
	// Store records accepted solutions so they cannot be used twice. Default is
	// an in-memory store, which only works when a single process verifies
	// solutions.
	Store ReplayStore
}

var (
	//go:embed widget.js
	widgetJS []byte
	//go:embed worker.js
	workerJS []byte
)

// Challenger returns a middleware handler that injects altcha.Challenge into the
// request context, and serves challenges and the widget script under
// URLPrefix.
func Challenger(opts Options) flamego.Handler {
	ch := newChallenge(opts)
	opts = ch.opts
	return flamego.ContextInvoker(func(c flamego.Context) {
		c.MapTo(ch, (*Challenge)(nil))

		w := c.ResponseWriter()
		switch c.Request().URL.Path {
		case opts.URLPrefix + "challenge.json":
			body, err := json.Marshal(ch.issue(time.Now().Add(opts.Expiry)))
			if err != nil {
				panic("altcha: encode challenge: " + err.Error())
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		case opts.URLPrefix + "widget.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(widgetJS)
		case opts.URLPrefix + "worker.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(workerJS)
		}
	})
}

// parameters are the challenge parameters. Fields are in alphabetical order, as
// ALTCHA signs them.
type parameters struct {
	Algorithm    string `json:"algorithm"`
	Cost         int    `json:"cost"`
	ExpiresAt    int64  `json:"expiresAt"`
	KeyLength    int    `json:"keyLength"`
	KeyPrefix    string `json:"keyPrefix"`
	KeySignature string `json:"keySignature"`
	Nonce        string `json:"nonce"`
	Salt         string `json:"salt"`
}

// challengeJSON is what the browser receives, and sends back inside the
// payload.
type challengeJSON struct {
	Parameters parameters `json:"parameters"`
	Signature  string     `json:"signature"`
}

// payloadJSON is what the browser sends back, base64-encoded.
type payloadJSON struct {
	Challenge challengeJSON `json:"challenge"`
	Solution  struct {
		Counter    uint32 `json:"counter"`
		DerivedKey string `json:"derivedKey"`
	} `json:"solution"`
}

func subkey(secret []byte, purpose string) []byte {
	return hmacSum(secret, []byte(purpose))
}

func hmacSum(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// deriveKey returns the PBKDF2 key for a counter: the password is the nonce
// followed by the counter as a big-endian uint32.
func deriveKey(nonce, salt []byte, counter uint32, cost int) []byte {
	password := binary.BigEndian.AppendUint32(append([]byte(nil), nonce...), counter)
	key, err := pbkdf2.Key(sha256.New, string(password), salt, cost, keyLength)
	if err != nil {
		panic("altcha: derive key: " + err.Error())
	}
	return key
}

var _ Challenge = (*challenge)(nil)

// newChallenge validates opts, fills in defaults, and derives the signing keys.
// The keys depend on URLPrefix, so a challenge only verifies under the
// Challenger that issued it, even when several share a Secret.
func newChallenge(opts Options) *challenge {
	if len(opts.Secret) < 16 {
		panic("altcha: secret must be at least 16 bytes")
	}
	if opts.URLPrefix == "" {
		opts.URLPrefix = "/.altcha/"
	}
	if !strings.HasPrefix(opts.URLPrefix, "/") {
		opts.URLPrefix = "/" + opts.URLPrefix
	}
	if !strings.HasSuffix(opts.URLPrefix, "/") {
		opts.URLPrefix += "/"
	}
	if opts.Cost <= 0 {
		opts.Cost = 5000
	}
	if opts.MaxCounter <= 0 {
		opts.MaxCounter = 5000
	}
	// A variable, not a constant, so this compiles where int is 32 bits.
	if maxCounter := uint64(math.MaxUint32); uint64(opts.MaxCounter) > maxCounter {
		opts.MaxCounter = int(maxCounter)
	}
	if opts.Expiry <= 0 {
		opts.Expiry = 5 * time.Minute
	}
	if opts.Store == nil {
		opts.Store = newMemoryStore()
	}
	return &challenge{
		opts:         opts,
		signatureKey: subkey(opts.Secret, "altcha challenge signature "+opts.URLPrefix),
		keySigKey:    subkey(opts.Secret, "altcha key signature "+opts.URLPrefix),
	}
}

type challenge struct {
	opts         Options
	signatureKey []byte // Signs the challenge parameters.
	keySigKey    []byte // Signs the derived key, so Verify needs no PBKDF2.
}

// issue hides a random counter up to MaxCounter in a signed challenge. The
// browser sees the first half of the counter's derived key and must find the
// counter to learn the whole key, which keySignature lets Verify check.
func (c *challenge) issue(expires time.Time) challengeJSON {
	nonce := make([]byte, 16)
	salt := make([]byte, 16)
	_, _ = rand.Read(nonce) // Never returns an error.
	_, _ = rand.Read(salt)
	n, err := rand.Int(rand.Reader, big.NewInt(int64(c.opts.MaxCounter)+1))
	if err != nil {
		panic("altcha: generate counter: " + err.Error())
	}

	key := deriveKey(nonce, salt, uint32(n.Uint64()), c.opts.Cost)
	params := parameters{
		Algorithm:    algorithm,
		Cost:         c.opts.Cost,
		ExpiresAt:    expires.Add(time.Second - 1).Unix(), // Round up to whole seconds.
		KeyLength:    keyLength,
		KeyPrefix:    hex.EncodeToString(key[:keyLength/2]),
		KeySignature: hex.EncodeToString(hmacSum(c.keySigKey, key)),
		Nonce:        hex.EncodeToString(nonce),
		Salt:         hex.EncodeToString(salt),
	}
	return challengeJSON{Parameters: params, Signature: c.sign(params)}
}

func (c *challenge) sign(params parameters) string {
	body, err := json.Marshal(params)
	if err != nil {
		panic("altcha: encode parameters: " + err.Error())
	}
	return hex.EncodeToString(hmacSum(c.signatureKey, body))
}

func (c *challenge) HTML(msgs ...Messages) template.HTML {
	prefix := template.HTMLEscapeString(c.opts.URLPrefix)
	var attrs strings.Builder
	for _, m := range msgs {
		for _, a := range []struct{ name, value string }{
			{"idle", m.Idle},
			{"verifying", m.Verifying},
			{"verified", m.Verified},
			{"error", m.Error},
			{"retry", m.Retry},
		} {
			if a.value != "" {
				fmt.Fprintf(&attrs, ` data-message-%s="%s"`, a.name, template.HTMLEscapeString(a.value))
			}
		}
	}
	return template.HTML(fmt.Sprintf(`
<div class="flamego-altcha" data-challenge-url="%[1]schallenge.json"%[2]s></div>
<script src="%[1]swidget.js" defer></script>`, prefix, attrs.String()))
}

func (c *challenge) Verify(payload string) error {
	if len(payload) > maxPayload {
		return ErrInvalid
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ErrInvalid
	}
	var p payloadJSON
	if err = json.Unmarshal(raw, &p); err != nil {
		return ErrInvalid
	}

	params := p.Challenge.Parameters
	if params.Algorithm != algorithm || params.Cost != c.opts.Cost || params.KeyLength != keyLength ||
		!hmac.Equal([]byte(p.Challenge.Signature), []byte(c.sign(params))) {
		return ErrInvalid
	}
	key, err := hex.DecodeString(p.Solution.DerivedKey)
	if err != nil || len(key) != keyLength ||
		!hmac.Equal([]byte(params.KeySignature), []byte(hex.EncodeToString(hmacSum(c.keySigKey, key)))) {
		return ErrInvalid
	}

	expires := time.Unix(params.ExpiresAt, 0)
	if !time.Now().Before(expires) {
		return ErrExpired
	}

	fresh, err := c.opts.Store.Use(p.Challenge.Signature, expires.Add(replayMargin))
	if err != nil {
		return fmt.Errorf("altcha: record solution: %w", err)
	}
	if !fresh {
		return ErrReplayed
	}
	return nil
}
