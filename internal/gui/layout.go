package gui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"bati/internal/dto"
	"bati/internal/statusfmt"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	appName          = "BATI"
	appTagline       = "Battery Analytics & Timeline Interface"
	aboutPrivacyText = "All collected telemetry stays strictly on this machine."
	aboutSignature   = "Signed by Tunahanyrd."
)

func (state *UIState) layout(gtx layout.Context) layout.Dimensions {
	paint.Fill(gtx.Ops, palette.background)
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(state.layoutSidebar),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return filledBox(gtx, image.Pt(1, gtx.Constraints.Max.Y), palette.border)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return pageInset().Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(state.layoutTopBar),
					layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
					layout.Flexed(1, state.layoutPage),
				)
			})
		}),
	)
}

func (state *UIState) layoutTopBar(gtx layout.Context) layout.Dimensions {
	height := gtx.Dp(42)
	width := gtx.Constraints.Max.X
	fillRect(gtx, image.Rect(0, height-1, width, height), palette.border)
	dimensions := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					label := material.Body1(state.theme, appName)
					label.Color = palette.text
					label.Font.Weight = font.Bold
					return label.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					label := material.Body2(state.theme, appTagline)
					label.Color = palette.muted
					return label.Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return state.statusPill(gtx, topHeaderStatus(state.data, state.currentFreshness()))
		}),
	)
	dimensions.Size.Y = height
	return dimensions
}

func (state *UIState) statusPill(gtx layout.Context, text string) layout.Dimensions {
	if strings.TrimSpace(text) == "" {
		return layout.Dimensions{}
	}
	macro := op.Record(gtx.Ops)
	label := material.Caption(state.theme, text)
	label.Color = palette.muted
	label.Font.Weight = font.Medium
	dimensions := layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, label.Layout)
	call := macro.Stop()
	size := image.Pt(dimensions.Size.X, max(dimensions.Size.Y, gtx.Dp(26)))
	drawRoundedRect(gtx, size, gtx.Dp(13), palette.card, palette.border)
	call.Add(gtx.Ops)
	dimensions.Size = size
	return dimensions
}

func topHeaderStatus(data *dto.DashboardDTO, freshness freshnessView) string {
	if data == nil {
		return "loading"
	}
	if data.LiveSnapshot.Available {
		parts := []string{
			fmt.Sprintf("live %.0f%%", data.LiveSnapshot.CapacityPercent),
			statusfmt.Lower(data.LiveSnapshot.Status),
		}
		if data.LiveSnapshot.ChargeLimitAvailable && data.LiveSnapshot.ChargeLimitPercent > 0 {
			parts = append(parts, fmt.Sprintf("limit %d%%", data.LiveSnapshot.ChargeLimitPercent))
		}
		if freshness.Stale {
			parts = append(parts, "history stale")
		}
		return strings.Join(parts, " · ")
	}
	if data.HistoricalSnapshot.Available {
		return "last recorded " + formatAge(data.HistoricalSnapshot.Age) + " ago"
	}
	return "no battery data"
}

func (state *UIState) layoutSidebar(gtx layout.Context) layout.Dimensions {
	width := gtx.Dp(230)
	gtx.Constraints.Min.X = width
	gtx.Constraints.Max.X = width
	paint.Fill(gtx.Ops, palette.sidebar)
	return layout.Inset{Top: unit.Dp(24), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.H5(state.theme, appName)
				label.Color = palette.text
				label.Font.Weight = font.Bold
				return layout.Inset{Left: unit.Dp(10), Bottom: unit.Dp(24)}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return state.sidebarItem(gtx, "Overview", state.view == viewOverview, &state.overviewButton)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return state.sidebarItem(gtx, "Usage History", state.view == viewHistory, &state.historyButton)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return state.sidebarItem(gtx, "Battery Health", state.view == viewHealth, &state.healthButton)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return state.sidebarItem(gtx, "About", state.view == viewAbout, &state.aboutButton)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Spacer{}.Layout(gtx)
			}),
			layout.Rigid(state.layoutSidebarFreshness),
		)
	})
}

