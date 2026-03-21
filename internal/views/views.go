package views

import (
	"context"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"home-automation-schedule-analytics-single-bin/internal/domain/control"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

type HomePageData struct {
	Controls []storage.ControlSummary
}

type HeatmapData struct {
	ControlID      string
	QuarterOptions []int
	QuarterIndex   int
	Clock          string
	Metric         string
	Values         []uint64
	HasData        bool
}

type ControlPageData struct {
	Control control.Control
	Heatmap HeatmapData
}

type SnapshotsPageData struct {
	Snapshots []storage.SnapshotRecord
}

func Layout(title string, content templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">"); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "<title>"+html.EscapeString(title)+"</title>"); err != nil {
			return err
		}
		head := `<link rel="stylesheet" href="/static/app.css">
<script src="/static/vendor/htmx.min.js"></script>
<script defer src="/static/app.js"></script>`
		if _, err := io.WriteString(w, head); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "</head><body><div class=\"shell\"><header class=\"topbar\"><a class=\"brand\" href=\"/\">Schedule Analytics</a><nav><a href=\"/\">Controls</a><a href=\"/snapshots\">Snapshots</a></nav></header><main>"); err != nil {
			return err
		}
		if content != nil {
			if err := content.Render(ctx, w); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "</main></div></body></html>")
		return err
	})
}

func HomePage(data HomePageData) templ.Component {
	return Layout("Home Automation Schedule Analytics", templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "<section class=\"page-header\"><h1>Controls</h1><p>Registered controls and available aggregated quarters.</p></section>"); err != nil {
			return err
		}
		if len(data.Controls) == 0 {
			_, err := io.WriteString(w, "<section class=\"card\"><p>No controls have been registered yet.</p></section>")
			return err
		}
		if _, err := io.WriteString(w, "<section class=\"card\"><table class=\"data-table\"><thead><tr><th>Control</th><th>Type</th><th>States</th><th>Quarters</th><th>Updated</th></tr></thead><tbody>"); err != nil {
			return err
		}
		for _, item := range data.Controls {
			line := fmt.Sprintf(
				"<tr><td><a href=\"/controls/%s\">%s</a></td><td>%s</td><td>%d</td><td>%d</td><td>%s</td></tr>",
				html.EscapeString(item.Control.ID),
				html.EscapeString(item.Control.ID),
				html.EscapeString(string(item.Control.Type)),
				item.Control.NumStates,
				item.QuarterCount,
				html.EscapeString(time.UnixMilli(item.LastUpdatedMs).UTC().Format(time.RFC3339)),
			)
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "</tbody></table></section>")
		return err
	}))
}

func ControlPage(data ControlPageData) templ.Component {
	return Layout("Control "+data.Control.ID, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		header := fmt.Sprintf("<section class=\"page-header\"><h1>%s</h1><p>%s control with %d states.</p></section>",
			html.EscapeString(data.Control.ID),
			html.EscapeString(string(data.Control.Type)),
			data.Control.NumStates,
		)
		if _, err := io.WriteString(w, header); err != nil {
			return err
		}
		form := fmt.Sprintf(
			"<section class=\"card\"><form class=\"filters\" hx-get=\"/controls/%s/heatmap\" hx-target=\"#heatmap-panel\" hx-trigger=\"change from:select\"><label>Quarter<select name=\"quarter\">%s</select></label><label>Clock<select name=\"clock\">%s</select></label><label>Metric<select name=\"metric\">%s</select></label></form><div id=\"heatmap-panel\">",
			html.EscapeString(data.Control.ID),
			quarterOptions(data.Heatmap.QuarterOptions, data.Heatmap.QuarterIndex),
			selectOptions([]string{"utc", "local", "mean_solar", "apparent_solar", "unequal_hours"}, data.Heatmap.Clock),
			selectOptions([]string{"holding", "transition"}, data.Heatmap.Metric),
		)
		if _, err := io.WriteString(w, form); err != nil {
			return err
		}
		if err := HeatmapPanel(data.Heatmap).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, "</div></section>")
		return err
	}))
}

