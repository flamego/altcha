// Copyright 2026 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package altcha

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flamego/flamego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cheap settings so tests solve challenges quickly.
var testOptions = Options{Secret: []byte("test secret, 16+ bytes"), Cost: 10, MaxCounter: 200}

// solve searches counters like the widget does, independently of the server's
// helpers, and returns the base64 payload.
func solve(t *testing.T, c challengeJSON) string {
	p := c.Parameters
	nonce, _ := hex.DecodeString(p.Nonce)
	salt, _ := hex.DecodeString(p.Salt)
	for counter := 0; counter < 1<<16; counter++ {
		password := append(append([]byte(nil), nonce...), byte(counter>>24), byte(counter>>16), byte(counter>>8), byte(counter))
		key, err := pbkdf2.Key(sha256.New, string(password), salt, p.Cost, p.KeyLength)
		require.NoError(t, err)
		if strings.HasPrefix(hex.EncodeToString(key), p.KeyPrefix) {
			return encode(t, c, uint32(counter), hex.EncodeToString(key))
		}
	}
	t.Fatal("no solution")
	return ""
}

func encode(t *testing.T, c challengeJSON, counter uint32, derivedKey string) string {
	var p payloadJSON
	p.Challenge = c
	p.Solution.Counter = counter
	p.Solution.DerivedKey = derivedKey
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func decode(t *testing.T, payload string) payloadJSON {
	raw, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	var p payloadJSON
	require.NoError(t, json.Unmarshal(raw, &p))
	return p
}

func newServer(opts Options) *flamego.Flame {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Challenger(opts))
	f.Get("/", func(c Challenge) string { return string(c.HTML()) })
	f.Post("/", func(r *http.Request, c Challenge) string {
		if err := c.Verify(r.FormValue(FieldName)); err != nil {
			return err.Error()
		}
		return "ok"
	})
	return f
}

func get(f *flamego.Flame, path string) *httptest.ResponseRecorder {
	resp := httptest.NewRecorder()
	f.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
	return resp
}

func fetchChallenge(t *testing.T, f *flamego.Flame) challengeJSON {
	resp := get(f, "/.altcha/challenge.json")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "application/json", resp.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
	var c challengeJSON
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &c))
	return c
}

func submit(f *flamego.Flame, payload string) string {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Form = map[string][]string{FieldName: {payload}}
	resp := httptest.NewRecorder()
	f.ServeHTTP(resp, req)
	return resp.Body.String()
}