func (state *UIState) layoutSidebarFreshness(gtx layout.Context) layout.Dimensions {
	freshness := state.currentFreshness()
	fillRect(gtx, image.Rect(8, 0, gtx.Constraints.Max.X-8, 1), palette.border)
	return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(18), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Caption(state.theme, freshness.Title)
				label.Color = palette.muted
				if freshness.Stale {
					label.Color = palette.warning
				}
				return label.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Caption(state.theme, freshness.Detail)
				label.Color = palette.muted
				return label.Layout(gtx)
			}),
		)
	})
}

func (state *UIState) sidebarItem(gtx layout.Context, text string, selected bool, button *widget.Clickable) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			height := gtx.Dp(42)
			width := gtx.Constraints.Max.X
			if selected {
				fillRoundedRect(gtx, image.Rect(0, 0, width, height), gtx.Dp(10), palette.selected)
			}
			dimensions := layout.Inset{
				Top: unit.Dp(10), Bottom: unit.Dp(10),
				Left: unit.Dp(12), Right: unit.Dp(12),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				label := body(state.theme, text)
				if selected {
					label.Font.Weight = font.SemiBold
				}
				return label.Layout(gtx)
			})
			dimensions.Size = image.Pt(width, height)
			return dimensions
		})
	})
}

func (state *UIState) layoutPage(gtx layout.Context) layout.Dimensions {
	switch state.view {
	case viewOverview:
		return state.layoutOverview(gtx)
	case viewHistory:
		return state.layoutHistory(gtx)
	case viewHealth:
		return state.layoutHealth(gtx)
	case viewAbout:
		return state.layoutAbout(gtx)
	default:
		return state.layoutOverview(gtx)
	}
}

func (state *UIState) layoutOverview(gtx layout.Context) layout.Dimensions {
	if !state.hasData() {
		return state.layoutLoadState(gtx)
	}

	live := state.data.LiveSnapshot
	hist := state.data.HistoricalSnapshot

	if !live.Available && !hist.Available {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return secondary(state.theme, "No battery data available").Layout(gtx)
		})
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			subtitle := "Current battery and recent activity"
			if live.Available {
				if live.ChargeLimitAvailable && live.ChargeLimitPercent > 0 {
					subtitle = fmt.Sprintf("charge limit %d%%", live.ChargeLimitPercent)
				}
				freshness := state.currentFreshness()
				if freshness.Stale {
					subtitle += " · history stale"
				}
			} else if hist.Available {
				subtitle = fmt.Sprintf("Last recorded battery as of %s ago · history may not be updating", formatAge(hist.Age))
			}
			return titleBlock(gtx, state.theme, "Overview", subtitle)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(22)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					cardTitle := "Battery level"
					value, detail := "Unknown", ""
					muted := true

					if live.Available {
						value = fmt.Sprintf("%.0f%%", live.CapacityPercent)
						statusText := statusfmt.Lower(live.Status)
						if live.ChargeLimitAvailable && live.ChargeLimitPercent > 0 {
							limitVal := live.ChargeLimitPercent
							if math.Abs(live.CapacityPercent-float64(limitVal)) <= 1.0 && isChargeHoldingStatus(live.Status) {
								detail = fmt.Sprintf("limit reached · charge limit %d%%", limitVal)
							} else {
								detail = fmt.Sprintf("%s · charge limit %d%%", statusText, limitVal)
							}
						} else {
							detail = statusText
							if isPluggedIn(live.Status) {
								detail += " · plugged in"
							}
						}
						muted = false
					} else if hist.Available {
						cardTitle = "Last recorded battery"
						value = fmt.Sprintf("%.0f%%", hist.CapacityPercent)
						statusText := statusfmt.Lower(hist.Status)
						detail = fmt.Sprintf("recorded %s ago · %s", formatAge(hist.Age), statusText)
						muted = false
					}

					return state.metricCard(gtx, cardTitle, value, detail, muted, true)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					value, detail := "Unknown", ""
					muted := true

					if live.Available {
						value = statusfmt.Lower(live.Status)
						parts := []string{}
						if live.PowerRateAvailable {
							parts = append(parts, fmt.Sprintf("%.2f W", live.PowerRateW))
						}
						if live.VoltageAvailable && live.VoltageV > 0 {
							parts = append(parts, fmt.Sprintf("%.2f V", live.VoltageV))
						}
						if live.CapacityLevel != "" && strings.ToLower(live.CapacityLevel) != "unknown" {
							parts = append(parts, fmt.Sprintf("capacity level: %s", live.CapacityLevel))
						}
						detail = strings.Join(parts, " · ")
						muted = false
					} else if hist.Available {
						value = statusfmt.Lower(hist.Status)
						parts := []string{fmt.Sprintf("%.2f W", hist.PowerRateW)}
						if hist.VoltageV > 0 {
							parts = append(parts, fmt.Sprintf("%.2f V", hist.VoltageV))
						}
						detail = strings.Join(parts, " · ")
						muted = false
					}

					return state.metricCard(gtx, "State", value, detail, muted, false)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					value, detail := "Not enough data", "Keep batid running"
					muted := true
					if state.data.RecentSummary.AvailableDuration > 0 {
						value = formatDuration(state.data.RecentSummary.ActiveDuration)
						detail = "last 24 hours"
						if !live.Available && hist.Available {
							detail = "last recorded 24h"
						} else {
							freshness := state.currentFreshness()
							if freshness.Stale {
								detail = "last recorded 24h"
							}
						}
						muted = false
					}
					return state.metricCard(gtx, "Active screen time", value, detail, muted, false)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					value, detail := overnightDisplay(state.data.Overnight)
					return state.metricCard(gtx, "Overnight drain", value, detail, !overnightAvailable(state.data.Overnight), false)
				}),
			)
		}),
	)
}

