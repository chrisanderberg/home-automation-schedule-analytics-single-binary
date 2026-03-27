(function () {
  const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
  const BUCKETS_PER_DAY = 288;
  const TOTAL_BUCKETS = 2016;
  const PADDING = { top: 18, right: 12, bottom: 28, left: 12 };
  const BG = "#1a1d27";
  const GRID = "rgba(139,143,163,0.18)";
  const TEXT = "#8b8fa3";
  const BAR = "#63a0ff";

  function formatValue(value, format) {
    if (format === "durationMillis") {
      const totalMinutes = Math.round(value / 60000);
      const hours = Math.floor(totalMinutes / 60);
      const minutes = totalMinutes % 60;
      return hours + "h " + String(minutes).padStart(2, "0") + "m";
    }
    if (format === "share") {
      return (value * 100).toFixed(1) + "%";
    }
    return Math.round(value).toString();
  }

  function bucketLabel(index) {
    const day = Math.floor(index / BUCKETS_PER_DAY);
    const bucket = index % BUCKETS_PER_DAY;
    const hour = Math.floor(bucket / 12);
    const minute = (bucket % 12) * 5;
    return DAYS[day] + " " + String(hour).padStart(2, "0") + ":" + String(minute).padStart(2, "0");
  }

  function parsePayload(canvas) {
    const raw = canvas.getAttribute("data-chart-payload");
    if (!raw) return null;
    try {
      return JSON.parse(raw);
    } catch (_) {
      return null;
    }
  }

  function setupCanvas(canvas) {
    const ratio = window.devicePixelRatio || 1;
    const width = canvas.width;
    const height = canvas.height;
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";
    canvas.width = Math.floor(width * ratio);
    canvas.height = Math.floor(height * ratio);
    const ctx = canvas.getContext("2d");
    ctx.scale(ratio, ratio);
    return { ctx, width, height };
  }

  function drawFrame(ctx, width, height) {
    ctx.fillStyle = BG;
    ctx.fillRect(0, 0, width, height);
    ctx.strokeStyle = GRID;
    ctx.lineWidth = 1;
    for (let day = 0; day <= 7; day++) {
      const x = PADDING.left + ((width - PADDING.left - PADDING.right) * day) / 7;
      ctx.beginPath();
      ctx.moveTo(x, PADDING.top);
      ctx.lineTo(x, height - PADDING.bottom);
      ctx.stroke();
    }
    ctx.fillStyle = TEXT;
    ctx.font = "11px system-ui, sans-serif";
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    for (let day = 0; day < 7; day++) {
      const x = PADDING.left + ((width - PADDING.left - PADDING.right) * (day + 0.5)) / 7;
      ctx.fillText(DAYS[day], x, height - PADDING.bottom + 8);
    }
  }

  function drawBars(ctx, width, height, payload) {
    const series = payload && payload.series;
    if (!series || series.length !== TOTAL_BUCKETS) return null;
    let max = 0;
    for (let i = 0; i < series.length; i++) {
      if (series[i] > max) max = series[i];
    }
    if (max <= 0) max = 1;

    drawFrame(ctx, width, height);
    const plotW = width - PADDING.left - PADDING.right;
    const plotH = height - PADDING.top - PADDING.bottom;
    const barW = plotW / TOTAL_BUCKETS;

    ctx.fillStyle = BAR;
    for (let i = 0; i < series.length; i++) {
      const value = series[i];
      if (value <= 0) continue;
      const barH = Math.max(1, (value / max) * plotH);
      const x = PADDING.left + i * barW;
      const y = height - PADDING.bottom - barH;
      ctx.fillRect(x, y, Math.ceil(barW), barH);
    }
    return { type: "bars", payload: payload, plotW: plotW, plotH: plotH };
  }

  function drawStacked(ctx, width, height, payload) {
    const stacks = payload && payload.stacks;
    if (!stacks || stacks.length === 0) return null;
    drawFrame(ctx, width, height);
    const plotW = width - PADDING.left - PADDING.right;
    const plotH = height - PADDING.top - PADDING.bottom;
    const barW = plotW / TOTAL_BUCKETS;

    for (let bucket = 0; bucket < TOTAL_BUCKETS; bucket++) {
      let y = height - PADDING.bottom;
      for (let s = 0; s < stacks.length; s++) {
        const values = stacks[s].values || [];
        const value = values[bucket] || 0;
        if (value <= 0) continue;
        const segmentH = Math.max(1, value * plotH);
        y -= segmentH;
        ctx.fillStyle = stacks[s].color || BAR;
        ctx.fillRect(PADDING.left + bucket * barW, y, Math.ceil(barW), segmentH);
      }
    }
    return { type: "stacked", payload: payload, plotW: plotW, plotH: plotH };
  }

  function attachTooltip(canvas, rendered, width) {
    if (!rendered) return;
    canvas.onmousemove = function (event) {
      const rect = canvas.getBoundingClientRect();
      const x = event.clientX - rect.left - PADDING.left;
      if (x < 0 || x > width - PADDING.left - PADDING.right) {
        canvas.title = "";
        return;
      }
      const bucket = Math.max(0, Math.min(TOTAL_BUCKETS - 1, Math.floor((x / (width - PADDING.left - PADDING.right)) * TOTAL_BUCKETS)));
      if (rendered.type === "bars") {
        const value = rendered.payload.series[bucket] || 0;
        canvas.title = bucketLabel(bucket) + " | " + formatValue(value, rendered.payload.valueFormat);
        return;
      }
      const stacks = rendered.payload.stacks || [];
      const parts = [];
      for (let i = 0; i < stacks.length; i++) {
        const value = (stacks[i].values || [])[bucket] || 0;
        if (value <= 0) continue;
        parts.push(stacks[i].label + ": " + formatValue(value, rendered.payload.valueFormat));
      }
      canvas.title = bucketLabel(bucket) + " | " + parts.join(", ");
    };
  }

  function render(canvas) {
    const kind = canvas.getAttribute("data-chart-kind");
    const payload = parsePayload(canvas);
    if (!kind || !payload) return;
    const setup = setupCanvas(canvas);
    let rendered = null;
    if (kind === "bars") {
      rendered = drawBars(setup.ctx, setup.width, setup.height, payload);
    } else if (kind === "stacked") {
      rendered = drawStacked(setup.ctx, setup.width, setup.height, payload);
    }
    attachTooltip(canvas, rendered, setup.width);
  }

  function init(root) {
    const scope = root || document;
    const canvases = scope.querySelectorAll("canvas.weekly-chart[data-chart-kind]");
    for (let i = 0; i < canvases.length; i++) {
      render(canvases[i]);
    }
  }

  window.initWeeklyCharts = init;

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
