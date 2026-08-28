(function () {
  var type = document.getElementById('ptype');
  var postish = document.getElementById('postish');
  var landish = document.getElementById('landish');
  var cols = document.getElementById('cols');
  var n = document.getElementById('ncols');
  var field = document.getElementById('landingField');
  var form = document.getElementById('editform');
  if (!type) return;
  function show() {
    var land = type.value === 'landing';
    if (postish) postish.hidden = land;
    if (landish) landish.hidden = !land;
    if (land && cols && cols.childElementCount === 0) load(parseInt(n.value, 10) || 3);
  }
  function load(count) {
    var x = new XMLHttpRequest();
    x.open('GET', '/dash/ajax/landing?n=' + count);
    x.onload = function () {
      cols.innerHTML = x.responseText;
      fill();
    };
    x.send();
  }
  function fill() {
    if (!field || !field.value) return;
    try {
      var d = JSON.parse(field.value);
      if (d.text_above) document.getElementById('text_above').value = d.text_above;
      if (d.text_below) document.getElementById('text_below').value = d.text_below;
      if (d.corners) document.getElementById('lcorners').value = d.corners;
      if (d.n && n) n.value = d.n;
      (d.cols || []).forEach(function (c, i) {
        set('col_header_' + i, c.header);
        set('col_button_' + i, c.button);
        set('col_href_' + i, c.href);
        set('col_logo_' + i, c.logo);
        set('col_bg_' + i, c.bg);
        set('col_text_' + i, c.text);
        set('col_cta_' + i, c.cta);
      });
    } catch (e) {}
  }
  function set(name, v) {
    var el = form.querySelector('[name="' + name + '"]');
    if (el && v) el.value = v;
  }
  function dump() {
    var count = parseInt(n.value, 10) || 1;
    var colsArr = [];
    for (var i = 0; i < count; i++) {
      colsArr.push({
        header: val('col_header_' + i),
        button: val('col_button_' + i),
        href: val('col_href_' + i),
        logo: val('col_logo_' + i),
        bg: val('col_bg_' + i),
        text: val('col_text_' + i),
        cta: val('col_cta_' + i)
      });
    }
    field.value = JSON.stringify({
      n: count,
      corners: document.getElementById('lcorners').value,
      text_above: document.getElementById('text_above').value,
      text_below: document.getElementById('text_below').value,
      cols: colsArr
    });
  }
  function val(name) {
    var el = form.querySelector('[name="' + name + '"]');
    return el ? el.value : '';
  }
  type.addEventListener('change', show);
  if (n) n.addEventListener('change', function () { load(parseInt(n.value, 10) || 1); });
  if (form) form.addEventListener('submit', dump);
  show();
})();