func (state *UIState) metricCard(gtx layout.Context, label, value, detail string, mutedValue, accent bool) layout.Dimensions {
	return state.card(gtx, 150, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(18), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondary(state.theme, label).Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					valueLabel := material.H4(state.theme, value)
					valueLabel.Color = palette.text
					if accent {
						valueLabel.Color = palette.green
					}
					if mutedValue {
						valueLabel = material.Body1(state.theme, value)
						valueLabel.Color = palette.muted
					} else {
						valueLabel.Font.Weight = font.Bold
					}
					return valueLabel.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondary(state.theme, detail).Layout(gtx)
				}),
			)
		})
	})
}

func (state *UIState) layoutHistory(gtx layout.Context) layout.Dimensions {
	if !state.hasData() {
		return state.layoutLoadState(gtx)
	}
	children := []layout.Widget{
		state.layoutHistoryHeader,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(16)}.Layout(gtx, state.layoutRangeSelector)
		},
		state.layoutHistoryNotice,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, state.layoutLastCharge)
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, state.layoutBatteryChart)
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, state.layoutScreenChart)
		},
		state.layoutRangeSummary,
	}
	return material.List(state.theme, &state.contentList).Layout(gtx, len(children), func(gtx layout.Context, index int) layout.Dimensions {
		return children[index](gtx)
	})
}

func (state *UIState) layoutHistoryHeader(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return titleBlock(gtx, state.theme, "Usage History", "Battery level, sessions, and screen activity")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return state.smallButton(gtx, "Export CSV", &state.exportButton)
				}),
				layout.Rigid(state.layoutExportMessage),
			)
		}),
	)
}

func (state *UIState) smallButton(gtx layout.Context, text string, button *widget.Clickable) layout.Dimensions {
	return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Dp(112), gtx.Dp(36))
		gtx.Constraints.Min = size
		gtx.Constraints.Max = size
		background := palette.card
		border := palette.border
		if button.Hovered() {
			background = palette.buttonHover
			border = palette.threshold
		}
		if button.Pressed() {
			background = palette.buttonPressed
			border = palette.green
		}
		drawRoundedRect(gtx, size, gtx.Dp(8), background, border)
		if button.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			label := material.Body2(state.theme, text)
			label.Color = palette.text
			label.Font.Weight = font.Medium
			if button.Pressed() {
				label.Color = palette.green
			}
			return label.Layout(gtx)
		})
	})
}