func HeatmapPanel(data HeatmapData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if !data.HasData {
			_, err := io.WriteString(w, "<div class=\"empty-state\"><p>No aggregate data is available for this control yet.</p></div>")
			return err
		}
		card := fmt.Sprintf(
			"<div class=\"heatmap-card\"><div class=\"heatmap-meta\"><span>Quarter %s</span><span>Clock %s</span><span>Metric %s</span></div><canvas class=\"heatmap-canvas\" width=\"1152\" height=\"196\" data-heatmap-values=\"%s\" data-heatmap-metric=\"%s\" data-heatmap-quarter=\"%d\"></canvas></div>",
			html.EscapeString(quarterLabel(data.QuarterIndex)),
			html.EscapeString(strings.ReplaceAll(data.Clock, "_", " ")),
			html.EscapeString(data.Metric),
			html.EscapeString(valuesCSV(data.Values)),
			html.EscapeString(data.Metric),
			data.QuarterIndex,
		)
		_, err := io.WriteString(w, card)
		return err
	})
}

func SnapshotsPage(data SnapshotsPageData) templ.Component {
	return Layout("Snapshots", templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "<section class=\"page-header\"><h1>Snapshots</h1><p>Export current controls and aggregate blobs into standalone SQLite snapshots.</p></section>"); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "<section class=\"card\"><form class=\"snapshot-form\" method=\"post\" action=\"/snapshots\" hx-post=\"/snapshots\" hx-target=\"#snapshot-list\"><label>Name<input type=\"text\" name=\"name\" required placeholder=\"Quarterly export\"></label><button type=\"submit\">Create Snapshot</button></form></section><section id=\"snapshot-list\">"); err != nil {
			return err
		}
		if err := SnapshotList(data.Snapshots).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, "</section>")
		return err
	}))
}

func SnapshotList(items []storage.SnapshotRecord) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(items) == 0 {
			_, err := io.WriteString(w, "<section class=\"card\"><p>No snapshots have been created yet.</p></section>")
			return err
		}
		if _, err := io.WriteString(w, "<section class=\"card\"><table class=\"data-table\"><thead><tr><th>Name</th><th>Created</th><th>Path</th></tr></thead><tbody>"); err != nil {
			return err
		}
		for _, item := range items {
			line := fmt.Sprintf(
				"<tr><td>%s</td><td>%s</td><td><code>%s</code></td></tr>",
				html.EscapeString(item.Name),
				html.EscapeString(time.UnixMilli(item.CreatedAtMs).UTC().Format(time.RFC3339)),
				html.EscapeString(item.Path),
			)
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "</tbody></table></section>")
		return err
	})
}

func selectOptions(options []string, selected string) string {
	var b strings.Builder
	for _, option := range options {
		selectedAttr := ""
		if option == selected {
			selectedAttr = " selected"
		}
		label := strings.ReplaceAll(option, "_", " ")
		fmt.Fprintf(&b, "<option value=\"%s\"%s>%s</option>", html.EscapeString(option), selectedAttr, html.EscapeString(label))
	}
	return b.String()
}

func quarterOptions(options []int, selected int) string {
	var b strings.Builder
	for _, option := range options {
		selectedAttr := ""
		if option == selected {
			selectedAttr = " selected"
		}
		fmt.Fprintf(&b, "<option value=\"%d\"%s>%s</option>", option, selectedAttr, html.EscapeString(quarterLabel(option)))
	}
	return b.String()
}

func quarterLabel(index int) string {
	year := 1970 + index/4
	quarter := index%4 + 1
	return fmt.Sprintf("%d Q%d", year, quarter)
}

func valuesCSV(values []uint64) string {
	var b strings.Builder
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatUint(value, 10))
	}
	return b.String()
}
