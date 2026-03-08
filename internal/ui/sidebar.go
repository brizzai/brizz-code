package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuvalhayke/brizz-code/internal/session"
)

// Tree drawing characters.
const (
	treeBranch = "├─"
	treeLast   = "└─"
)

// SidebarItem represents a flattened item for cursor navigation.
type SidebarItem struct {
	IsRepoHeader bool
	RepoPath     string
	Session      *session.Session
	IsLast       bool // last session in its repo group
}

// BuildFlatItems groups sessions by repo and flattens into a navigable list.
func BuildFlatItems(sessions []*session.Session) []SidebarItem {
	groups := session.GroupByRepo(sessions)

	// Sort repo paths alphabetically.
	repos := make([]string, 0, len(groups))
	for repo := range groups {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	var items []SidebarItem
	for _, repo := range repos {
		items = append(items, SidebarItem{
			IsRepoHeader: true,
			RepoPath:     repo,
		})

		groupSessions := groups[repo]
		for i, s := range groupSessions {
			items = append(items, SidebarItem{
				Session: s,
				IsLast:  i == len(groupSessions)-1,
			})
		}
	}
	return items
}

// RenderSidebar renders the session list with repo grouping and cursor.
func RenderSidebar(items []SidebarItem, cursor, viewOffset, width, height int) string {
	if len(items) == 0 {
		msg := DimStyle.Render("  No sessions. Press 'a' to add one.")
		return PanelTitleStyle.Render(" SESSIONS") + "\n" + msg
	}

	var b strings.Builder

	// Panel title.
	b.WriteString(PanelTitleStyle.Render(" SESSIONS"))
	b.WriteString("\n")

	// Determine visible range (subtract 1 for the title line).
	visibleHeight := height - 1
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	visibleEnd := viewOffset + visibleHeight
	if visibleEnd > len(items) {
		visibleEnd = len(items)
	}

	for i := viewOffset; i < visibleEnd; i++ {
		item := items[i]
		if item.IsRepoHeader {
			b.WriteString(renderRepoHeader(item.RepoPath, items, i, width, i == cursor))
		} else {
			b.WriteString(renderSessionItem(item.Session, item.IsLast, width, i == cursor))
		}
		if i < visibleEnd-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func renderRepoHeader(repoPath string, items []SidebarItem, headerIdx, width int, selected bool) string {
	name := filepath.Base(repoPath) + "/"

	// Count sessions and collect status counts for this group.
	var sessionCount int
	statusCounts := make(map[session.Status]int)
	for i := headerIdx + 1; i < len(items); i++ {
		if items[i].IsRepoHeader {
			break
		}
		if items[i].Session != nil {
			sessionCount++
			statusCounts[items[i].Session.GetStatus()]++
		}
	}

	// Build status indicators for the group.
	var indicators []string
	if n := statusCounts[session.StatusRunning] + statusCounts[session.StatusStarting]; n > 0 {
		indicators = append(indicators, StatusRunningStyle.Render(fmt.Sprintf("● %d", n)))
	}
	if n := statusCounts[session.StatusWaiting]; n > 0 {
		indicators = append(indicators, StatusWaitingStyle.Render(fmt.Sprintf("◐ %d", n)))
	}
	if n := statusCounts[session.StatusError]; n > 0 {
		indicators = append(indicators, StatusErrorStyle.Render(fmt.Sprintf("✕ %d", n)))
	}

	countStr := DimStyle.Render(fmt.Sprintf("(%d)", sessionCount))
	statsStr := ""
	if len(indicators) > 0 {
		statsStr = " " + strings.Join(indicators, " ")
	}

	if selected {
		return SessionSelectedStyle.Render(fmt.Sprintf("  %s %s", name, countStr)) + statsStr
	}
	return RepoHeaderStyle.Render(fmt.Sprintf("  %s", name)) + " " + countStr + statsStr
}

func renderSessionItem(s *session.Session, isLast bool, width int, selected bool) string {
	status := s.GetStatus()
	symbolRaw := StatusSymbolRaw(status)
	title := s.Title

	// Tree connector.
	connector := treeBranch
	if isLast {
		connector = treeLast
	}

	// Truncate title if needed.
	maxTitleLen := width - 16 // account for tree + symbol + badge + spacing
	if maxTitleLen < 10 {
		maxTitleLen = 10
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	if selected {
		// Inverted highlight for selected row.
		prefix := SessionSelectionPrefix.Render("▶")
		styledConnector := TreeConnectorSelStyle.Render(connector)
		styledSymbol := SessionStatusSelStyle.Render(symbolRaw)
		styledTitle := SessionTitleSelStyle.Render(" " + title + " ")
		styledBadge := ToolBadgeSelStyle.Render("claude")
		return fmt.Sprintf(" %s %s %s%s %s", prefix, styledConnector, styledSymbol, styledTitle, styledBadge)
	}

	// Normal rendering.
	styledConnector := DimStyle.Render(connector)
	styledSymbol := StatusSymbol(status)
	styledTitle := TitleStyleForStatus(status).Render(title)
	styledBadge := ToolClaudeStyle.Render("claude")
	return fmt.Sprintf("  %s %s %s %s", styledConnector, styledSymbol, styledTitle, styledBadge)
}

// NextSelectableItem finds the next non-header item index moving in the given direction.
func NextSelectableItem(items []SidebarItem, current, direction int) int {
	next := current + direction
	for next >= 0 && next < len(items) {
		if !items[next].IsRepoHeader {
			return next
		}
		next += direction
	}
	return current // Stay if nothing found.
}

// FirstSelectableItem returns the index of the first session item.
func FirstSelectableItem(items []SidebarItem) int {
	for i, item := range items {
		if !item.IsRepoHeader {
			return i
		}
	}
	return 0
}