func (state *UIState) layoutHistoryNotice(gtx layout.Context) layout.Dimensions {
	if state.history.NoticeTitle == "" {
		return layout.Dimensions{}
	}
	return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return state.card(gtx, 0, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(13), Left: unit.Dp(18), Right: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := body(state.theme, state.history.NoticeTitle)
						label.Font.Weight = font.Medium
						return label.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return secondary(state.theme, state.history.NoticeBody).Layout(gtx)
					}),
				)
			})
		})
	})
}

func (state *UIState) layoutRangeSummary(gtx layout.Context) layout.Dimensions {
	text := formatRangeSummary(state.data.Summary, state.data.Overnight, state.use24h, state.currentFreshness().Stale)
	return state.card(gtx, 62, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(18), Left: unit.Dp(18), Right: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			label := material.Body2(state.theme, text)
			label.Color = palette.muted
			return label.Layout(gtx)
		})
	})
}

func (state *UIState) layoutExportMessage(gtx layout.Context) layout.Dimensions {
	if state.exportMessage == "" {
		return layout.Dimensions{}
	}
	return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		width := min(gtx.Constraints.Max.X, gtx.Dp(420))
		if width <= 0 {
			width = gtx.Dp(300)
		}
		gtx.Constraints.Min.X = width
		gtx.Constraints.Max.X = width
		macro := op.Record(gtx.Ops)
		dims := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			label := material.Caption(state.theme, state.exportMessage)
			label.Color = palette.success
			if state.exportErr {
				label.Color = palette.danger
			}
			label.Font.Weight = font.Medium
			return label.Layout(gtx)
		})
		call := macro.Stop()
		background := color.NRGBA{R: 239, G: 247, B: 242, A: 255}
		border := color.NRGBA{R: 200, G: 224, B: 210, A: 255}
		if state.exportErr {
			background = color.NRGBA{R: 250, G: 241, B: 241, A: 255}
			border = color.NRGBA{R: 232, G: 204, B: 204, A: 255}
		}
		size := image.Pt(width, max(dims.Size.Y, gtx.Dp(34)))
		drawRoundedRect(gtx, size, gtx.Dp(10), background, border)
		call.Add(gtx.Ops)
		dims.Size = size
		return dims
	})
}

func (state *UIState) layoutRangeSelector(gtx layout.Context) layout.Dimensions {
	height := gtx.Dp(46)
	width := min(gtx.Constraints.Max.X, gtx.Dp(520))
	gtx.Constraints.Min = image.Pt(width, height)
	gtx.Constraints.Max = image.Pt(width, height)
	drawRoundedRect(gtx, image.Pt(width, height), 12, palette.sidebar, palette.border)
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return state.rangeSegment(gtx, "Last 24 Hours", state.use24h, &state.hoursButton)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return state.rangeSegment(gtx, "Last 10 Days", !state.use24h, &state.daysButton)
			}),
		)
	})
}

func (state *UIState) rangeSegment(gtx layout.Context, text string, selected bool, button *widget.Clickable) layout.Dimensions {
	return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		if selected {
			drawRoundedRect(gtx, size, 8, palette.card, palette.border)
		}
		dimensions := layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			label := body(state.theme, text)
			if selected {
				label.Font.Weight = font.SemiBold
			}
			return label.Layout(gtx)
		})
		dimensions.Size = size
		return dimensions
	})
}

func (state *UIState) layoutLastCharge(gtx layout.Context) layout.Dimensions {
	value := "Not available"
	if state.data.LastCharge.Available {
		value = state.data.LastCharge.Timestamp.Local().Format("02 Jan 2006, 15:04")
	}
	return state.card(gtx, 72, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(14), Left: unit.Dp(18), Right: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return body(state.theme, "Last full charge").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondary(state.theme, value).Layout(gtx)
				}),
			)
		})
	})
}

