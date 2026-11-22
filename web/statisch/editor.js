// Editor: Bausteine, Absätze verschieben, Vorschau, Strg+S speichert.
(() => {
  const feld = document.getElementById("inhalt");
  if (!feld) return;
  const form = feld.form, schau = document.getElementById("schau");
  let geaendert = false;
  const geschrieben = () => {
    geaendert = true;
    feld.dispatchEvent(new Event("input", { bubbles: true }));
  };
  const einfuegen = (text) => {
    feld.setRangeText(text, feld.selectionStart, feld.selectionEnd, "end");
    feld.focus();
    geschrieben();
  };
  document.querySelectorAll("[data-md]").forEach((b) => b.addEventListener("click", () => einfuegen(b.dataset.md)));
  document.querySelectorAll("[data-baustein]").forEach((b) => b.addEventListener("click", () => {
    const a = feld.selectionStart, e = feld.selectionEnd;
    const wahl = feld.value.slice(a, e) || b.dataset.leer;
    const luft = b.dataset.zeile && a > 0 ? (feld.value.slice(0, a).endsWith("\n\n") ? "" : "\n\n") : "";
    einfuegen(luft + b.dataset.baustein.replace("%", () => wahl).replaceAll("\\n", "\n"));
  }));

  // Alt+Pfeil verschiebt den Absatz unter der Schreibmarke.
  const schieben = (richtung) => {
    const teile = feld.value.split("\n\n");
    let i = 0, ab = 0;
    while (i < teile.length - 1 && ab + teile[i].length + 2 <= feld.selectionStart) ab += teile[i++].length + 2;
    const j = i + richtung;
    if (j < 0 || j >= teile.length) return;
    [teile[i], teile[j]] = [teile[j], teile[i]];
    feld.value = teile.join("\n\n");
    const neu = teile.slice(0, j).reduce((n, t) => n + t.length + 2, 0);
    feld.setSelectionRange(neu, neu);
    geschrieben();
  };
  feld.addEventListener("keydown", (e) => {
    if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
      e.preventDefault();
      schieben(e.key === "ArrowUp" ? -1 : 1);
    } else if (e.key === "Tab" && !e.shiftKey) {
      e.preventDefault();
      einfuegen("  ");
    } else if (e.key === "Escape") {
      feld.blur(); // sonst käme die Tastatur nicht mehr aus dem Feld
    }
  });

  let uhr, lauf = 0;
  const zeigen = async () => {
    if (!schau || schau.hidden) return;
    const nr = ++lauf;
    try {
      const r = await fetch("/admin/markdown", { method: "POST", body: new URLSearchParams({ token: form.elements.token.value, inhalt: feld.value }) });
      const html = r.ok ? await r.text() : "";
      if (nr !== lauf) return;
      // Der Server lässt kein rohes HTML durch.
      if (r.ok) schau.innerHTML = html;
      else schau.textContent = "Die Vorschau geht gerade nicht.";
    } catch {
      if (nr === lauf) schau.textContent = "Die Vorschau geht gerade nicht.";
    }
  };
  feld.addEventListener("input", () => {
    clearTimeout(uhr);
    uhr = setTimeout(zeigen, 300);
  });
  const knopf = document.getElementById("schau-knopf");
  knopf?.addEventListener("click", () => {
    schau.hidden = !schau.hidden;
    knopf.setAttribute("aria-pressed", String(!schau.hidden));
    zeigen();
  });

  form.addEventListener("input", () => { geaendert = true; });
  form.addEventListener("submit", () => { geaendert = false; });
  addEventListener("beforeunload", (e) => { if (geaendert) e.preventDefault(); });
  document.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
      e.preventDefault();
      // Veröffentlichtes bleibt veröffentlicht.
      form.querySelector("button[data-speichern]")?.click();
    }
  });
})();
