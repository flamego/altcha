// Copyright 2026 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Proof-of-work widget for github.com/flamego/altcha. It turns every
// <div class="flamego-altcha" data-challenge-url="..."> into a checkbox that
// solves an ALTCHA v3 challenge when clicked, or when its form is submitted,
// and writes the solution to a hidden "altcha" field. The solving runs in Web
// Workers loaded from worker.js next to this script.
(() => {
  if (window.flamegoAltcha) return;
  window.flamegoAltcha = true;

  // Default look. Every rule sits inside :where(), so it has zero specificity
  // and any site rule wins; the colors and sizes read CSS custom properties.
  // Add data-unstyled to the element to turn the defaults off entirely.
  const style = document.createElement("style");
  style.textContent = `
:where(.flamego-altcha) {
  --altcha-font: 14px/1.4 system-ui, -apple-system, "Segoe UI", sans-serif;
  --altcha-color: #1f2328;
  --altcha-background: #f6f8fa;
  --altcha-border: #d0d7de;
  --altcha-radius: 8px;
  --altcha-check-background: #fff;
  --altcha-check-border: #8c959f;
  --altcha-accent: #0969da;
  --altcha-success: #1a7f37;
  --altcha-error: #cf222e;
}
:where(.flamego-altcha:not([data-unstyled])) :where(.altcha-box) { display: inline-flex; align-items: center; gap: 12px; min-width: 260px;
  padding: 12px 14px; border: 1px solid var(--altcha-border); border-radius: var(--altcha-radius);
  background: var(--altcha-background); font: var(--altcha-font); color: var(--altcha-color); box-sizing: border-box; }
:where(.flamego-altcha:not([data-unstyled])) :where(.altcha-check) { flex: none; width: 22px; height: 22px; border: 2px solid var(--altcha-check-border);
  border-radius: 4px; background: var(--altcha-check-background); cursor: pointer; display: grid;
  place-items: center; box-sizing: border-box; }
:where(.flamego-altcha:not([data-unstyled])) :where(.altcha-check:focus-visible) { outline: 2px solid var(--altcha-accent); outline-offset: 2px; }
:where(.flamego-altcha:not([data-unstyled])) :where(.altcha-text) { flex: 1; }
:where(.flamego-altcha:not([data-unstyled])[data-state="verifying"]) :where(.altcha-check) {
  border-color: var(--altcha-accent); border-top-color: transparent; border-radius: 50%;
  animation: altcha-spin .8s linear infinite; cursor: default; }
:where(.flamego-altcha:not([data-unstyled])[data-state="verified"]) :where(.altcha-check) {
  border-color: var(--altcha-success); background: var(--altcha-success); cursor: default; }
:where(.flamego-altcha:not([data-unstyled])[data-state="verified"]) :where(.altcha-check)::after {
  content: ""; width: 5px; height: 10px; margin-top: -2px; border: solid #fff; border-width: 0 2px 2px 0;
  transform: rotate(45deg); }
:where(.flamego-altcha:not([data-unstyled])[data-state="verified"]) :where(.altcha-box) {
  border-color: var(--altcha-success); box-shadow: 0 0 0 1px var(--altcha-success); }
:where(.flamego-altcha:not([data-unstyled])[data-state="error"]) :where(.altcha-box) {
  border-color: var(--altcha-error); box-shadow: 0 0 0 1px var(--altcha-error); }
:where(.flamego-altcha:not([data-unstyled])[data-state="error"]) :where(.altcha-check) { border-color: var(--altcha-error); }
:where(.flamego-altcha:not([data-unstyled])[data-state="error"]) :where(.altcha-text) { color: var(--altcha-error); }
@keyframes altcha-spin { to { transform: rotate(360deg); } }`;
  document.head.prepend(style);

  // The worker sits next to this script, such as /.altcha/worker.js.
  const workerURL = new URL("worker.js", document.currentScript ? document.currentScript.src : location.href);

  // solve finds the counter for an ALTCHA v3 challenge. It runs one Web Worker
  // per CPU core, up to 8, each trying every n-th counter, so the page never
  // blocks while the browser works.
  function solve(c) {
    const n = Math.min(8, Math.max(1, navigator.hardwareConcurrency || 1));
    return new Promise((resolve, reject) => {
      const workers = [];
      let done = false;
      const stop = () => {
        done = true;
        workers.forEach((w) => w.terminate());
      };
      for (let i = 0; i < n; i++) {
        const w = new Worker(workerURL);
        w.onmessage = (e) => {
          if (done) return; // Another worker already finished.
          const d = e.data;
          if (d.error) {
            stop();
            reject(new Error(d.error));
          } else if (d.derivedKey) {
            stop();
            resolve({ counter: d.counter, derivedKey: d.derivedKey });
          }
        };
        w.onerror = (e) => {
          if (done) return;
          stop();
          reject(new Error(e.message || "worker failed"));
        };
        workers.push(w);
        w.postMessage({ parameters: c.parameters, start: i, step: n });
      }
    });
  }

  function mount(el) {
    if (el.dataset.state) return;
    // Sites can supply their own markup: any element with class altcha-check
    // inside the container becomes the control. Otherwise use the default.
    if (!el.querySelector(".altcha-check")) {
      el.innerHTML = `
<div class="altcha-box">
  <span class="altcha-check" role="checkbox" aria-checked="false" tabindex="0" aria-label="Verify that you are human"></span>
  <span class="altcha-text altcha-label"></span>
</div>`;
    }
    const check = el.querySelector(".altcha-check");
    const label = el.querySelector(".altcha-label");
    let input = el.querySelector("input[name=altcha]");
    if (!input) {
      input = document.createElement("input");
      input.type = "hidden";
      input.name = "altcha";
      el.appendChild(input);
    }

    // The container's data-state (idle, verifying, verified, error) is the
    // styling hook. The label is optional and keeps any text the site wrote
    // for the idle state.
    const idleLabel = label?.textContent.trim() || "I'm not a robot";
    function setState(state, text) {
      el.dataset.state = state;
      check.setAttribute("aria-checked", state === "verified" ? "true" : "false");
      if (label) label.textContent = text;
    }
    setState("idle", idleLabel);

    let expiryTimer = null;
    let solving = null;
    // sent is true once a submit carried the current solution out; it is
    // cleared whenever a new solution replaces it.
    let sent = false;
    // reset throws away the current solution, which works only once. A solve
    // in progress is left to finish, since it will produce a fresh solution.
    function reset() {
      if (solving) return;
      clearTimeout(expiryTimer);
      input.value = "";
      setState("idle", idleLabel);
    }

    function verify() {
      if (el.dataset.state === "verified") return Promise.resolve();
      if (solving) return solving;
      setState("verifying", "Verifying…");
      solving = (async () => {
        try {
          const res = await fetch(el.dataset.challengeUrl, { cache: "no-store" });
          if (!res.ok) throw new Error(`challenge: HTTP ${res.status}`);
          // Measure the challenge's lifetime against the server's clock, from
          // its Date header, so a browser clock that is off does not matter.
          const fetchedAt = performance.now();
          const serverNow = Date.parse(res.headers.get("Date")) || Date.now();
          const c = await res.json();
          const started = performance.now();
          const solution = await solve(c);
          const took = Math.round(performance.now() - started);

          // A challenge that expired, or is about to, while solving would only
          // be rejected, so report it instead of handing it to a submit.
          const left = c.parameters.expiresAt * 1000 - serverNow - (performance.now() - fetchedAt);
          const delay = left - Math.min(5000, Math.max(0, left / 10));
          if (delay <= 0) throw new Error("challenge expired while solving");

          sent = false;
          input.value = btoa(JSON.stringify({
            challenge: { parameters: c.parameters, signature: c.signature },
            solution: { counter: solution.counter, derivedKey: solution.derivedKey, time: took },
          }));
          setState("verified", "Verified");

          // Start over shortly before the challenge expires, so a stale
          // solution is never sent. setTimeout cannot wait longer than about
          // 24.8 days, so skip the timer for longer lifetimes.
          if (delay <= 0x7fffffff) {
            expiryTimer = setTimeout(reset, delay);
          }
        } catch (err) {
          input.value = "";
          setState("error", "Verification failed");
          throw err;
        } finally {
          solving = null;
        }
      })();
      return solving;
    }

    check.addEventListener("click", () => verify().catch(() => {}));
    check.addEventListener("keydown", (e) => {
      if (e.key === " " || e.key === "Enter") {
        e.preventDefault();
        verify().catch(() => {});
      }
    });

    // A solution goes back to the browser's page cache with the page, but the
    // server already used it, so start over when the page is shown again.
    window.addEventListener("pageshow", (e) => {
      if (e.persisted) reset();
    });

    const form = el.closest("form");
    if (form) {
      // The submit event held back while solving; only it is sent afterwards.
      let pendingSubmit = null;
      // Resetting the form also cancels a submit held back while solving.
      form.addEventListener("reset", () => {
        pendingSubmit = null;
        reset();
      });

      // Submitting before verifying solves first, then submits. The listener
      // runs in the capture phase on the form and stops the event, so submit
      // handlers on the form never see it without a solution. Only the first
      // submit while solving goes through afterwards, so forms that submit
      // with fetch do not send the same solution twice.
      //
      // A submit that goes through may send the solution, so the next submit
      // starts over with a new one. The solution stays in the form until then,
      // so site handlers that read the form after awaiting something still
      // find it.
      form.addEventListener("submit", (e) => {
        if (sent) {
          sent = false;
          reset();
        }
        if (el.dataset.state === "verified" && !pendingSubmit) {
          sent = true;
          return;
        }
        e.preventDefault();
        e.stopImmediatePropagation();
        if (pendingSubmit) return;
        pendingSubmit = e;
        verify().then(
          () => {
            // A reset, and possibly a newer submit, replaced this one.
            if (pendingSubmit !== e) return;
            pendingSubmit = null;
            // The site may have removed or changed the button while solving.
            try {
              form.requestSubmit(e.submitter && e.submitter.form === form ? e.submitter : undefined);
            } catch {
              form.requestSubmit();
            }
          },
          () => {
            if (pendingSubmit === e) pendingSubmit = null;
          },
        );
      }, { capture: true });
    }
  }

  function mountAll() {
    document.querySelectorAll(".flamego-altcha").forEach(mount);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mountAll);
  else mountAll();
})();