func (state *UIState) layoutHealth(gtx layout.Context) layout.Dimensions {
	if !state.hasData() {
		return state.layoutLoadState(gtx)
	}
	health := state.data.Health
	healthValue := "Unknown"
	if health.HealthAvailable {
		healthValue = fmt.Sprintf("%.1f%%", health.HealthPct)
	}

	cycleCount := "Unknown"
	if state.data.LiveSnapshot.Available {
		cycleCount = fmt.Sprintf("%d", state.data.LiveSnapshot.CycleCount)
	} else if health.CycleCountAvailable {
		cycleCount = fmt.Sprintf("%d", health.CycleCount)
	}

	chargeLimitVal := ""
	chargeLimitAvailable := false
	if state.data.LiveSnapshot.Available {
		chargeLimitAvailable = state.data.LiveSnapshot.ChargeLimitAvailable
		chargeLimitVal = fmt.Sprintf("%d%%", state.data.LiveSnapshot.ChargeLimitPercent)
	} else if health.ChargeLimitAvailable {
		chargeLimitAvailable = true
		chargeLimitVal = fmt.Sprintf("%d%%", health.ChargeLimitPercent)
	}

	rows := []struct{ label, value string }{
		{"Cycle count", cycleCount},
	}
	if chargeLimitAvailable {
		rows = append(rows, struct{ label, value string }{
			"Charge limit", chargeLimitVal,
		})
	}
	rows = append(rows, []struct{ label, value string }{
		{"Manufacturer", availableText(health.Vendor)},
		{"Model", availableText(health.Model)},
		{"Technology", availableText(health.Technology)},
		{"Design capacity", formatCapacity(health.DesignCapacity)},
		{"Full charge capacity", formatCapacity(health.FullCapacity)},
	}...)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return titleBlock(gtx, state.theme, "Battery Health", "Capacity and device information")
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(22)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return state.card(gtx, 0, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(18), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return secondary(state.theme, "Health").Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(7)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := material.H4(state.theme, healthValue)
							label.Color = palette.text
							label.Font.Weight = font.Bold
							return label.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return secondary(state.theme, healthInterpretation(health)).Layout(gtx)
						}),
					)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return state.card(gtx, 0, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					children := make([]layout.FlexChild, 0, len(rows))
					for index, row := range rows {
						index, row := index, row
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return state.detailRow(gtx, row.label, row.value, index < len(rows)-1)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			})
		}),
	)
}

func (state *UIState) detailRow(gtx layout.Context, label, value string, divider bool) layout.Dimensions {
	height := gtx.Dp(50)
	dimensions := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return secondary(state.theme, label).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			valueLabel := body(state.theme, value)
			valueLabel.Font.Weight = font.Medium
			return valueLabel.Layout(gtx)
		}),
	)
	if divider {
		fillRect(gtx, image.Rect(0, height-1, gtx.Constraints.Max.X, height), palette.border)
	}
	dimensions.Size.Y = height
	return dimensions
}

