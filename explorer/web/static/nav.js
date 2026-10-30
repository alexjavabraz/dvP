// Tela de carregamento com o logo animado:
// - na primeira pagina da sessao do navegador (abertura), por pelo menos SPLASH_MS;
// - ao trocar de pagina: aparece no clique e continua na pagina nova ate ela ficar pronta.
// O script inline do <head> marca <html> com "bx-splash" ou "bx-nav" antes da pintura,
// para a animacao nao piscar entre uma pagina e outra.
(function () {
  var el = document.getElementById("nav-loader");
  if (!el) return;
  var root = document.documentElement;
  var SPLASH_MS = 1200, NAV_MS = 250, FADE_MS = 220;
  var safety = null;

  function store(k, v) {
    try { if (v === null) sessionStorage.removeItem(k); else sessionStorage.setItem(k, v); } catch (e) { /* sem storage */ }
  }
  function elapsed() { return performance && performance.now ? performance.now() : 0; }

  function hide() {
    clearTimeout(safety);
    if (el.hidden && !root.classList.contains("bx-splash") && !root.classList.contains("bx-nav")) return;
    el.classList.add("nl-out");
    setTimeout(function () {
      el.hidden = true;
      el.classList.remove("nl-out");
      root.classList.remove("bx-splash", "bx-nav");
    }, FADE_MS);
  }
  function hideAfter(minMs) { setTimeout(hide, Math.max(0, minMs - elapsed())); }

  function show() {
    store("bx-nav", "1");
    el.classList.remove("nl-out");
    el.hidden = false;
    clearTimeout(safety);
    safety = setTimeout(function () { store("bx-nav", null); hide(); }, 15000); // navegacao cancelada ou lenta demais
  }

  // chegada numa pagina
  if (root.classList.contains("bx-splash")) {
    store("bx-splash", "1");
    store("bx-nav", null);
    if (document.readyState === "complete") hideAfter(SPLASH_MS);
    else window.addEventListener("load", function () { hideAfter(SPLASH_MS); });
  } else if (root.classList.contains("bx-nav")) {
    store("bx-nav", null);
    hideAfter(NAV_MS);
  }

  document.addEventListener("click", function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest && e.target.closest("a[href]");
    if (!a || a.hasAttribute("download") || (a.target && a.target !== "_self")) return;
    var u;
    try { u = new URL(a.href, location.href); } catch (err) { return; }
    if (u.origin !== location.origin) return;
    if (u.pathname === location.pathname && u.search === location.search && u.hash) return; // ancora na mesma pagina
    show();
  });

  document.addEventListener("submit", function (e) {
    if (e.defaultPrevented) return;
    var f = e.target;
    if (f && f.target && f.target !== "_self") return;
    show();
  });

  // voltar/avancar do navegador (bfcache): a pagina volta sem recarregar
  window.addEventListener("pageshow", function (e) {
    if (e.persisted) { store("bx-nav", null); el.hidden = true; root.classList.remove("bx-splash", "bx-nav"); }
  });
})();
