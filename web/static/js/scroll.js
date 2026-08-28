(function () {
  document.querySelectorAll('.land-render').forEach(function (box) {
    var d;
    try { d = JSON.parse(box.getAttribute('data-json') || '{}'); } catch (e) { return; }
    var html = '';
    if (d.text_above) html += '<div class="prose land-text">' + d.text_above + '</div>';
    html += '<div class="cta-row cta-' + (d.n || (d.cols || []).length) + ' corners-' + (d.corners || 'square') + '">';
    (d.cols || []).forEach(function (c) {
      var bg = c.bg || '', style = '';
      if (bg.indexOf('linear') === 0) style = 'background:' + bg;
      else if (bg.indexOf('/') === 0 || bg.indexOf('http') === 0) style = 'background-image:url(' + bg + ');background-size:cover';
      else if (bg) style = 'background:' + bg;
      html += '<section class="cta" style="' + style + '">';
      if (c.logo) html += '<img class="cta-logo" src="' + c.logo + '" alt="">';
      if (c.header) html += '<h2>' + c.header + '</h2>';
      if (c.text) html += '<p>' + c.text + '</p>';
      if (c.button) html += '<p><a class="btn theme-cta" href="' + (c.href || '#') + '">' + c.button + '</a></p>';
      html += '</section>';
    });
    html += '</div>';
    if (d.text_below) html += '<div class="prose land-text">' + d.text_below + '</div>';
    box.innerHTML = html;
  });
  var root = document.querySelector('.scroll-thru');
  if (!root || !window.IntersectionObserver) return;
  var base = root.getAttribute('data-base') || '';
  var obs = new IntersectionObserver(function (ents) {
    ents.forEach(function (e) {
      if (!e.isIntersecting) return;
      var slug = e.target.getAttribute('data-slug');
      if (!slug) return;
      history.replaceState({}, '', base + '/' + slug);
    });
  }, { threshold: 0.55 });
  root.querySelectorAll('.scroll-page').forEach(function (el) { obs.observe(el); });
})();
