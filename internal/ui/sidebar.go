package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yuvalhayke/brizz-code/internal/git"
	"github.com/yuvalhayke/brizz-code/internal/github"
	"github.com/yuvalhayke/brizz-code/internal/session"
)

// Tree drawing characters.
const (
	treeBranch = "├─"
	treeLast   = "└─"
	branchIcon = ""
)

// SidebarItem represents a flattened item for cursor navigation.
type SidebarItem struct {
	IsRepoHeader bool
	RepoPath     string
	Session      *session.Session
	IsLast       bool // last session in its repo group
	Expanded     bool // only for repo headers
	SessionCount int  // only for repo headers: total sessions in group
	Pending      *PendingWorkspace // non-nil for phantom "creating..." entries
}

// RepoGroupInfo holds session counts/statuses for a repo group (used when collapsed).
type RepoGroupInfo struct {
	SessionCount int
	StatusCounts map[session.Status]int
}

// BuildFlatItems groups sessions by repo and flattens into a navigable list.
// expanded maps repo path -> whether the group is expanded.
// filter, when non-empty, only includes sessions whose title contains the filter string.
// pending workspaces are injected as phantom entries under their repo group.
func BuildFlatItems(sessions []*session.Session, pending []*PendingWorkspace, expanded map[string]bool, filter string) []SidebarItem {
	groups := session.GroupByRepo(sessions)

	// Include repos that only have pending workspaces (no sessions yet).
	for _, pw := range pending {
		if _, exists := groups[pw.RepoPath]; !exists {
			groups[pw.RepoPath] = nil
		}
	}

	// Sort repo paths alphabetically.
	repos := make([]string, 0, len(groups))
	for repo := range groups {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	lowerFilter := strings.ToLower(filter)

	// Index pending workspaces by repo for fast lookup.
	pendingByRepo := make(map[string][]*PendingWorkspace)
	for _, pw := range pending {
		pendingByRepo[pw.RepoPath] = append(pendingByRepo[pw.RepoPath], pw)
	}

	var items []SidebarItem
	for _, repo := range repos {
		groupSessions := groups[repo]
		repoPending := pendingByRepo[repo]

		// Apply filter: only include matching sessions.
		var filtered []*session.Session
		if lowerFilter != "" {
			for _, s := range groupSessions {
				if strings.Contains(strings.ToLower(s.Title), lowerFilter) {
					filtered = append(filtered, s)
				}
			}
			// Skip repo groups with no matching sessions and no pending.
			if len(filtered) == 0 && len(repoPending) == 0 {
				continue
			}
		} else {
			filtered = groupSessions
		}

		isExpanded := expanded[repo] // default false = collapsed

		items = append(items, SidebarItem{
			IsRepoHeader: true,
			RepoPath:     repo,
			Expanded:     isExpanded,
			SessionCount: len(groupSessions), // Always show total count (real sessions only).
		})

		if isExpanded {
			totalChildren := len(filtered) + len(repoPending)
			childIdx := 0

			for _, s := range filtered {
				childIdx++
				items = append(items, SidebarItem{
					Session: s,
					IsLast:  childIdx == totalChildren,
				})
			}

			// Append pending workspaces after real sessions.
			for _, pw := range repoPending {
				childIdx++
				items = append(items, SidebarItem{
					Pending: pw,
					IsLast:  childIdx == totalChildren,
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
func RenderSidebar(items []SidebarItem, sessions []*session.Session, gitInfo map[string]*git.RepoInfo, cursor, viewOffset, width, height int) string {
	if len(items) == 0 {
		return renderEmptyState(width, height)
	}

	var b strings.Builder

	// Panel title.
	b.WriteString(RenderPanelTitle(" SESSIONS", width))
	b.WriteString("\n")

	// Determine visible range (subtract 2 for title + underline).
	visibleHeight := height - 2
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Check if scroll indicators are needed.
	showAbove := viewOffset > 0
	showBelow := (viewOffset + visibleHeight) < len(items)

	// Reduce visible height for scroll indicators.
	if showAbove {
		visibleHeight--
	}
	if showBelow {
		visibleHeight--
	}
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	visibleEnd := viewOffset + visibleHeight
	if visibleEnd > len(items) {
		visibleEnd = len(items)
	}

	// Top scroll indicator.
	if showAbove {
		above := viewOffset
		b.WriteString(DimStyle.Render(fmt.Sprintf("  ⋮ +%d above", above)))
		b.WriteString("\n")
	}

	for i := viewOffset; i < visibleEnd; i++ {
		item := items[i]
		if item.IsRepoHeader {
			info := CollectGroupInfo(sessions, item.RepoPath)
			repoInfo := gitInfo[item.RepoPath]
			b.WriteString(renderRepoHeader(item.RepoPath, item.Expanded, info, repoInfo, width, i == cursor))
		} else if item.Pending != nil {
			b.WriteString(renderPendingItem(item.Pending, item.IsLast, width, i == cursor))
		} else {
			b.WriteString(renderSessionItem(item.Session, item.IsLast, width, i == cursor))
		}
		if i < visibleEnd-1 {
			b.WriteString("\n")
		}
	}

	// Bottom scroll indicator.
	if showBelow {
		below := len(items) - visibleEnd
		b.WriteString("\n")
		b.WriteString(DimStyle.Render(fmt.Sprintf("  ⋮ +%d below", below)))
	}

	return b.String()
}

// renderEmptyState renders the empty sessions placeholder.
func renderEmptyState(width, height int) string {
	var b strings.Builder
	b.WriteString(RenderPanelTitle(" SESSIONS", width))
	b.WriteString("\n")

	if height < 8 {
		b.WriteString(DimStyle.Render("  No sessions — 'a' to create"))
		return b.String()
	}

	// Centered empty state.
	icon := lipgloss.NewStyle().Foreground(ColorAccent).Render("⬡")
	title := lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render("No Sessions Yet")
	hint1 := DimStyle.Render("Press 'a' to create one")
	hint2 := DimStyle.Render("Press '?' for help")

	// Center each line.
	center := func(s string) string {
		w := lipgloss.Width(s)
		pad := (width - w) / 2
		if pad < 0 {
			pad = 0
		}
		return strings.Repeat(" ", pad) + s
	}

	b.WriteString("\n")
	b.WriteString(center(icon) + "\n")
	b.WriteString(center(title) + "\n")
	b.WriteString("\n")
	b.WriteString(center(hint1) + "\n")
	b.WriteString(center(hint2))

	return b.String()
}

func renderRepoHeader(repoPath string, expanded bool, info RepoGroupInfo, repoInfo *git.RepoInfo, width int, selected bool) string {
	name := filepath.Base(repoPath)

	// Expand/collapse indicator.
	expandIcon := "▸"
	if expanded {
		expandIcon = "▾"
	}

	// Git branch + dirty indicator.
	branchStr := ""
	dirtyStr := ""
	if repoInfo != nil {
		if repoInfo.Branch != "" {
			branch := repoInfo.Branch
			// Strip username prefix (e.g. "yuval/brz-123" → "brz-123").
			if idx := strings.LastIndex(branch, "/"); idx != -1 {
				branch = branch[idx+1:]
			}
			if len(branch) > 15 {
				branch = branch[:12] + "..."
			}
			if selected {
				branchStr = " " + SessionStatusSelStyle.Render(branchIcon+" "+branch)
			} else {
				branchStr = " " + BranchStyle.Render(branchIcon+" "+branch)
			}
		}
		if repoInfo.IsDirty {
			if selected {
				dirtyStr = SessionStatusSelStyle.Render("*")
			} else {
				dirtyStr = DirtyStyle.Render("*")
			}
		}
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

	// PR badge.
	prStr := ""
	if repoInfo != nil && repoInfo.PR != nil {
		prStr = " " + renderPRBadge(repoInfo.PR, selected)
	}

	if selected {
		icon := SessionSelectionPrefix.Render(expandIcon)
		styledName := SessionTitleSelStyle.Render(" " + name + " ")
		styledCount := SessionStatusSelStyle.Render(fmt.Sprintf("(%d)", info.SessionCount))
		return fmt.Sprintf(" %s %s%s%s %s", icon, styledName, branchStr, dirtyStr, styledCount) + statsStr + prStr
	}
	icon := DimStyle.Render(expandIcon)
	return fmt.Sprintf(" %s %s%s%s %s", icon, RepoHeaderStyle.Render(name), branchStr, dirtyStr, countStr) + statsStr + prStr
}

func renderPRBadge(pr *github.PR, selected bool) string {
	if pr == nil {
		return ""
	}

	// Hide closed (not merged) PRs entirely.
	if pr.State == "CLOSED" {
		return ""
	}

	badge := fmt.Sprintf("#%d", pr.Number)

	// Merged: purple with upward arrow.
	if pr.State == "MERGED" {
		result := badge + " ⇡"
		if selected {
			return SessionStatusSelStyle.Render(result)
		}
		return PRMergedStyle.Render(result)
	}

	// Determine color from overall state, icons only for problems.
	ciFail := pr.CIStatus == "FAILURE"
	changesReq := pr.ReviewDecision == "CHANGES_REQUESTED"
	approved := pr.ReviewDecision == "APPROVED"
	ciPass := pr.CIStatus == "SUCCESS"
	hasThreads := pr.UnresolvedThreads > 0

	var icons string
	style := PRPendingStyle // default: yellow (waiting)

	if ciFail || changesReq || hasThreads {
		// Red: something needs fixing. Icons explain what.
		style = PRFailStyle
		if ciFail {
			icons += "✕"
		}
		if changesReq || hasThreads {
			icons += "↩"
		}
	} else if approved && ciPass {
		// Green: ready to merge.
		style = PROpenStyle
		icons = "✓"
	}

	result := badge
	if icons != "" {
		result += " " + icons
	}

	if selected {
		return SessionStatusSelStyle.Render(result)
	}
	return style.Render(result)
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
	maxTitleLen := width - 10 // account for tree + symbol + spacing
	if maxTitleLen < 10 {
		maxTitleLen = 10
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	// Selection prefix: ▶ when selected, space when not — both 1 char wide.
	selPrefix := " "
	treeStyle := DimStyle
	var styledSymbol, styledTitle string

	if selected {
		selPrefix = SessionSelectionPrefix.Render("▶")
		treeStyle = TreeConnectorSelStyle
		styledSymbol = SessionStatusSelStyle.Render(symbolRaw)
		styledTitle = SessionTitleSelStyle.Render(" " + title + " ")
	} else {
		styledSymbol = StatusSymbol(status)
		styledTitle = TitleStyleForStatus(status).Render(title)
	}

	styledConnector := treeStyle.Render(connector)
	return fmt.Sprintf(" %s%s %s %s", selPrefix, styledConnector, styledSymbol, styledTitle)
}

func renderPendingItem(pw *PendingWorkspace, isLast bool, width int, selected bool) string {
	spinner := spinnerFrames[pw.Frame%len(spinnerFrames)]
	title := "Creating \"" + pw.Name + "\"..."

	connector := treeBranch
	if isLast {
		connector = treeLast
	}

	maxTitleLen := width - 10
	if maxTitleLen < 10 {
		maxTitleLen = 10
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	selPrefix := " "
	treeStyle := DimStyle
	var styledSpinner, styledTitle string

	if selected {
		selPrefix = SessionSelectionPrefix.Render("▶")
		treeStyle = TreeConnectorSelStyle
		styledSpinner = SessionStatusSelStyle.Render(spinner)
		styledTitle = SessionTitleSelStyle.Render(" " + title + " ")
	} else {
		styledSpinner = lipgloss.NewStyle().Foreground(ColorAccent).Render(spinner)
		styledTitle = DimStyle.Render(title)
	}

	styledConnector := treeStyle.Render(connector)
	return fmt.Sprintf(" %s%s %s %s", selPrefix, styledConnector, styledSpinner, styledTitle)
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
