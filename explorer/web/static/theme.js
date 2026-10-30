// Tema claro/escuro: segue o sistema ate o usuario escolher; a escolha fica salva no navegador.
(function () {
  var root = document.documentElement;
  var btn = document.getElementById("theme-toggle");
  var KEY = "besu-explorer-theme";
  var mq = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  var choice = null; // "light" | "dark" | null (segue o sistema)
  try { var t = localStorage.getItem(KEY); if (t === "light" || t === "dark") choice = t; } catch (e) { /* sem storage */ }

  function current() { return choice || (mq && mq.matches ? "dark" : "light"); }
  function apply() {
    var t = current();
    root.setAttribute("data-theme", t);
    if (btn) btn.title = t === "dark" ? "Mudar para o tema claro" : "Mudar para o tema escuro";
  }

  apply();
  if (mq && mq.addEventListener) mq.addEventListener("change", function () { if (!choice) apply(); });
  if (!btn) return;
  btn.hidden = false;
  btn.addEventListener("click", function () {
    choice = current() === "dark" ? "light" : "dark";
    try { localStorage.setItem(KEY, choice); } catch (e) { /* sem storage: vale so nesta pagina */ }
    apply();
  });
})();
