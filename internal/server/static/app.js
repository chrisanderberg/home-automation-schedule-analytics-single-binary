function renderHeatmaps(root) {
  const canvases = (root || document).querySelectorAll("canvas[data-heatmap-values]");
  canvases.forEach((canvas) => {
    const raw = canvas.dataset.heatmapValues || "";
    const values = raw.length === 0 ? [] : raw.split(",").map((value) => Number(value));
    const ctx = canvas.getContext("2d");
    // 288 five-minute buckets per day across 7 days in the week view.
    const width = 288;
    const height = 7;
    const cellWidth = canvas.width / width;
    const cellHeight = canvas.height / height;
    const max = values.reduce((acc, value) => Math.max(acc, value), 0);
    const expectedSize = width * height;

    if (values.length !== expectedSize) {
      console.warn("heatmap data length mismatch", { actual: values.length, expected: expectedSize });
      return;
    }

    ctx.clearRect(0, 0, canvas.width, canvas.height);
    values.forEach((value, index) => {
      const x = index % width;
      const y = Math.floor(index / width);
      const intensity = max === 0 ? 0 : value / max;
      const hue = 18;
      const lightness = 96 - intensity * 58;
      ctx.fillStyle = `hsl(${hue} 62% ${lightness}%)`;
      ctx.fillRect(x * cellWidth, y * cellHeight, Math.ceil(cellWidth), Math.ceil(cellHeight));
    });
  });
}

document.addEventListener("DOMContentLoaded", () => renderHeatmaps(document));
document.body.addEventListener("htmx:afterSwap", (event) => renderHeatmaps(event.target));
