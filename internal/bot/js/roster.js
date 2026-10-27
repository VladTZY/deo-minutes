// Reports who is in the meeting and who is speaking, every 500 ms, through
// the deoRoster binding. Started by Go after the bot is in the meeting:
// window.__deoRosterStart(selectorsJSON).
(() => {
  if (window.__deoRosterStart) return;

  const lower = (s) => (s || "").toString().toLowerCase();
  const firstLine = (s) => (s || "").split("\n").map((x) => x.trim()).filter(Boolean)[0] || "";
  const now = () => performance.timeOrigin + performance.now();
  const send = (obj) => { try { if (window.deoRoster) window.deoRoster(JSON.stringify(obj)); } catch (e) { /* ignore */ } };

  // Strip status words Webex appends to names ("Ion Popescu (Host, me)", "…, speaking").
  function cleanName(s, markers) {
    let n = firstLine(s);
    for (const m of markers) n = n.replace(new RegExp("[,\\-–]?\\s*" + m + ".*$", "i"), "");
    return n.replace(/\s*\((host|cohost|co-host|me|guest|presenter|eu)[^)]*\)\s*$/i, "").trim();
  }

  window.__deoRosterStart = (selJSON) => {
    const sel = JSON.parse(selJSON);
    const P = window.__deoPage;
    const names = new Set();
    let tick = 0;
    let missing = 0;

    function panel() {
      return P.allElements().find((el) =>
        sel.participantsPanel.some((n) => lower(el.getAttribute && el.getAttribute("aria-label")).includes(n)) &&
        el.querySelector && el.querySelector("[role=listitem]"));
    }

    function snapshot() {
      const pnl = panel();
      // Reopen the panel if it stays closed for 5 s (a click toggles it, so not every tick).
      if (pnl) missing = 0; else if (++missing % 10 === 0) P.click(sel.participants);
      const items = pnl ? Array.from(pnl.querySelectorAll("[role=listitem]")) : [];
      const visibleNames = [];
      const speakers = new Set();
      for (const it of items) {
        const label = P.labelOf(it);
        const n = cleanName(it.getAttribute("aria-label") || it.innerText, sel.speakingMarkers);
        if (!n) continue;
        visibleNames.push(n); names.add(n);
        const speakingEl = [it, ...it.querySelectorAll("*")].some((el) =>
          sel.speakingMarkers.some((m) => P.labelOf(el).includes(m)) ||
          sel.activeSpeakerClasses.some((c) => lower(el.className && el.className.baseVal !== undefined ? el.className.baseVal : el.className).includes(c)));
        if (speakingEl || sel.speakingMarkers.some((m) => label.includes(m))) speakers.add(n);
      }
      // Stage / video tiles marked as the active speaker.
      for (const el of P.allElements()) {
        const cls = lower(el.className && el.className.baseVal !== undefined ? el.className.baseVal : el.className);
        if (sel.activeSpeakerClasses.some((c) => cls.includes(c)) && !items.includes(el)) {
          const n = cleanName(el.getAttribute("aria-label") || el.innerText, sel.speakingMarkers);
          if (n && n.length < 80) speakers.add(n);
        }
      }
      // Virtualized lists only render what is on screen: scroll a page every 5 s.
      if (pnl && ++tick % 10 === 0) {
        const sc = [pnl, ...pnl.querySelectorAll("*")].find((e) => e.scrollHeight > e.clientHeight + 10);
        if (sc) sc.scrollTop = sc.scrollTop + sc.clientHeight >= sc.scrollHeight ? 0 : sc.scrollTop + sc.clientHeight;
      }
      send({ ts: now(), participants: Array.from(names), visible: visibleNames, speakers: Array.from(speakers), panel: !!pnl });
    }

    P.click(sel.participants);
    setInterval(snapshot, 500);
    return true;
  };
})();
