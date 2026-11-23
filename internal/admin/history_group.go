package admin

import (
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

func historyLines(entries []history.Entry, query template.URL, readOnly bool) []historyLine {
	lines := make([]historyLine, len(entries))
	for i, entry := range entries {
		lines[i] = historyLine{
			historyRow:  historyRowFrom(entry),
			FilterQuery: query,
			ReadOnly:    readOnly,
			GroupKey:    historyGroupKey(entry),
		}
	}
	return lines
}

// historyGroupKey is method, resolved upstream id, and the app path.
// The query string is not part of the key. An empty upstream id stays empty.
func historyGroupKey(entry history.Entry) string {
	return url.QueryEscape(entry.Method) + " " + url.QueryEscape(entry.UpstreamID) + " " + url.QueryEscape(entry.Path)
}

// groupHistory buckets entries that share a key. Groups and the rows inside
// them are ordered by newest timestamp first. Equal timestamps keep the input order.
func groupHistory(entries []history.Entry, lines []historyLine) []historyGroup {
	if len(entries) != len(lines) {
		return nil
	}
	type pair struct {
		entry history.Entry
		line  historyLine
	}
	order := make([]string, 0)
	buckets := map[string][]pair{}
	for i, entry := range entries {
		key := historyGroupKey(entry)
		if _, ok := buckets[key]; !ok {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], pair{entry: entry, line: lines[i]})
	}
	groups := make([]historyGroup, 0, len(order))
	for _, key := range order {
		pairs := buckets[key]
		sort.SliceStable(pairs, func(i, j int) bool {
			return pairs[i].entry.StartedAt.After(pairs[j].entry.StartedAt)
		})
		rows := make([]historyLine, len(pairs))
		statuses := make([]int, len(pairs))
		sources := make([]string, len(pairs))
		for i, item := range pairs {
			rows[i] = item.line
			statuses[i] = item.entry.Status
			sources[i] = item.line.Source
		}
		first := pairs[0]
		summary := statusSummary(statuses)
		sourceSummary := distinctLabels(sources)
		groups = append(groups, historyGroup{
			Key:           key,
			Method:        first.entry.Method,
			Path:          first.entry.Path,
			Upstream:      first.entry.UpstreamLabel(),
			When:          first.line.When,
			CountLabel:    requestCountLabel(len(rows)),
			StatusSummary: summary,
			StatusLines:   splitSummary(summary),
			Sources:       sourceSummary,
			SourceLines:   splitSummary(sourceSummary),
			Delay:         sharedText(delayValues(rows)),
			Rule:          sharedText(ruleValues(rows)),
			Rows:          rows,
			newest:        first.entry.StartedAt,
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].newest.After(groups[j].newest)
	})
	return groups
}

func markGroupOpen(groups []historyGroup, open []string, source, upstream, text, sessionID string, grouped bool) {
	selected := map[string]bool{}
	for _, key := range open {
		selected[key] = true
	}
	for i := range groups {
		groups[i].Open = selected[groups[i].Key]
		groups[i].ToggleQuery = template.URL(historyQueryString(source, upstream, text, sessionID, grouped, toggleOpenKeys(open, groups[i].Key, groups[i].Open)))
		// A group of one renders as a plain row, so it must stay visible.
		if len(groups[i].Rows) < 2 {
			continue
		}
		for j := range groups[i].Rows {
			groups[i].Rows[j].Child = true
			groups[i].Rows[j].Hidden = !groups[i].Open
		}
	}
}

func toggleOpenKeys(open []string, key string, currentlyOpen bool) []string {
	next := make([]string, 0, len(open)+1)
	if currentlyOpen {
		for _, item := range open {
			if item != key {
				next = append(next, item)
			}
		}
		return next
	}
	next = append(next, open...)
	for _, item := range open {
		if item == key {
			return next
		}
	}
	return append(next, key)
}

func splitSummary(summary string) []string {
	if summary == "" {
		return nil
	}
	return strings.Split(summary, ", ")
}

func delayValues(rows []historyLine) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.Delay
	}
	return out
}

func ruleValues(rows []historyLine) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ruleCell(row)
	}
	return out
}

func ruleCell(row historyLine) string {
	if row.RuleID == "" {
		return "none"
	}
	name := row.RuleName
	if name == "" {
		name = "unnamed"
	}
	return name + " (" + row.RuleID + ")"
}

func sharedText(values []string) string {
	if len(values) == 0 {
		return ""
	}
	first := values[0]
	for _, value := range values[1:] {
		if value != first {
			return ""
		}
	}
	if first == "none" {
		return ""
	}
	return first
}

func requestCountLabel(n int) string {
	if n == 1 {
		return "1 request"
	}
	return fmt.Sprintf("%d requests", n)
}

func statusSummary(statuses []int) string {
	counts := map[int]int{}
	order := make([]int, 0)
	for _, status := range statuses {
		if _, ok := counts[status]; !ok {
			order = append(order, status)
		}
		counts[status]++
	}
	sort.SliceStable(order, func(i, j int) bool {
		if counts[order[i]] != counts[order[j]] {
			return counts[order[i]] > counts[order[j]]
		}
		return order[i] < order[j]
	})
	parts := make([]string, len(order))
	for i, status := range order {
		parts[i] = fmt.Sprintf("%d ×%d", status, counts[status])
	}
	return strings.Join(parts, ", ")
}

func distinctLabels(labels []string) string {
	seen := map[string]bool{}
	parts := make([]string, 0)
	for _, label := range labels {
		if seen[label] {
			continue
		}
		seen[label] = true
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}

func historySummary(label, text string, shown, total, groups int, grouped bool) string {
	line := "Filter: " + label + "."
	if text != "" {
		line += " Text: " + text + "."
	}
	if !grouped {
		return fmt.Sprintf("%s Showing %d of %d.", line, shown, total)
	}
	noun := "groups"
	if groups == 1 {
		noun = "group"
	}
	return fmt.Sprintf("%s Showing %d of %d requests in %d %s.", line, shown, total, groups, noun)
}
