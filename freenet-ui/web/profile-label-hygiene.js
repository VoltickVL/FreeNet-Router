;(() => {
  'use strict';
  if (window.__freenetProfileLabelHygieneLoaded) return;
  window.__freenetProfileLabelHygieneLoaded = true;

  const q = (selector, root = document) => root.querySelector(selector);
  const regional = cp => cp >= 0x1F1E6 && cp <= 0x1F1FF;

  function stripLeadingCountryDecoration(value) {
    let text = String(value || '').trim();
    for (let pass = 0; pass < 8 && text; pass++) {
      const before = text;
      text = text.replace(/^[A-Za-z]{2}(?=[\s·:|/\-])(?:[\s·:|/\-])*/u, '').trimStart();
      let chars = Array.from(text);
      if (chars.length >= 2 && regional(chars[0].codePointAt(0)) && regional(chars[1].codePointAt(0))) {
        text = chars.slice(2).join('').replace(/^[\s·:|/\-]+/u, '').trimStart();
      }
      if (text === before) break;
    }
    return text.trim();
  }

  function normalizeNode(node) {
    if (!node) return;
    const clean = stripLeadingCountryDecoration(node.textContent);
    if (clean && clean !== node.textContent) node.textContent = clean;
  }

  function normalizeCurrentVPN() {
    normalizeNode(q('#fn3Profile'));
    normalizeNode(q('#fn3ProfileSmall'));
  }

  let scheduled = false;
  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(() => {
      scheduled = false;
      normalizeCurrentVPN();
    });
  }

  function boot() {
    normalizeCurrentVPN();
    if (typeof MutationObserver === 'function') {
      const root = document.body || document.documentElement;
      if (root) new MutationObserver(schedule).observe(root, {childList:true,subtree:true,characterData:true});
    }
    window.addEventListener('hashchange', schedule);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot, {once:true});
  else boot();
})();
