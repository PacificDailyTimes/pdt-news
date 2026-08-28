(function () {
  var wys = document.getElementById("wys");
  var field = document.getElementById("contentField");
  var form = document.getElementById("editform");
  if (!wys || !field) return;
  document.getElementById("edbar").addEventListener("click", function (e) {
    var b = e.target.closest("button");
    if (!b) return;
    e.preventDefault();
    wys.focus();
    if (b.dataset.cmd === "bold") document.execCommand("bold");
    if (b.dataset.cmd === "italic") document.execCommand("italic");
    if (b.dataset.cmd === "color") {
      document.execCommand("foreColor", false, document.getElementById("ink").value);
    }
    if (b.dataset.class) {
      var sel = window.getSelection();
      if (!sel.rangeCount) return;
      var span = document.createElement("span");
      span.className = b.dataset.class;
      span.appendChild(sel.getRangeAt(0).extractContents());
      sel.getRangeAt(0).insertNode(span);
    }
    if (b.dataset.align) {
      var img = wys.querySelector("img");
      if (img) img.className = b.dataset.align;
    }
  });
  form.addEventListener("submit", function () { field.value = wys.innerHTML; });
})();
