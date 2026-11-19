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
		groups = append(groups, historyGroup{
			Key:           key,
			Method:        first.entry.Method,
			Path:          first.entry.Path,
			Upstream:      first.entry.UpstreamLabel(),
			When:          first.line.When,
			CountLabel:    requestCountLabel(len(rows)),
			StatusSummary: statusSummary(statuses),
			Sources:       distinctLabels(sources),
			Rows:          rows,
			newest:        first.entry.StartedAt,
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].newest.After(groups[j].newest)
	})
	return groups
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
