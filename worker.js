// Copyright 2026 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Web Worker for widget.js. It solves an ALTCHA v3 PBKDF2 challenge by trying
// counters start, start+step, start+2*step, ... until the derived key starts
// with keyPrefix, and posts {counter, derivedKey} when it finds the answer.
const fromHex = (hex) => Uint8Array.from(hex.match(/../g) || [], (b) => parseInt(b, 16));
const toHex = (bytes) => Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");

self.onmessage = async (e) => {
  const { parameters: p, start, step } = e.data;
  try {
    if (!p.algorithm.startsWith("PBKDF2/")) throw new Error(`unsupported algorithm ${p.algorithm}`);
    const hash = p.algorithm.slice("PBKDF2/".length);
    const nonce = fromHex(p.nonce);
    const salt = fromHex(p.salt);
    const prefix = fromHex(p.keyPrefix);
    const password = new Uint8Array(nonce.length + 4);
    password.set(nonce);
    const view = new DataView(password.buffer);

    for (let counter = start; counter <= 0xffffffff; counter += step) {
      view.setUint32(nonce.length, counter);
      const key = await crypto.subtle.importKey("raw", password, "PBKDF2", false, ["deriveBits"]);
      const bits = new Uint8Array(await crypto.subtle.deriveBits(
        { name: "PBKDF2", salt, iterations: p.cost, hash }, key, p.keyLength * 8));
      if (prefix.every((b, i) => bits[i] === b)) {
        self.postMessage({ counter, derivedKey: toHex(bits) });
        return;
      }
    }
    throw new Error("no solution found");
  } catch (err) {
    self.postMessage({ error: String(err && err.message || err) });
  }
};
