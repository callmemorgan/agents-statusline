package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

// ─── Configure Mode ──────────────────────────────────────────────────

func effectiveLine(id string, cfg config) int {
	if override, ok := cfg.Lines[id]; ok && override >= 1 {
		return override
	}
	if s, ok := segmentByID(id); ok {
		return s.line
	}
	return 1
}

func runConfigure() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "claude-statusline --configure requires an interactive terminal.")
		fmt.Fprintf(os.Stderr, "Edit %s directly, or run from a terminal.\n", configPath())
		os.Exit(1)
	}

	cfg := loadConfig()
	initSegments(cfg.Plugins)
	segments := registeredSegments

	app := tview.NewApplication()

	// Scrollable list of all segments with toggle state.
	list := tview.NewList().
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkSlateGrey).
		ShowSecondaryText(false)
	list.SetBorder(true)

	// Description panel — shows the description of the currently selected segment.
	descView := tview.NewTextView().SetWrap(true)
	descView.SetBorder(true).SetTitle(" Description ")

	// Live preview of the statusline (plain text — no ANSI / tview colour tags).
	// Fixed at 12 rows (10 content + 2 border) — max 9 statusline lines plus padding.
	preview := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)

	previewBox := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(preview, 0, 1, false)
	previewBox.SetBorder(true).SetTitle(" Preview ")

	// Fixed-height help bar.
	help := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText(" space toggle • 1-9 line • c color • ←/→ reorder • ↑/↓ nav • ⇧↑/↓ move row • o options • h help • r reset • s save • q quit")

	// Help page — full README rendered with markdown formatting.
	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetText(markdownToTview(readmeContent))
	helpView.SetBorder(true).SetTitle(" Help — README (↑/↓ scroll • q/Esc close) ")

	// ─── Flyout Panel ────────────────────────────────────────────────────
	// Sub-feature toggle panel for segments that expose granular settings.
	// Populated dynamically when the user presses 'o' on a segment.

	flyoutTitle := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	flyoutList := tview.NewList().
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkSlateGrey).
		ShowSecondaryText(false)
	flyoutList.SetBorder(true)

	var confirmModal *tview.Modal

	flyoutDescView := tview.NewTextView().SetWrap(true)
	flyoutDescView.SetBorder(true).SetTitle(" Description ")

	flyoutPreview := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	flyoutPreview.SetBorder(true).SetTitle(" Preview ")

	flyoutHelp := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText(" space toggle/cycle • ←/→ adjust • ⇧+←/→ coarse step • ↑/↓ nav • q/Esc close ")

	var currentFlyoutSegment string

	updateFlyout := func() {
		if currentFlyoutSegment == "" {
			return
		}
		specs := segmentSpecs(currentFlyoutSegment)
		currentIdx := flyoutList.GetCurrentItem()
		flyoutList.Clear()
		for _, sp := range specs {
			val := flyoutValueStr(currentFlyoutSegment, sp, cfg)
			mark := "  "
			display := sp.Name
			if sp.Kind == kindBool {
				if val == "on" {
					mark = "✓ "
				}
			} else {
				display = fmt.Sprintf("%s: %s", sp.Name, val)
			}
			flyoutList.AddItem(mark+display, "", 0, nil)
		}
		if currentIdx >= 0 && currentIdx < len(specs) {
			flyoutList.SetCurrentItem(currentIdx)
		}
		flyoutList.SetTitle(fmt.Sprintf(" %s settings ", currentFlyoutSegment))

		// Update preview
		p := flyoutPreviewPayload(currentFlyoutSegment, samplePayload())
		segPalette := currentPalette(cfg)
		if s, ok := segmentByID(currentFlyoutSegment); ok && segPalette.Rst != "" {
			if colorName := cfg.Colors[currentFlyoutSegment]; colorName != "" && colorName != "default" {
				segPalette = paletteWithOverride(segPalette, s.primaryColor, colorName)
			}
		}
		if s, ok := segmentByID(currentFlyoutSegment); ok {
			ctx := renderCtx{
				P:   p,
				C:   segPalette,
				S:   settingsFor(cfg, s),
				Now: time.Now(),
			}
			if rendered, show := s.render(ctx); show {
				flyoutPreview.SetText(ansiToTview(strings.TrimLeft(rendered, " ")))
			} else {
				flyoutPreview.SetText("(segment hidden)")
			}
		}
	}

	flyoutList.SetChangedFunc(func(idx int, _, _ string, _ rune) {
		if currentFlyoutSegment == "" {
			return
		}
		specs := segmentSpecs(currentFlyoutSegment)
		if idx >= 0 && idx < len(specs) {
			flyoutDescView.SetText(specs[idx].Desc)
		} else {
			flyoutDescView.SetText("")
		}
	})

	pages := tview.NewPages()

	openFlyout := func(segID string) {
		specs := segmentSpecs(segID)
		if len(specs) == 0 {
			descView.SetText("(no configurable options for this segment)")
			return
		}
		currentFlyoutSegment = segID
		flyoutTitle.SetText(fmt.Sprintf("[yellow::b]  %s — sub-features[-::-]", segID))
		updateFlyout()
		flyoutDescView.SetText(specs[0].Desc)
		pages.SwitchToPage("flyout")
		app.SetFocus(flyoutList)
	}

	var updateUI func()

	list.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDoubleClick && list.InRect(event.Position()) {
			idx := list.GetCurrentItem()
			if idx >= 0 && idx < len(segments) {
				openFlyout(segments[idx].id)
			}
			return tview.MouseConsumed, nil
		}
		if action == tview.MouseLeftClick && list.InRect(event.Position()) {
			x, y := event.Position()
			innerX, innerY, _, _ := list.GetInnerRect()
			if x >= innerX && x <= innerX+1 {
				itemOff, _ := list.GetOffset()
				clickedIdx := y - innerY + itemOff
				if clickedIdx >= 0 && clickedIdx < len(segments) {
					id := segments[clickedIdx].id
					found := -1
					for i, segID := range cfg.Segments {
						if segID == id {
							found = i
							break
						}
					}
					if found >= 0 {
						cfg.Segments = append(cfg.Segments[:found], cfg.Segments[found+1:]...)
					} else {
						cfg.Segments = append(cfg.Segments, id)
					}
					list.SetCurrentItem(clickedIdx)
					app.SetFocus(list)
					updateUI()
					return tview.MouseConsumed, nil
				}
			}
		}
		return action, event
	})

	// activateFlyoutRow handles "primary action" on a flyout row (space, enter,
	// double-click): bools toggle, enums cycle forward, ints step up.
	// sync_to_all opens the confirm modal instead of mutating directly.
	activateFlyoutRow := func(idx int) {
		specs := segmentSpecs(currentFlyoutSegment)
		if idx < 0 || idx >= len(specs) {
			return
		}
		sp := specs[idx]
		if sp.Key == "sync_to_all" {
			pages.SwitchToPage("confirm")
			app.SetFocus(confirmModal)
			return
		}
		applyFlyoutChange(currentFlyoutSegment, sp, &cfg, 1)
		if sp.Key == "stress_test" && stressTestActive[currentFlyoutSegment] {
			scheduleStressTick(app, currentFlyoutSegment, updateFlyout)
		}
		updateFlyout()
	}

	flyoutList.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDoubleClick && flyoutList.InRect(event.Position()) {
			activateFlyoutRow(flyoutList.GetCurrentItem())
			return tview.MouseConsumed, nil
		}
		return action, event
	})

	flyoutList.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		activateFlyoutRow(idx)
	})

	flyoutTopRow := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(flyoutList, 0, 1, true).
		AddItem(flyoutDescView, 0, 3, false)

	flyoutFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(flyoutTitle, 1, 0, false).
		AddItem(flyoutTopRow, 0, 1, true).
		AddItem(flyoutPreview, 5, 0, false).
		AddItem(flyoutHelp, 1, 0, false)

	// Update list items and preview from current cfg.
	updateUI = func() {
		currentIdx := list.GetCurrentItem()

		list.Clear()
		for _, s := range segments {
			enabled := false
			for _, id := range cfg.Segments {
				if id == s.id {
					enabled = true
					break
				}
			}
			mark := "  "
			if enabled {
				mark = "✓ "
			}

			line := s.line
			if override, ok := cfg.Lines[s.id]; ok && override >= 1 {
				line = override
			}
			lineStr := ""
			if line != s.line {
				lineStr = fmt.Sprintf(" [L%d]", line)
			}

			colorStr := ""
			if colorName := cfg.Colors[s.id]; colorName != "" && colorName != "default" {
				colorStr = fmt.Sprintf("[%s]", colorName)
			}

			arrow := ""
			if len(s.settings) > 0 {
				arrow = " →"
			}
			mainText := fmt.Sprintf("%s%s%s%s", mark, s.id, lineStr, colorStr)
			if arrow != "" {
				_, _, innerWidth, _ := list.GetInnerRect()
				pad := innerWidth - tview.TaggedStringWidth(mainText) - tview.TaggedStringWidth(arrow)
				if pad < 0 {
					pad = 0
				}
				mainText += strings.Repeat(" ", pad) + arrow
			}
			list.AddItem(mainText, "", 0, nil)
		}

		if currentIdx >= 0 && currentIdx < len(segments) {
			list.SetCurrentItem(currentIdx)
		}
		list.SetTitle(fmt.Sprintf(" Segments (%d/%d) ", len(cfg.Segments), len(segments)))

		// Refresh preview with colours converted to tview tags.
		p := samplePayload()
		lines := buildStatusline(buildInput{P: p, C: currentPalette(cfg), Cfg: cfg, Now: time.Now()})
		for i, l := range lines {
			lines[i] = strings.TrimLeft(l, " ")
		}
		previewText := strings.TrimSpace(strings.Join(lines, "\n"))
		if previewText == "" {
			previewText = "(statusline hidden — no segments enabled)"
		} else {
			previewText = ansiToTview(previewText)
		}
		preview.SetText(previewText)
	}

	updateUI()

	list.SetChangedFunc(func(idx int, _, _ string, _ rune) {
		if idx >= 0 && idx < len(segments) {
			descView.SetText(segments[idx].desc)
		} else {
			descView.SetText("")
		}
	})
	// Seed the description for the initial selection.
	if len(segments) > 0 {
		descView.SetText(segments[0].desc)
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// When the help or flyout page is visible, only intercept close/nav keys;
		// let everything else pass through so the inner widget handles scrolling.
		pageName, _ := pages.GetFrontPage()
		if pageName == "help" {
			switch event.Key() {
			case tcell.KeyEscape:
				pages.SwitchToPage("configure")
				app.SetFocus(list)
				return nil
			case tcell.KeyRune:
				if event.Rune() == 'q' || event.Rune() == 'Q' {
					pages.SwitchToPage("configure")
					app.SetFocus(list)
					return nil
				}
			}
			return event
		}
		if pageName == "flyout" {
			switch event.Key() {
			case tcell.KeyEscape:
				stopStressTest(currentFlyoutSegment)
				pages.SwitchToPage("configure")
				app.SetFocus(list)
				updateUI()
				return nil
			case tcell.KeyRune:
				r := event.Rune()
				if r == 'q' || r == 'Q' {
					stopStressTest(currentFlyoutSegment)
					pages.SwitchToPage("configure")
					app.SetFocus(list)
					updateUI()
					return nil
				}
				if r == ' ' {
					activateFlyoutRow(flyoutList.GetCurrentItem())
					return nil
				}
			case tcell.KeyLeft, tcell.KeyRight:
				idx := flyoutList.GetCurrentItem()
				specs := segmentSpecs(currentFlyoutSegment)
				if idx >= 0 && idx < len(specs) && specs[idx].Kind == kindInt {
					delta := 1
					if event.Key() == tcell.KeyLeft {
						delta = -1
					}
					if event.Modifiers()&tcell.ModShift != 0 && specs[idx].Step > 1 {
						delta *= specs[idx].Step
					}
					applyFlyoutChange(currentFlyoutSegment, specs[idx], &cfg, delta)
					updateFlyout()
					return nil
				}
			}
			return event
		}
		if pageName == "confirm" {
			switch event.Key() {
			case tcell.KeyEscape:
				pages.SwitchToPage("flyout")
				app.SetFocus(flyoutList)
				return nil
			case tcell.KeyRune:
				if event.Rune() == 'q' || event.Rune() == 'Q' {
					pages.SwitchToPage("flyout")
					app.SetFocus(flyoutList)
					return nil
				}
			}
			return event
		}

		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'o', 'O':
				idx := list.GetCurrentItem()
				if idx >= 0 && idx < len(segments) {
					openFlyout(segments[idx].id)
				}
				return nil
			case 'h', 'H':
				pages.SwitchToPage("help")
				app.SetFocus(helpView)
				return nil
			case ' ':
				idx := list.GetCurrentItem()
				if idx < 0 || idx >= len(segments) {
					return nil
				}
				id := segments[idx].id
				found := -1
				for i, segID := range cfg.Segments {
					if segID == id {
						found = i
						break
					}
				}
				if found >= 0 {
					cfg.Segments = append(cfg.Segments[:found], cfg.Segments[found+1:]...)
				} else {
					cfg.Segments = append(cfg.Segments, id)
				}
				updateUI()
				return nil
			case 'c', 'C':
				idx := list.GetCurrentItem()
				if idx < 0 || idx >= len(segments) {
					return nil
				}
				id := segments[idx].id
				if cfg.Colors == nil {
					cfg.Colors = make(map[string]string)
				}
				currentColor := cfg.Colors[id]
				if currentColor == "" {
					currentColor = "default"
				}
				nextColor := "default"
				for i, name := range colorCycle {
					if name == currentColor {
						nextColor = colorCycle[(i+1)%len(colorCycle)]
						break
					}
				}
				if nextColor == "default" {
					delete(cfg.Colors, id)
				} else {
					cfg.Colors[id] = nextColor
				}
				// Ensure the segment is enabled when assigning a color.
				enabled := false
				for _, segID := range cfg.Segments {
					if segID == id {
						enabled = true
						break
					}
				}
				if !enabled {
					cfg.Segments = append(cfg.Segments, id)
				}
				updateUI()
				return nil
			default:
				r := event.Rune()
				if r >= '1' && r <= '9' {
					idx := list.GetCurrentItem()
					if idx < 0 || idx >= len(segments) {
						return nil
					}
					id := segments[idx].id
					n := int(r - '0')
					if cfg.Lines == nil {
						cfg.Lines = make(map[string]int)
					}
					if segments[idx].line == n {
						delete(cfg.Lines, id)
					} else {
						cfg.Lines[id] = n
					}
					// Ensure the segment is enabled when assigning a line.
					enabled := false
					for _, segID := range cfg.Segments {
						if segID == id {
							enabled = true
							break
						}
					}
					if !enabled {
						cfg.Segments = append(cfg.Segments, id)
					}
					updateUI()
					return nil
				}
			case 'r', 'R':
				cfg = defaultConfig()
				updateUI()
				return nil
			case 's', 'S':
				if err := saveConfig(cfg); err != nil {
					preview.SetText(fmt.Sprintf("Error saving: %v", err))
					return nil
				}
				app.Stop()
				fmt.Printf("Saved to %s\n", configPath())
				return nil
			case 'q', 'Q':
				app.Stop()
				return nil
			}
		case tcell.KeyEscape:
			app.Stop()
			return nil
		case tcell.KeyUp, tcell.KeyDown:
			// Unmodified Up/Down: pass through for list navigation.
			if event.Modifiers()&tcell.ModShift == 0 {
				return event
			}
			// Shift+Up / Shift+Down: swap the entire row with the adjacent row.
			idx := list.GetCurrentItem()
			if idx < 0 || idx >= len(segments) {
				return nil
			}
			id := segments[idx].id
			myLine := effectiveLine(id, cfg)
			targetLine := myLine - 1
			if event.Key() == tcell.KeyDown {
				targetLine = myLine + 1
			}
			if targetLine < 1 || targetLine > 9 {
				return nil
			}
			if cfg.Lines == nil {
				cfg.Lines = make(map[string]int)
			}
			// Snapshot which segments are on each line before reassigning.
			var onMyLine, onTargetLine []string
			for _, sid := range cfg.Segments {
				el := effectiveLine(sid, cfg)
				if el == myLine {
					onMyLine = append(onMyLine, sid)
				} else if el == targetLine {
					onTargetLine = append(onTargetLine, sid)
				}
			}
			assignLine := func(sid string, line int) {
				naturalLine := 1
				if s, ok := segmentByID(sid); ok {
					naturalLine = s.line
				}
				if line == naturalLine {
					delete(cfg.Lines, sid)
				} else {
					cfg.Lines[sid] = line
				}
			}
			for _, sid := range onMyLine {
				assignLine(sid, targetLine)
			}
			for _, sid := range onTargetLine {
				assignLine(sid, myLine)
			}
			updateUI()
			return nil
		case tcell.KeyLeft, tcell.KeyRight:
			idx := list.GetCurrentItem()
			if idx < 0 || idx >= len(segments) {
				return event
			}
			id := segments[idx].id
			myLine := effectiveLine(id, cfg)
			// Collect indices in cfg.Segments that share the same line, in order.
			var peers []int
			for i, sid := range cfg.Segments {
				if effectiveLine(sid, cfg) == myLine {
					peers = append(peers, i)
				}
			}
			// Find this segment's position within peers.
			pos := -1
			for i, pi := range peers {
				if cfg.Segments[pi] == id {
					pos = i
					break
				}
			}
			if event.Key() == tcell.KeyLeft && pos > 0 {
				cfg.Segments[peers[pos]], cfg.Segments[peers[pos-1]] =
					cfg.Segments[peers[pos-1]], cfg.Segments[peers[pos]]
				updateUI()
				return nil
			} else if event.Key() == tcell.KeyRight && pos >= 0 && pos < len(peers)-1 {
				cfg.Segments[peers[pos]], cfg.Segments[peers[pos+1]] =
					cfg.Segments[peers[pos+1]], cfg.Segments[peers[pos]]
				updateUI()
				return nil
			}
		}
		return event
	})

	topRow := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(list, 0, 1, true).
		AddItem(descView, 0, 3, false)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topRow, 0, 1, true).
		AddItem(previewBox, 12, 0, false).
		AddItem(help, 1, 0, false)

	pages.AddPage("configure", flex, true, true)
	pages.AddPage("help", helpView, true, false)
	pages.AddPage("flyout", flyoutFlex, true, false)

	confirmModal = tview.NewModal().
		SetText("Copy these settings to all progress bar segments?").
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Yes" {
				syncSettingsToAllBars(&cfg, currentFlyoutSegment)
			}
			pages.SwitchToPage("flyout")
			app.SetFocus(flyoutList)
			updateFlyout()
		})
	pages.AddPage("confirm", confirmModal, true, false)

	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