func TestChallenger(t *testing.T) {
	f := newServer(testOptions)

	assert.Equal(t, `
<div class="flamego-altcha" data-challenge-url="/.altcha/challenge.json"></div>
<script src="/.altcha/widget.js" defer></script>`, get(f, "/").Body.String())
	for _, path := range []string{"/.altcha/widget.js", "/.altcha/worker.js"} {
		resp := get(f, path)
		assert.Equal(t, http.StatusOK, resp.Code, path)
		assert.Equal(t, "text/javascript; charset=utf-8", resp.Header().Get("Content-Type"), path)
	}

	// The challenge has the fields the official widget requires.
	resp := get(f, "/.altcha/challenge.json")
	var raw struct {
		Parameters map[string]any `json:"parameters"`
		Signature  string         `json:"signature"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))
	for _, field := range []string{"algorithm", "nonce", "salt", "keyPrefix", "cost", "keyLength", "expiresAt"} {
		assert.Contains(t, raw.Parameters, field)
	}
	assert.Len(t, raw.Signature, 64)

	c := fetchChallenge(t, f)
	assert.Equal(t, "PBKDF2/SHA-256", c.Parameters.Algorithm)
	assert.Equal(t, 10, c.Parameters.Cost)
	assert.Len(t, c.Parameters.KeyPrefix, 32, "half of a 32-byte key, hex-encoded")
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), time.Unix(c.Parameters.ExpiresAt, 0), 2*time.Second)

	good := solve(t, c)
	assert.LessOrEqual(t, decode(t, good).Solution.Counter, uint32(200))
	assert.Equal(t, "ok", submit(f, good))
	assert.Equal(t, ErrReplayed.Error(), submit(f, good), "same solution twice")
}

func TestChallenge_HTMLMessages(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Challenger(testOptions))
	f.Get("/", func(c Challenge) string {
		return string(c.HTML(Messages{Idle: `我不是"机器人"`, Verified: "<b>已验证</b>", Retry: "点击重试"}))
	})

	// Empty fields are left out so the widget keeps its English default, and
	// values are escaped for the attribute.
	assert.Equal(t, `
<div class="flamego-altcha" data-challenge-url="/.altcha/challenge.json" data-message-idle="我不是&#34;机器人&#34;" data-message-verified="&lt;b&gt;已验证&lt;/b&gt;" data-message-retry="点击重试"></div>
<script src="/.altcha/widget.js" defer></script>`, get(f, "/").Body.String())
}

func TestVerify_Invalid(t *testing.T) {
	f := newServer(testOptions)
	c := fetchChallenge(t, f)
	good := solve(t, c)
	p := decode(t, good)

	// A challenge from a server with a different secret.
	other := newServer(Options{Secret: []byte("other secret, 16+ bytes"), Cost: 10, MaxCounter: 200})
	forged := solve(t, fetchChallenge(t, other))

	// The client cannot loosen any signed parameter.
	tamper := func(change func(*parameters)) string {
		c := p.Challenge
		change(&c.Parameters)
		return encode(t, c, p.Solution.Counter, p.Solution.DerivedKey)
	}
	extended := tamper(func(p *parameters) { p.ExpiresAt += 3600 })
	cheaper := tamper(func(p *parameters) { p.Cost = 1 })
	noKeySignature := tamper(func(p *parameters) { p.KeySignature = "" })

	// A key with the right prefix but a wrong second half, as a client that
	// skipped the work might guess.
	key, _ := hex.DecodeString(p.Solution.DerivedKey)
	key[keyLength-1] ^= 1
	guessed := encode(t, p.Challenge, p.Solution.Counter, hex.EncodeToString(key))

	for name, payload := range map[string]string{
		"empty":            "",
		"not base64":       "!!",
		"not JSON":         base64.StdEncoding.EncodeToString([]byte("nope")),
		"no solution":      encode(t, p.Challenge, 0, ""),
		"short key":        encode(t, p.Challenge, p.Solution.Counter, p.Solution.DerivedKey[:32]),
		"guessed key":      guessed,
		"forged":           forged,
		"extended expiry":  extended,
		"cheaper cost":     cheaper,
		"no key signature": noKeySignature,
	} {
		assert.Equal(t, ErrInvalid.Error(), submit(f, payload), name)
	}

	// The rejected attempts did not use up the real solution.
	assert.Equal(t, "ok", submit(f, good))
}

func TestVerify_Expired(t *testing.T) {
	opts := testOptions
	opts.Store = newMemoryStore()
	f := newServer(opts)
	ch := newChallenge(opts)
	assert.Equal(t, ErrExpired.Error(), submit(f, solve(t, ch.issue(time.Now().Add(-time.Second)))))
}

func TestChallenger_ShortSecret(t *testing.T) {
	msg := "altcha: secret must be at least 16 bytes"
	assert.PanicsWithValue(t, msg, func() { Challenger(Options{}) })
	assert.PanicsWithValue(t, msg, func() { Challenger(Options{Secret: []byte("change-me")}) })
}

// A challenge only verifies under the Challenger that issued it, so an easy
// Challenger cannot stand in for a harder one sharing the Secret.
func TestVerify_OtherChallenger(t *testing.T) {
	easy := newServer(testOptions)
	samePrefixHarder := testOptions
	samePrefixHarder.Cost = 20
	otherPrefix := testOptions
	otherPrefix.URLPrefix = "/other"
	good := solve(t, fetchChallenge(t, easy))
	assert.Equal(t, ErrInvalid.Error(), submit(newServer(samePrefixHarder), good), "different cost")
	assert.Equal(t, ErrInvalid.Error(), submit(newServer(otherPrefix), good), "different prefix")
	assert.Equal(t, "ok", submit(easy, good))
}

func TestChallenger_PrefixWithoutSlashes(t *testing.T) {
	for _, prefix := range []string{"/captcha", "captcha/", "captcha"} {
		opts := testOptions
		opts.URLPrefix = prefix
		f := newServer(opts)
		assert.Contains(t, get(f, "/").Body.String(), `src="/captcha/widget.js"`, prefix)
		for _, path := range []string{"/captcha/challenge.json", "/captcha/widget.js", "/captcha/worker.js"} {
			assert.Equal(t, http.StatusOK, get(f, path).Code, prefix+" "+path)
		}
	}
}

func TestChallenger_SubSecondExpiry(t *testing.T) {
	opts := testOptions
	opts.Expiry = 300 * time.Millisecond
	f := newServer(opts)
	issued := time.Now()
	c := fetchChallenge(t, f)
	// Rounding down to whole seconds would cut the lifetime short, or leave
	// the challenge expired as soon as it is issued.
	assert.False(t, time.Unix(c.Parameters.ExpiresAt, 0).Before(issued.Add(opts.Expiry)))
}

func TestChallenger_HugeMaxCounter(t *testing.T) {
	opts := testOptions
	opts.MaxCounter = math.MaxInt
	assert.Equal(t, http.StatusOK, get(newServer(opts), "/.altcha/challenge.json").Code)
}

func TestVerify_ReplayAfterExpiryCheck(t *testing.T) {
	// The store must keep the solution past expiresAt, or a second request
	// arriving at the boundary finds it gone.
	var recorded time.Time
	opts := testOptions
	opts.Store = storeFunc(func(_ string, expiresAt time.Time) (bool, error) {
		recorded = expiresAt
		return true, nil
	})
	f := newServer(opts)
	c := fetchChallenge(t, f)
	require.Equal(t, "ok", submit(f, solve(t, c)))
	assert.True(t, recorded.After(time.Unix(c.Parameters.ExpiresAt, 0)))
}

type storeFunc func(string, time.Time) (bool, error)

func (f storeFunc) Use(key string, expiresAt time.Time) (bool, error) { return f(key, expiresAt) }
