// Atualizacao ao vivo, a cada bloco novo (sem recarregar a pagina):
// - o servidor avisa por /api/events (Server-Sent Events) quando um bloco e indexado;
// - a pagina busca a propria URL e aplica so o que mudou:
//     [data-live="rows"]  tabelas: linhas novas entram no topo com destaque, as antigas saem
//                         (cada <tr> tem data-key); linhas iguais nao sao tocadas;
//     [data-live="swap"]  contadores, paginacao, dados do endereco: troca o conteudo se mudou;
// - em paginas 2 em diante as tabelas nao mudam sob o leitor: aparece um aviso com link;
// - os "ha N s" sao calculados aqui pelo relogio do servidor, a cada segundo;
// - a pagina Pendentes (mempool) e consultada por tempo (data-live-mode="poll").
(function () {
  var body = document.body;
  var interval = parseInt(body.getAttribute("data-live") || "0", 10) * 1000;
  var mode = body.getAttribute("data-live-mode") || "block";
  var btn = document.getElementById("live-toggle");

  // ---------------------------------------------------------------- "ha N s"
  var offset = (parseInt(body.getAttribute("data-now") || "0", 10) * 1000 || Date.now()) - Date.now();
  function serverSec() { return Math.floor((Date.now() + offset) / 1000); }
  function agoText(ts) {
    var d = serverSec() - ts;
    if (d < 0) return "agora";
    if (d < 60) return "há " + d + "s";
    if (d < 3600) return "há " + Math.floor(d / 60) + " min";
    if (d < 48 * 3600) return "há " + Math.floor(d / 3600) + " h";
    return "há " + Math.floor(d / 86400) + " dias";
  }
  function paintAgo(root) {
    var els = root.querySelectorAll("[data-ts]");
    for (var i = 0; i < els.length; i++) {
      var t = agoText(parseInt(els[i].getAttribute("data-ts"), 10));
      if (els[i].textContent !== t) els[i].textContent = t;
    }
  }
  paintAgo(document);
  setInterval(function () { paintAgo(document); }, 1000);

  if (!interval || !btn) return;

  // ---------------------------------------------------------------- ligar / pausar
  var KEY = "besu-explorer-live";
  var sessionOverride = null;
  function enabled() { try { return localStorage.getItem(KEY) !== "off"; } catch (e) { return true; } }
  function isOn() { return sessionOverride === null ? enabled() : sessionOverride; }
  function setEnabled(v) {
    try { localStorage.setItem(KEY, v ? "on" : "off"); } catch (e) { /* sem storage: so nesta aba */ }
    sessionOverride = v;
  }
  function paint() {
    var on = isOn();
    btn.classList.toggle("on", on);
    btn.setAttribute("aria-pressed", on ? "true" : "false");
    btn.querySelector(".lbl").textContent = on ? "Ao vivo" : "Pausado";
    btn.title = on ? (mode === "poll" ? "Atualizando a cada " + interval / 1000 + " s" : "Atualizando a cada bloco novo") + " (clique para pausar)"
                   : "Atualização pausada (clique para retomar)";
  }

  var page = parseInt(new URLSearchParams(location.search).get("page") || "1", 10);
  var frozen = page > 1; // paginas antigas: avisa em vez de mexer nas linhas

  // ---------------------------------------------------------------- aplicar o que mudou
  function selectionIn(el) {
    var sel = window.getSelection ? window.getSelection() : null;
    return !!(sel && String(sel).length && sel.anchorNode && el.contains(sel.anchorNode));
  }
  function liveIds(root) {
    var out = [], els = root.querySelectorAll("[data-live][id]");
    for (var i = 0; i < els.length; i++) out.push(els[i].id);
    return out.sort().join(",");
  }
  function sameRow(a, b) {
    var flagged = a.classList.contains("is-new");
    if (flagged) { a.classList.remove("is-new"); if (!a.className) a.removeAttribute("class"); }
    var same = a.outerHTML === b.outerHTML;
    if (flagged) a.classList.add("is-new");
    return same;
  }
  function mergeRows(cur, next) {
    var byKey = {}, hadKeys = false, i, child;
    for (i = 0; i < cur.children.length; i++) {
      child = cur.children[i];
      var k = child.getAttribute("data-key");
      if (k) { byKey[k] = child; hadKeys = true; }
    }
    var list = [], fresh = [];
    for (i = 0; i < next.children.length; i++) {
      var row = next.children[i], key = row.getAttribute("data-key"), old = key && byKey[key];
      if (old) {
        delete byKey[key];
        list.push(sameRow(old, row) ? old : document.importNode(row, true));
      } else {
        var n = document.importNode(row, true);
        if (key && hadKeys) { n.classList.add("is-new"); fresh.push(n); }
        list.push(n);
      }
    }
    cur.replaceChildren.apply(cur, list);
    if (fresh.length) setTimeout(function () {
      fresh.forEach(function (n) { n.classList.remove("is-new"); if (!n.className) n.removeAttribute("class"); });
    }, 3200);
  }
  function countNew(cur, next) {
    var seen = {}, n = 0, i;
    for (i = 0; i < cur.children.length; i++) seen[cur.children[i].getAttribute("data-key")] = 1;
    for (i = 0; i < next.children.length; i++) {
      var k = next.children[i].getAttribute("data-key");
      if (k && !seen[k]) n++;
    }
    return n;
  }
  function showNotice() {
    if (document.getElementById("live-notice")) return;
    var u = new URL(location.href);
    u.searchParams.delete("page");
    var el = document.createElement("div");
    el.id = "live-notice";
    el.className = "live-notice";
    el.innerHTML = "<span>Há registros novos desde que esta página foi aberta.</span> <a></a>";
    el.querySelector("a").href = u.pathname + u.search;
    el.querySelector("a").textContent = "Ver os mais recentes";
    var main = document.querySelector("main"), head = main.querySelector(".pagehead");
    main.insertBefore(el, head ? head.nextSibling : main.firstChild);
  }
  function swapHTML(cur, next) {
    if (cur && next && cur.innerHTML !== next.innerHTML && !selectionIn(cur)) cur.innerHTML = next.innerHTML;
  }

  function apply(doc) {
    swapHTML(document.getElementById("banners"), doc.getElementById("banners"));
    swapHTML(document.getElementById("lv-foot"), doc.getElementById("lv-foot"));
    var main = document.querySelector("main"), nextMain = doc.querySelector("main");
    if (!main || !nextMain) return;
    // a estrutura mudou (ex.: uma secao apareceu): troca o conteudo todo, se o usuario nao estiver digitando
    if (liveIds(main) !== liveIds(nextMain)) {
      var a = document.activeElement;
      if (!(a && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName)) && !selectionIn(main)) main.innerHTML = nextMain.innerHTML;
      return;
    }
    var regions = main.querySelectorAll("[data-live][id]");
    for (var i = 0; i < regions.length; i++) {
      var cur = regions[i], next = doc.getElementById(cur.id);
      if (!next) continue;
      if (cur.getAttribute("data-live") === "rows") {
        if (selectionIn(cur)) continue;
        if (frozen) { if (countNew(cur, next)) showNotice(); }
        else mergeRows(cur, next);
      } else {
        swapHTML(cur, next);
      }
    }
    if (doc.title && doc.title !== document.title) document.title = doc.title;
  }

  // ---------------------------------------------------------------- buscar
  var busy = false, again = false, lastRun = 0, MIN_GAP = 800;
  function refresh() {
    if (!isOn() || document.hidden) { again = true; return; }
    if (busy) { again = true; return; }
    var wait = lastRun + MIN_GAP - Date.now();
    if (wait > 0) { busy = true; setTimeout(function () { busy = false; refresh(); }, wait); return; }
    busy = true; again = false; lastRun = Date.now();
    fetch(location.href, { cache: "no-store", headers: { "X-Live-Refresh": "1" } })
      .then(function (r) {
        if (r.status === 401) { location.reload(); throw new Error("sessao expirada"); } // volta para o login
        if (!r.ok && r.status !== 404) throw new Error("http " + r.status);
        return r.text();
      })
      .then(function (html) {
        var doc = new DOMParser().parseFromString(html, "text/html");
        paintAgo(doc); // mesmo relogio dos dois lados: linhas iguais continuam iguais
        apply(doc);
        btn.classList.remove("err");
        paint();
      })
      .catch(function () { btn.classList.add("err"); btn.title = "Sem resposta do explorer; tentando de novo"; })
      .then(function () { busy = false; if (again) setTimeout(refresh, 0); });
  }

  // ---------------------------------------------------------------- avisos de bloco novo
  var es = null, lastIndexed = null, pollTimer = null, failures = 0;
  function startPolling() { if (!pollTimer) pollTimer = setInterval(refresh, interval); }
  function stopPolling() { clearInterval(pollTimer); pollTimer = null; }
  function disconnect() { if (es) { es.close(); es = null; } }
  function connect() {
    if (es || !isOn()) return;
    if (mode === "poll" || !window.EventSource) { startPolling(); return; }
    es = new EventSource("/api/events");
    es.addEventListener("block", function (e) {
      failures = 0;
      stopPolling();
      btn.classList.remove("err");
      var d;
      try { d = JSON.parse(e.data); } catch (err) { return; }
      if (d.now) offset = d.now * 1000 - Date.now();
      var key = d.indexed + ":" + d.synced;
      if (key !== lastIndexed) { lastIndexed = key; refresh(); }
    });
    es.onerror = function () {
      btn.classList.add("err");
      failures++;
      // sessao encerrada ou expirada: o aviso responde 401; confere e manda para o login
      fetch("/api/status", { cache: "no-store" }).then(function (r) { if (r.status === 401) location.reload(); }).catch(function () {});
      if (es.readyState === EventSource.CLOSED || failures >= 3) {
        disconnect();
        startPolling(); // enquanto o aviso nao volta, consulta por tempo
        setTimeout(connect, 15000);
      }
    };
  }

  btn.hidden = false;
  btn.addEventListener("click", function () {
    setEnabled(!isOn());
    paint();
    if (isOn()) { connect(); refresh(); } else { disconnect(); stopPolling(); }
  });
  document.addEventListener("visibilitychange", function () { if (!document.hidden && again) refresh(); });
  paint();
  connect();
})();
