package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/sender"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/templates"
)

var (
	sendIcon    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/></svg>`))
	fileIcon    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h8"/></svg>`))
	bookIcon    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>`))
	serverIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><path d="M6 7h.01M6 17h.01"/></svg>`))
	historyIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12a9 9 0 1 0 2.5-6.2L3 8"/><path d="M3 3v5h5M12 7v5l3 2"/></svg>`))
)

func (a *application) sidebar(c *ui.Context) {
	side := ui.Column(c).Width(220).Shrink(0)
	if !c.Vibrancy() {
		side.Background(ui.Hex("#F0F1F5"))
	}
	side.Children(func() {
		ui.Box(c).Height(titleHeight).DragWindow()
		chosen := a.page
		list := ui.Sidebar(c, &chosen, func() {
			ui.SidebarSection(c, "日志测试", nil, func() {
				ui.SidebarItem(c, "single", sendIcon, "单条发送")
				ui.SidebarItem(c, "txt", fileIcon, "TXT 导入")
				ui.SidebarItem(c, "templates", bookIcon, "日志模板")
			})
			ui.SidebarSection(c, "资料库", nil, func() {
				ui.SidebarItem(c, "targets", serverIcon, "接收目标")
				ui.SidebarItem(c, "history", historyIcon, "发送历史")
			})
		}).Grow(1).Label("功能导航")
		if c.Vibrancy() {
			list.Background(ui.Transparent)
		}
		if list.Changed() {
			a.page = chosen
			a.status = ""
		}
		ui.Text(c, "Syslog Tester · Native UI").Padding(14, 18).FontSize(11).TextColor(c.Theme().TextMuted)
	})
}
func panel(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).Padding(18).Gap(12).Radius(12).Background(ui.Hex("#FFFFFF")).Border(1, ui.Hex("#E5E7ED")).Children(func() {
		ui.Text(c, title).FontSize(15).Bold().TextColor(t.Text)
		body()
	})
}
func head(c *ui.Context, title, subtitle string) {
	ui.Text(c, title).FontSize(27).Bold()
	ui.Text(c, subtitle).FontSize(12).TextColor(c.Theme().TextMuted)
}
func (a *application) targetPicker(c *ui.Context) {
	targets := a.db.Snapshot().Targets
	options := make([]string, 0, len(targets))
	ids := make(map[string]string, len(targets))
	selected := ""
	for _, t := range targets {
		label := fmt.Sprintf("%s · %s · %s", t.Name, t.Address(), strings.ToUpper(string(t.Protocol)))
		if t.Protocol == "tcp" {
			label += " · " + string(t.Framing)
		}
		if len(t.ID) >= 6 {
			label += " · " + t.ID[:6]
		}
		options = append(options, label)
		ids[label] = t.ID
		if t.ID == a.selectedID {
			selected = label
		}
	}
	panel(c, "接收目标", func() {
		if len(options) == 0 {
			ui.Text(c, "还没有接收目标，请先在左侧「接收目标」中添加。").TextColor(c.Theme().TextMuted)
			if ui.Button(c, "前往配置").Clicked() {
				a.page = "targets"
			}
			return
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.Select(c, &selected, options).Grow(1).Label("接收目标").Changed() {
				a.updateTarget(ids[selected])
			}
			if ui.Button(c, "管理目标").Clicked() {
				a.page = "targets"
			}
		})
	})
}
func (a *application) parameters(c *ui.Context) {
	panel(c, "发送参数", func() {
		ui.Row(c).Gap(24).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, "重复 / 循环次数")
			ui.NumberInput(c, &a.loops, 1, 10000, 1).Width(128)
			ui.Text(c, "发送间隔 (ms)")
			ui.NumberInput(c, &a.interval, 0, 60000, 10).Width(128)
		})
		ui.Text(c, "单任务顺序发送，无自动重试；Socket 写入成功不等于目标平台已采集。").FontSize(12).TextColor(c.Theme().TextMuted)
	})
}
func (a *application) sendControls(c *ui.Context, launch func()) {
	running := a.stats.State == sender.Running || a.stats.State == sender.Paused
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		if running {
			if ui.Button(c, "取消发送").Clicked() {
				a.runner.Cancel()
			}
			if a.stats.State == sender.Running {
				if ui.Button(c, "暂停").Clicked() {
					if e := a.runner.Pause(); e != nil {
						a.status = e.Error()
					} else {
						a.stats = a.runner.Snapshot()
					}
				}
			}
			if a.stats.State == sender.Paused {
				if ui.Button(c, "继续").Clicked() {
					if e := a.runner.Resume(); e != nil {
						a.status = e.Error()
					} else {
						a.stats = a.runner.Snapshot()
					}
				}
			}
		} else {
			btn := ui.Row(c).Padding(10, 18).Radius(21).Material(glass.Glass{Tint: c.Theme().Accent, Interactive: true}).Children(func() {
				ui.Icon(c, sendIcon).Size(17, 17).TextColor(ui.Hex("#FFFFFF"))
				ui.Text(c, "开始发送").Bold().TextColor(ui.Hex("#FFFFFF"))
			})
			if btn.Clicked() {
				launch()
			}
		}
	})
	if a.status != "" {
		ui.Text(c, a.status).FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
	}
	if a.stats.State != sender.Idle {
		s := a.stats
		panel(c, "本次任务", func() {
			ui.Row(c).Gap(18).Wrap().Children(func() {
				ui.Textf(c, "状态：%s", s.State)
				ui.Textf(c, "尝试：%d", s.Attempted)
				ui.Textf(c, "写入成功：%d", s.Sent)
				ui.Textf(c, "失败：%d", s.FailedCount)
				ui.Textf(c, "字节：%d", s.Bytes)
			})
			if s.LastError != "" {
				ui.Text(c, "详情："+s.LastError).FontSize(12).TextColor(ui.Hex("#B43A45")).Wrap()
			}
		})
	}
}
func (a *application) singlePage(c *ui.Context) {
	head(c, "单条日志发送", "粘贴完整 Syslog 原始日志，或者让应用生成标准消息头。")
	a.targetPicker(c)
	panel(c, "日志内容", func() {
		ui.Segmented(c, &a.format, "原始透传", "RFC3164", "RFC5424").Label("消息格式")
		ui.TextArea(c, &a.message).Height(210).Placeholder("粘贴或输入 Syslog 日志 ...").Label("日志内容")
		if a.format != 0 {
			ui.Row(c).Gap(14).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, "Facility")
				ui.NumberInput(c, &a.facility, 0, 23, 1).Width(100)
				ui.Text(c, "Severity")
				ui.NumberInput(c, &a.severity, 0, 7, 1).Width(100)
			})
			ui.Text(c, "将自动生成时间戳、hostname 和 app-name；原始透传不会修改日志正文。").FontSize(12).TextColor(c.Theme().TextMuted)
		}
	})
	a.parameters(c)
	a.sendControls(c, a.sendSingle)
}
func (a *application) txtPage(c *ui.Context) {
	head(c, "TXT 批量发送", "每行一条 Syslog 日志，或者将整个 TXT 文件作为单条消息发送。")
	a.targetPicker(c)
	panel(c, "TXT 文件", func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "选择 TXT 文件").Clicked() {
				a.pickTXT()
			}
			if a.txtPath != "" {
				ui.Text(c, filepath.Base(a.txtPath)).TextColor(c.Theme().TextMuted).SingleLine()
			}
		})
		zone := ui.Column(c).Padding(14).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
			ui.Text(c, "拖入 .txt 文件，或使用上方按钮选择").FontSize(12).TextColor(c.Theme().TextMuted)
		})
		if paths := zone.DroppedFiles(); len(paths) > 0 {
			a.loadTXT(paths[0])
		}
		ui.Segmented(c, &a.txtMode, "每行一条", "整个文件作为一条").Label("读取方式")
		if a.previewStatus != "" {
			ui.Text(c, a.previewStatus).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if len(a.preview) > 0 {
			ui.Column(c).Gap(5).Padding(12).Radius(8).Background(ui.Hex("#F7F8FA")).Children(func() {
				for i, line := range a.preview {
					s := line
					if len(s) > 220 {
						s = s[:220] + "…"
					}
					ui.Textf(c, "%02d   %s", i+1, s).Font("monospace").FontSize(12).SingleLine()
				}
			})
		}
	})
	a.parameters(c)
	a.sendControls(c, a.sendTXT)
}
func (a *application) templatesPage(c *ui.Context) {
	head(c, "日志模板", "快速生成常见安全设备与 Linux 系统测试事件。")
	for _, item := range templates.All() {
		item := item
		panel(c, item.Name, func() {
			ui.Text(c, item.Description).FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Text(c, item.Message).Font("monospace").FontSize(12).Wrap()
			if ui.Button(c, "使用此模板").Clicked() {
				a.message = item.Message
				a.format = 1
				a.page = "single"
			}
		})
	}
}
func (a *application) targetsPage(c *ui.Context) {
	head(c, "接收目标", "统一配置 Syslog 服务器；发送页面只需要选择目标。")
	data := a.db.Snapshot()
	panel(c, fmt.Sprintf("已保存的接收目标 · %d", len(data.Targets)), func() {
		ui.SearchField(c, &a.targetFilter).Placeholder("搜索名称、IP 或协议").Label("查找接收目标")
		if len(data.Targets) == 0 {
			ui.Text(c, "暂无目标，请在下方添加。").TextColor(c.Theme().TextMuted)
		}
		for _, target := range data.Targets {
			term := strings.ToLower(strings.TrimSpace(a.targetFilter))
			if term != "" && !strings.Contains(strings.ToLower(target.Name+" "+target.Host+" "+string(target.Protocol)), term) {
				continue
			}
			target := target
			ui.Row(c).Padding(12, 8).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, serverIcon).Size(18, 18).TextColor(c.Theme().Accent)
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					name := target.Name
					if target.Default {
						name += " · 默认"
					}
					ui.Text(c, name).Bold()
					ui.Textf(c, "%s · %s", target.Address(), strings.ToUpper(string(target.Protocol))).FontSize(12).TextColor(c.Theme().TextMuted)
				})
				if ui.Button(c, "选择").Disabled(a.selectedID == target.ID).Clicked() {
					a.updateTarget(target.ID)
				}
				if ui.Button(c, "编辑").Clicked() {
					a.editTarget(target)
				}
				if ui.Button(c, "复制").Clicked() {
					a.editTarget(target)
					a.editingID = ""
					a.form.Name = target.Name + " 副本"
					a.form.Default = false
				}
				if ui.Button(c, "删除").Clicked() {
					if e := a.db.Delete(target.ID); e != nil {
						a.status = e.Error()
					} else {
						a.status = "已删除目标"
						a.selectedID = a.db.Snapshot().SelectedID
					}
				}
			})
			ui.Divider(c)
		}
	})
	title := "添加接收目标"
	if a.editingID != "" {
		title = "编辑接收目标"
	}
	panel(c, title, func() {
		ui.Form(c, func() {
			ui.Field(c, "名称", func() { ui.TextInput(c, &a.form.Name).Placeholder("态势感知平台 UDP") })
			ui.Field(c, "IP / Hostname", func() { ui.TextInput(c, &a.form.Host).Placeholder("192.0.2.100") })
			ui.Field(c, "端口", func() { ui.TextInput(c, &a.form.Port).Placeholder("514") })
			ui.Field(c, "协议", func() { ui.Select(c, &a.form.Protocol, []string{"udp", "tcp"}) })
			if a.form.Protocol == "tcp" {
				ui.Field(c, "TCP 分帧", func() { ui.Select(c, &a.form.Framing, []string{"octet-counting", "lf", "crlf"}) })
			}
			ui.Field(c, "本地源 IP（可选）", func() { ui.TextInput(c, &a.form.LocalIP).Placeholder("由系统自动选择") })
			ui.Field(c, "连接总超时 (ms)", func() { ui.TextInput(c, &a.form.ConnectTimeoutMS).Placeholder("5000") })
			ui.Field(c, "写入超时 (ms)", func() { ui.TextInput(c, &a.form.WriteTimeoutMS).Placeholder("5000") })
			ui.Checkbox(c, &a.form.Default, "设为默认目标")
		})
		ui.Row(c).Gap(10).Children(func() {
			if ui.PrimaryButton(c, "保存目标").Clicked() {
				a.saveTarget()
			}
			if a.editingID != "" {
				if ui.Button(c, "取消编辑").Clicked() {
					a.editingID = ""
					a.form = blankTargetForm()
				}
			}
		})
	})
	if a.status != "" {
		ui.Text(c, a.status).FontSize(12).TextColor(c.Theme().TextMuted)
	}
}
func (a *application) historyPage(c *ui.Context) {
	data := a.db.Snapshot()
	head(c, "发送历史", fmt.Sprintf("最近 %d 条任务记录，最多保留 %d 条。", len(data.History), 300))
	if len(data.History) == 0 {
		panel(c, "没有发送记录", func() {
			ui.Text(c, "完成一次发送后会在这里保留任务摘要。").TextColor(c.Theme().TextMuted)
		})
		return
	}
	panel(c, "历史任务", func() {
		for _, record := range data.History {
			r := record
			row := ui.Row(c).Padding(10).Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, r.Stats.Target.Name+" · "+strings.ToUpper(string(r.Stats.Target.Protocol))).Bold()
					ui.Text(c, r.Stats.Started.Format("2006-01-02 15:04:05")).TextColor(c.Theme().TextMuted).FontSize(12)
				})
				ui.Textf(c, "%d / %d", r.Stats.Sent, r.Stats.Attempted)
				ui.Text(c, string(r.Stats.State)).FontSize(12)
			})
			if row.Clicked() {
				a.details = record.ID
			}
			if a.details == record.ID {
				ui.Column(c).Padding(12).Gap(7).Background(c.Theme().Surface).Radius(8).Children(func() {
					ui.Textf(c, "接收地址：%s", r.Stats.Target.Address())
					ui.Textf(c, "写入字节：%d · 失败：%d", r.Stats.Bytes, r.Stats.FailedCount)
					if r.Stats.Finished.After(r.Stats.Started) {
						ui.Textf(c, "耗时：%s", r.Stats.Finished.Sub(r.Stats.Started).Round(time.Millisecond))
					}
					if r.Stats.LastError != "" {
						ui.Text(c, "错误："+r.Stats.LastError).Wrap()
					}
					ui.Text(c, "说明：此处仅记录 Socket 写入状态，非平台采集确认。").FontSize(12).TextColor(c.Theme().TextMuted)
				})
			}
			ui.Divider(c)
		}
	})
}
