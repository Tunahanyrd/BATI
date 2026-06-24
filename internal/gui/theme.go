package gui

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

var palette = struct {
	background, sidebar, card, border, text, muted                  color.NRGBA
	green, greenFill, blue, blueHighlight, sleep, selected, tooltip color.NRGBA
	fullMarker, discharging, threshold, estimated                   color.NRGBA
	availableRange, markerLane, warning, danger                     color.NRGBA
	hoverGuide, gapRegion, buttonHover, buttonPressed, success      color.NRGBA
}{
	background:     color.NRGBA{R: 244, G: 245, B: 246, A: 255},
	sidebar:        color.NRGBA{R: 238, G: 240, B: 241, A: 255},
	card:           color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	border:         color.NRGBA{R: 228, G: 232, B: 234, A: 255},
	text:           color.NRGBA{R: 32, G: 36, B: 38, A: 255},
	muted:          color.NRGBA{R: 110, G: 118, B: 122, A: 255},
	green:          color.NRGBA{R: 55, G: 145, B: 87, A: 255},
	greenFill:      color.NRGBA{R: 55, G: 145, B: 87, A: 32},
	hoverGuide:     color.NRGBA{R: 55, G: 145, B: 87, A: 128},
	blue:           color.NRGBA{R: 110, G: 135, B: 155, A: 255},
	blueHighlight:  color.NRGBA{R: 110, G: 135, B: 155, A: 32},
	sleep:          color.NRGBA{R: 120, G: 124, B: 128, A: 32},
	selected:       color.NRGBA{R: 224, G: 232, B: 228, A: 255},
	tooltip:        color.NRGBA{R: 255, G: 255, B: 255, A: 250},
	fullMarker:     color.NRGBA{R: 112, G: 135, B: 120, A: 255},
	discharging:    color.NRGBA{R: 215, G: 145, B: 65, A: 255},
	threshold:      color.NRGBA{R: 108, G: 135, B: 138, A: 255},
	estimated:      color.NRGBA{R: 124, G: 132, B: 137, A: 255},
	availableRange: color.NRGBA{R: 55, G: 145, B: 87, A: 10},
	markerLane:     color.NRGBA{R: 246, G: 248, B: 247, A: 255},
	warning:        color.NRGBA{R: 161, G: 105, B: 42, A: 255},
	danger:         color.NRGBA{R: 164, G: 74, B: 74, A: 255},
	gapRegion:      color.NRGBA{R: 120, G: 124, B: 128, A: 22},
	buttonHover:    color.NRGBA{R: 247, G: 249, B: 248, A: 255},
	buttonPressed:  color.NRGBA{R: 224, G: 232, B: 228, A: 255},
	success:        color.NRGBA{R: 55, G: 145, B: 87, A: 255},
}

func heading(theme *material.Theme, text string) material.LabelStyle {
	label := material.H5(theme, text)
	label.Color = palette.text
	label.Font.Weight = font.Bold
	return label
}

func sectionTitle(theme *material.Theme, text string) material.LabelStyle {
	label := material.H6(theme, text)
	label.Color = palette.text
	label.Font.Weight = font.SemiBold
	return label
}

func body(theme *material.Theme, text string) material.LabelStyle {
	label := material.Body1(theme, text)
	label.Color = palette.text
	return label
}

func secondary(theme *material.Theme, text string) material.LabelStyle {
	label := material.Body2(theme, text)
	label.Color = palette.muted
	return label
}

func pageInset() layout.Inset {
	return layout.Inset{
		Top: unit.Dp(28), Bottom: unit.Dp(28),
		Left: unit.Dp(30), Right: unit.Dp(30),
	}
}
