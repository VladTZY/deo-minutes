// DOM helpers used by the Go join flow. Matches elements by visible text,
// aria-label, title or placeholder so Webex class renames do not break us.
(() => {
  if (window.__deoPage) return;

  const lower = (s) => (s || "").toString().toLowerCase();

  // Every element, descending into open shadow roots and same-origin iframes.
  function allElements(root, out) {
    out = out || [];
    const walk = (node) => {
      if (!node) return;
      const kids = node.querySelectorAll ? node.querySelectorAll("*") : [];
      for (const el of kids) {
        out.push(el);
        if (el.shadowRoot) walk(el.shadowRoot);
        if (el.tagName === "IFRAME") {
          try { if (el.contentDocument) walk(el.contentDocument); } catch (e) { /* cross-origin */ }
        }
      }
    };
    walk(root || document);
    return out;
  }

  function visible(el) {
    if (!el.getClientRects || el.getClientRects().length === 0) return false;
    const st = (el.ownerDocument.defaultView || window).getComputedStyle(el);
    return st.visibility !== "hidden" && st.display !== "none" && !el.disabled;
  }

  // Text of the elements named by aria-labelledby and of <label for=...>.
  // Webex's own inputs carry their name only this way, inside a shadow root.
  function labelText(el) {
    const parts = [];
    const ids = el.getAttribute && el.getAttribute("aria-labelledby");
    const root = el.getRootNode && el.getRootNode();
    if (ids && root && root.getElementById) {
      for (const id of ids.split(/\s+/)) {
        const l = root.getElementById(id);
        if (l && l !== el) parts.push(l.innerText);
      }
    }
    for (const l of el.labels || []) parts.push(l.innerText);
    return parts.join(" | ");
  }

  function labelOf(el) {
    return lower([
      el.getAttribute && el.getAttribute("aria-label"),
      el.getAttribute && el.getAttribute("title"),
      el.getAttribute && el.getAttribute("placeholder"),
      el.getAttribute && el.getAttribute("name"),
      labelText(el),
      el.tagName === "INPUT" ? "" : el.innerText,
    ].filter(Boolean).join(" | "));
  }

  const isClickable = (el) =>
    ["BUTTON", "A"].includes(el.tagName) ||
    ["button", "link", "menuitem", "tab"].includes(lower(el.getAttribute && el.getAttribute("role")));
  const isInput = (el) =>
    (el.tagName === "INPUT" && !["hidden", "checkbox", "radio", "submit", "button"].includes(lower(el.type))) ||
    el.tagName === "TEXTAREA";

  // Best match: an exact label wins over a "contains" match; needles earlier in
  // the list win over later ones.
  function find(kind, needles) {
    const test = kind === "input" ? isInput : isClickable;
    const cands = allElements().filter((el) => test(el) && visible(el));
    for (const n of needles) {
      const exact = cands.find((el) => labelOf(el).split(" | ").some((p) => p.trim() === n));
      if (exact) return exact;
      const part = cands.find((el) => labelOf(el).includes(n));
      if (part) return part;
    }
    return null;
  }

  function click(needles) {
    const el = find("clickable", needles);
    if (!el) return false;
    el.scrollIntoView({ block: "center" });
    el.click();
    return true;
  }

  function fill(needles, value) {
    const el = find("input", needles);
    if (!el) return false;
    const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, "value").set;
    el.focus();
    setter.call(el, value); // bypass React's value tracking
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  }

  function pageText() {
    const parts = [document.body ? document.body.innerText : ""];
    for (const f of allElements().filter((e) => e.tagName === "IFRAME")) {
      try { parts.push(f.contentDocument.body.innerText); } catch (e) { /* cross-origin */ }
    }
    return lower(parts.join("\n"));
  }

  const hasText = (needles) => { const t = pageText(); return needles.some((n) => t.includes(n)); };

  // Ordered so the most decisive signals win.
  function state(sel) {
    if (hasText(sel.removedText)) return "removed";
    if (hasText(sel.endedText)) return "ended";
    if (hasText(sel.wrongPasswordText)) return "wrong_password";
    if (find("clickable", sel.leave)) return "in_meeting";
    if (hasText(sel.lobbyText)) return "lobby";
    if (find("input", sel.passwordInput)) return "password";
    if (find("input", sel.nameInput) || find("input", sel.emailInput)) return "prejoin";
    if (find("clickable", sel.joinFromBrowser)) return "landing";
    if (find("clickable", sel.joinButton)) return "preview";
    return "unknown";
  }

  window.__deoPage = { find, click, fill, hasText, state, labelOf, allElements, visible };
})();
