(function () {
  const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
  const BUCKETS_PER_DAY = 288;
  const LABEL_W = 36;
  const LABEL_H = 20;

  function render(canvas) {
    var raw = canvas.getAttribute("data-buckets");
    if (!raw) return;
    var data;
    try { data = JSON.parse(raw); } catch (_) { return; }
    if (!data || data.length !== 2016) return;

    var ctx = canvas.getContext("2d");
    var cellW = (canvas.width - LABEL_W) / BUCKETS_PER_DAY;
    var cellH = (canvas.height - LABEL_H) / 7;

    var max = 0;
    for (var i = 0; i < data.length; i++) {
      if (data[i] > max) max = data[i];
    }
    if (max === 0) max = 1;

    ctx.fillStyle = "#1a1d27";
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    for (var day = 0; day < 7; day++) {
      for (var b = 0; b < BUCKETS_PER_DAY; b++) {
        var val = data[day * BUCKETS_PER_DAY + b];
        var intensity = val / max;
        var r = Math.round(108 + 147 * intensity);
        var g = Math.round(140 - 40 * intensity);
        var bl = Math.round(255 - 155 * intensity);
        ctx.fillStyle = "rgba(" + r + "," + g + "," + bl + "," + (0.15 + 0.85 * intensity) + ")";
        ctx.fillRect(LABEL_W + b * cellW, LABEL_H + day * cellH, Math.ceil(cellW), Math.ceil(cellH));
      }
    }

    ctx.fillStyle = "#8b8fa3";
    ctx.font = "11px system-ui, sans-serif";
    ctx.textAlign = "right";
    ctx.textBaseline = "middle";
    for (var d = 0; d < 7; d++) {
      ctx.fillText(DAYS[d], LABEL_W - 4, LABEL_H + d * cellH + cellH / 2);
    }

    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    for (var h = 0; h < 24; h += 3) {
      var x = LABEL_W + h * 12 * cellW;
      ctx.fillText(h + ":00", x, 2);
    }

    canvas.onmousemove = function (e) {
      var rect = canvas.getBoundingClientRect();
      var mx = e.clientX - rect.left - LABEL_W;
      var my = e.clientY - rect.top - LABEL_H;
      if (mx < 0 || my < 0) { canvas.title = ""; return; }
      var bucket = Math.floor(mx / cellW);
      var day = Math.floor(my / cellH);
      if (day < 0 || day >= 7 || bucket < 0 || bucket >= BUCKETS_PER_DAY) { canvas.title = ""; return; }
      var hour = Math.floor(bucket / 12);
      var min = (bucket % 12) * 5;
      var val = data[day * BUCKETS_PER_DAY + bucket];
      canvas.title = DAYS[day] + " " + String(hour).padStart(2, "0") + ":" + String(min).padStart(2, "0") + " — " + val;
    };
  }

  function init(root) {
    var scope = root || document;
    var canvases = scope.querySelectorAll("canvas.heatmap[data-buckets]");
    for (var i = 0; i < canvases.length; i++) {
      render(canvases[i]);
    }
  }

  window.initHeatmaps = init;

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { init(document); });
  } else {
    init(document);
  }

  if (document.body && document.body.addEventListener) {
    document.body.addEventListener("htmx:afterSwap", function (event) {
      if (!event || !event.target || !event.target.querySelectorAll) return;
      init(event.target);
    });
  }
})();