func (state *UIState) layoutAbout(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return titleBlock(gtx, state.theme, "About BATI", "System information and shortcuts")
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(22)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return state.card(gtx, 500, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(24), Bottom: unit.Dp(24), Left: unit.Dp(28), Right: unit.Dp(28)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Title & Purpose
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return heading(state.theme, appName).Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return secondary(state.theme, appTagline).Layout(gtx)
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return body(state.theme, "BATI is a lightweight Linux battery health and history visualizer. It runs locally and keeps your energy telemetry private.").Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						// Local first / Privacy note
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := material.Body2(state.theme, aboutPrivacyText)
							label.Color = palette.green
							label.Font.Weight = font.Medium
							return label.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := material.Body2(state.theme, aboutSignature)
							label.Color = palette.muted
							label.Font.Weight = font.Medium
							return label.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
						// Divider
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return filledBox(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Dp(1)), palette.border)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
						// System Details
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											label := material.Body2(state.theme, "COMPONENTS")
											label.Font.Weight = font.Bold
											label.Color = palette.text
											return label.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return secondary(state.theme, "• bati (GUI application)\n• batid (telemetry daemon)\n• batictl (control utility)").Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											label := material.Body2(state.theme, "DATA SOURCES")
											label.Font.Weight = font.Bold
											label.Color = palette.text
											return label.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return secondary(state.theme, "• sysfs (/sys/class/power_supply)\n• UPower interface\n• systemd-logind (sleep events)").Layout(gtx)
										}),
									)
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									// Keyboard Shortcuts
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											label := material.Body2(state.theme, "KEYBOARD SHORTCUTS")
											label.Font.Weight = font.Bold
											label.Color = palette.text
											return label.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return secondary(state.theme, " [1]   Overview page\n [2]   Usage History page\n [3]   Battery Health page\n [4]   About BATI page\n [R]   Refresh telemetry\n [Esc] Hide chart tooltip").Layout(gtx)
										}),
									)
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
						// Divider
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return filledBox(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Dp(1)), palette.border)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
						// Database and status info
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							dbText := "Database path: " + state.dbPath
							freshness := state.currentFreshness()
							statusText := "Status: " + freshness.Title + " (" + strings.ToLower(freshness.Detail) + ")"
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return secondary(state.theme, dbText).Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return secondary(state.theme, statusText).Layout(gtx)
								}),
							)
						}),
					)
				})
			})
		}),
	)
}

func (state *UIState) card(gtx layout.Context, height int, content layout.Widget) layout.Dimensions {
	if height <= 0 {
		macro := op.Record(gtx.Ops)
		gtx.Constraints.Min = image.Point{}
		dimensions := content(gtx)
		call := macro.Stop()

		size := image.Pt(gtx.Constraints.Max.X, dimensions.Size.Y)
		rectangle := clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(14))
		paint.FillShape(gtx.Ops, palette.card, rectangle.Op(gtx.Ops))

		call.Add(gtx.Ops)

		border := clip.Stroke{Path: rectangle.Path(gtx.Ops), Width: 1}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: palette.border}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		border.Pop()

		dimensions.Size.X = size.X
		return dimensions
	}

	pixels := gtx.Dp(unit.Dp(height))
	size := image.Pt(gtx.Constraints.Max.X, pixels)
	rectangle := clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(14))
	paint.FillShape(gtx.Ops, palette.card, rectangle.Op(gtx.Ops))
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.Y = pixels
	dimensions := content(gtx)
	border := clip.Stroke{Path: rectangle.Path(gtx.Ops), Width: 1}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: palette.border}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	border.Pop()
	dimensions.Size = size
	return dimensions
}

func (state *UIState) layoutLoadState(gtx layout.Context) layout.Dimensions {
	message := "Loading..."
	if state.loadErr != nil {
		message = "Battery data is not available"
	}
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return secondary(state.theme, message).Layout(gtx)
	})
}

func titleBlock(gtx layout.Context, theme *material.Theme, title, subtitle string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return heading(theme, title).Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return secondary(theme, subtitle).Layout(gtx)
		}),
	)
}

func drawRoundedRect(gtx layout.Context, size image.Point, radius int, fill, border color.NRGBA) {
	rectangle := clip.UniformRRect(image.Rectangle{Max: size}, radius)
	paint.FillShape(gtx.Ops, fill, rectangle.Op(gtx.Ops))
	stack := clip.Stroke{Path: rectangle.Path(gtx.Ops), Width: 1}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: border}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func filledBox(gtx layout.Context, size image.Point, colour color.NRGBA) layout.Dimensions {
	paint.FillShape(gtx.Ops, colour, clip.Rect{Max: size}.Op())
	return layout.Dimensions{Size: size}
}

func isPluggedIn(status string) bool {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(status)), " ", "_") {
	case "charging", "full", "not_charging", "not charging":
		return true
	default:
		return false
	}
}

func isChargeHoldingStatus(status string) bool {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(status)), " ", "_") {
	case "not_charging", "not charging", "full", "unknown":
		return true
	default:
		return false
	}
}
