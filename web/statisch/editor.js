// Medien per Klick an der Schreibmarke einfügen; Tab rückt ein; Strg+S speichert als Entwurf.
(() => {
  const feld = document.getElementById("inhalt");
  if (!feld) return;
  document.querySelectorAll("[data-md]").forEach((b) => b.addEventListener("click", () => {
    const a = feld.selectionStart, e = feld.selectionEnd;
    feld.value = feld.value.slice(0, a) + b.dataset.md + feld.value.slice(e);
    feld.focus();
    feld.selectionStart = feld.selectionEnd = a + b.dataset.md.length;
  }));
  feld.addEventListener("keydown", (e) => {
    if (e.key === "Tab" && !e.shiftKey) {
      e.preventDefault();
      const a = feld.selectionStart;
      feld.setRangeText("  ", a, feld.selectionEnd, "end");
    }
  });
  document.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
      e.preventDefault();
      document.querySelector('button[name="status"][value="entwurf"]')?.click();
    }
  });
})();
