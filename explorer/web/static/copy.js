// Botao COPIAR dos ids longos (hash, txId, endereco). Funciona tambem em HTTP sem TLS,
// onde navigator.clipboard nao existe.
(function () {
  function fallback(text) {
    var ta = document.createElement("textarea");
    ta.value = text; ta.setAttribute("readonly", ""); ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta); ta.select();
    var ok = false;
    try { ok = document.execCommand("copy"); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }
  function done(btn, ok) {
    btn.textContent = ok ? "Copiado" : "Não foi possível copiar";
    setTimeout(function () { btn.textContent = "Copiar"; }, 1600);
  }
  document.addEventListener("click", function (e) {
    var btn = e.target.closest && e.target.closest("button.copy[data-copy]");
    if (!btn) return;
    var text = btn.getAttribute("data-copy");
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(function () { done(btn, true); }, function () { done(btn, fallback(text)); });
    } else {
      done(btn, fallback(text));
    }
  });
})();
