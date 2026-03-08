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
	Expanded     bool // only for repo headers
	SessionCount int  // only for repo headers: total sessions in group
}

// RepoGroupInfo holds session counts/statuses for a repo group (used when collapsed).
type RepoGroupInfo struct {
	SessionCount int
	StatusCounts map[session.Status]int
}

// BuildFlatItems groups sessions by repo and flattens into a navigable list.
// expanded maps repo path -> whether the group is expanded.
func BuildFlatItems(sessions []*session.Session, expanded map[string]bool) []SidebarItem {
	groups := session.GroupByRepo(sessions)

	// Sort repo paths alphabetically.
	repos := make([]string, 0, len(groups))
	for repo := range groups {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	var items []SidebarItem
	for _, repo := range repos {
		groupSessions := groups[repo]
		isExpanded := expanded[repo] // default false = collapsed

		items = append(items, SidebarItem{
			IsRepoHeader: true,
			RepoPath:     repo,
			Expanded:     isExpanded,
			SessionCount: len(groupSessions),
		})

		if isExpanded {
			for i, s := range groupSessions {
				items = append(items, SidebarItem{
					Session: s,
					IsLast:  i == len(groupSessions)-1,
				})
			}
		}
	}
	return items
}

// CollectGroupInfo gathers status counts for a repo group from all sessions.
func CollectGroupInfo(sessions []*session.Session, repoPath string) RepoGroupInfo {
	info := RepoGroupInfo{StatusCounts: make(map[session.Status]int)}
	groups := session.GroupByRepo(sessions)
	for _, s := range groups[repoPath] {
		info.SessionCount++
		info.StatusCounts[s.GetStatus()]++
	}
	return info
}

// RenderSidebar renders the session list with repo grouping and cursor.
func RenderSidebar(items []SidebarItem, sessions []*session.Session, cursor, viewOffset, width, height int) string {
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
			info := CollectGroupInfo(sessions, item.RepoPath)
			b.WriteString(renderRepoHeader(item.RepoPath, item.Expanded, info, width, i == cursor))
		} else {
			b.WriteString(renderSessionItem(item.Session, item.IsLast, width, i == cursor))
		}
		if i < visibleEnd-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func renderRepoHeader(repoPath string, expanded bool, info RepoGroupInfo, width int, selected bool) string {
	name := filepath.Base(repoPath) + "/"

	// Expand/collapse indicator.
	expandIcon := "▸"
	if expanded {
		expandIcon = "▾"
	}

	// Build status indicators for the group.
	var indicators []string
	if n := info.StatusCounts[session.StatusRunning] + info.StatusCounts[session.StatusStarting]; n > 0 {
		indicators = append(indicators, StatusRunningStyle.Render(fmt.Sprintf("● %d", n)))
	}
	if n := info.StatusCounts[session.StatusWaiting]; n > 0 {
		indicators = append(indicators, StatusWaitingStyle.Render(fmt.Sprintf("◐ %d", n)))
	}
	if n := info.StatusCounts[session.StatusError]; n > 0 {
		indicators = append(indicators, StatusErrorStyle.Render(fmt.Sprintf("✕ %d", n)))
	}

	countStr := DimStyle.Render(fmt.Sprintf("(%d)", info.SessionCount))
	statsStr := ""
	if len(indicators) > 0 {
		statsStr = " " + strings.Join(indicators, " ")
	}

	if selected {
		icon := SessionSelectionPrefix.Render(expandIcon)
		styledName := SessionTitleSelStyle.Render(" " + name + " ")
		styledCount := SessionStatusSelStyle.Render(fmt.Sprintf("(%d)", info.SessionCount))
		return fmt.Sprintf(" %s %s%s", icon, styledName, styledCount) + statsStr
	}
	icon := DimStyle.Render(expandIcon)
	return fmt.Sprintf(" %s %s %s", icon, RepoHeaderStyle.Render(name), countStr) + statsStr
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

	// Selection prefix: ▶ when selected, space when not — both 1 char wide.
	selPrefix := " "
	treeStyle := DimStyle
	var styledSymbol, styledTitle, styledBadge string

	if selected {
		selPrefix = SessionSelectionPrefix.Render("▶")
		treeStyle = TreeConnectorSelStyle
		styledSymbol = SessionStatusSelStyle.Render(symbolRaw)
		styledTitle = SessionTitleSelStyle.Render(" " + title + " ")
		styledBadge = ToolBadgeSelStyle.Render("claude")
	} else {
		styledSymbol = StatusSymbol(status)
		styledTitle = TitleStyleForStatus(status).Render(title)
		styledBadge = ToolClaudeStyle.Render("claude")
	}

	styledConnector := treeStyle.Render(connector)
	return fmt.Sprintf(" %s%s %s %s %s", selPrefix, styledConnector, styledSymbol, styledTitle, styledBadge)
}

// NextSelectableItem finds the next item index (repo headers are now selectable).
func NextSelectableItem(items []SidebarItem, current, direction int) int {
	next := current + direction
	if next >= 0 && next < len(items) {
		return next
	}
	return current // Stay if out of bounds.
}

// FirstSelectableItem returns the index of the first item.
func FirstSelectableItem(items []SidebarItem) int {
	if len(items) > 0 {
		return 0
	}
	return 0
}
