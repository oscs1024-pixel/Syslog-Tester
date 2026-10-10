package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/sender"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/source"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/store"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/syslog"
)

type application struct {
	win           *mygo.Window
	db            *store.Store
	runner        *sender.Runner
	page          string
	selectedID    string
	message       string
	format        int
	facility      float64
	severity      float64
	loops         float64
	interval      float64
	txtPath       string
	txtMode       int
	preview       []string
	previewStatus string
	stats         sender.Stats
	status        string
	form          targetForm
	editingID     string
	targetFilter  string
	details       string
}
type targetForm struct {
	Name, Host, Port, Protocol, Framing, LocalIP, ConnectTimeoutMS, WriteTimeoutMS string
	Default                                                                        bool
}

func blankTargetForm() targetForm {
	return targetForm{Port: "514", Protocol: "udp", Framing: "octet-counting", ConnectTimeoutMS: "5000", WriteTimeoutMS: "5000"}
}
func parseTimeoutMS(s string) (time.Duration, error) {
	ms, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || ms < 1 || ms > 60000 {
		return 0, fmt.Errorf("超时需为 1-60000 ms")
	}
	return time.Duration(ms) * time.Millisecond, nil
}
func newApplication(db *store.Store) *application {
	data := db.Snapshot()
	return &application{db: db, runner: sender.New(), page: "single", selectedID: data.SelectedID,
		message: "<134>Oct 10 12:00:00 fw01 firewall: action=deny src=192.0.2.10 dst=198.51.100.20",
		format:  0, facility: 16, severity: 6, loops: 1, txtMode: 0, form: blankTargetForm()}
}

func (a *application) target() (domain.Target, error) {
	for _, t := range a.db.Snapshot().Targets {
		if t.ID == a.selectedID {
			return t.Normalize()
		}
	}
	return domain.Target{}, fmt.Errorf("请先在接收目标页面配置并选择目标")
}
func (a *application) updateTarget(id string) {
	if err := a.db.Select(id); err != nil {
		a.status = err.Error()
		return
	}
	a.selectedID = id
}
func (a *application) start(factory source.Factory) {
	target, err := a.target()
	if err != nil {
		a.status = err.Error()
		return
	}
	loops := int(a.loops)
	if loops < 1 || loops > 10000 {
		a.status = "循环次数必须在 1-10000 之间"
		return
	}
	interval := time.Duration(a.interval * float64(time.Millisecond))
	if interval < 0 || interval > time.Hour {
		a.status = "发送间隔无效"
		return
	}
	// 快照和回调都通过 Window.Update 回到 GUI 主线程；持久化放在后台。
	done, err := a.runner.Start(context.Background(), sender.Job{Target: target, Source: factory, Loops: loops, Interval: interval, StopOnError: true}, func(stats sender.Stats) {
		if a.win != nil {
			a.win.Update(func() { a.stats = stats })
		}
	})
	if err != nil {
		a.status = err.Error()
		return
	}
	a.status = "发送任务已启动"
	go func() {
		<-done
		result := a.runner.Snapshot()
		saveErr := a.db.Append(result)
		if a.win != nil {
			a.win.Update(func() {
				if saveErr != nil {
					a.status = "历史保存失败: " + saveErr.Error()
				} else {
					a.status = "任务完成：" + string(result.State)
				}
			})
		}
	}()
}
func (a *application) sendSingle() {
	if a.message == "" {
		a.status = "请输入日志内容"
		return
	}
	var payload []byte
	var err error
	if a.format == 0 {
		payload = []byte(a.message)
	} else {
		if a.facility < 0 || a.facility > 23 || a.severity < 0 || a.severity > 7 || math.Trunc(a.facility) != a.facility || math.Trunc(a.severity) != a.severity {
			a.status = "Facility 或 Severity 超出范围"
			return
		}
		format := syslog.RFC3164
		if a.format == 2 {
			format = syslog.RFC5424
		}
		payload, err = syslog.Build(format, syslog.Fields{Facility: uint8(a.facility), Severity: uint8(a.severity), Hostname: "syslog-tester", AppName: "syslog-tester", Message: a.message})
	}
	if err != nil {
		a.status = err.Error()
		return
	}
	a.start(source.Single(payload))
}
func (a *application) pickTXT() {
	// 系统文件对话框是阻塞操作，因此放到 goroutine，不阻塞 MyGo 绘制。
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "选择 TXT 日志文件", Filters: []mygo.FileFilter{{Name: "文本日志", Extensions: []string{"txt"}}}})
		if a.win == nil {
			return
		}
		if err != nil {
			a.win.Update(func() { a.status = err.Error() })
			return
		}
		if len(paths) == 0 {
			return
		}
		a.loadTXT(paths[0])
	}()
}

// loadTXT 在后台读取有限条预览，避免阻塞 UI 绘制。
func (a *application) loadTXT(path string) {
	go func() {
		if !strings.EqualFold(filepath.Ext(path), ".txt") {
			a.win.Update(func() { a.previewStatus = "仅支持 TXT 文件" })
			return
		}
		preview := make([]string, 0, 30)
		rd, err := source.TXT(path, source.Lines)()
		if err == nil {
			defer rd.Close()
			for len(preview) < 30 {
				b, e := rd.Next(context.Background())
				if e == io.EOF {
					break
				}
				if e != nil {
					err = e
					break
				}
				preview = append(preview, string(b))
			}
		}
		size := int64(0)
		if info, e := os.Stat(path); e == nil {
			size = info.Size()
		}
		a.win.Update(func() {
			if err != nil {
				a.previewStatus = err.Error()
				return
			}
			a.txtPath = path
			a.preview = preview
			a.previewStatus = fmt.Sprintf("文件大小 %d 字节 · 已预览 %d 条", size, len(preview))
		})
	}()
}

func (a *application) sendTXT() {
	if a.txtPath == "" {
		a.status = "请先导入 TXT 文件"
		return
	}
	mode := source.Lines
	if a.txtMode == 1 {
		mode = source.Whole
	}
	a.start(source.TXT(a.txtPath, mode))
}
func (a *application) saveTarget() {
	port, err := strconv.Atoi(strings.TrimSpace(a.form.Port))
	if err != nil || port < 1 || port > 65535 {
		a.status = "端口范围必须为 1-65535"
		return
	}
	connect, e := parseTimeoutMS(a.form.ConnectTimeoutMS)
	if e != nil {
		a.status = "连接" + e.Error()
		return
	}
	write, e := parseTimeoutMS(a.form.WriteTimeoutMS)
	if e != nil {
		a.status = "写入" + e.Error()
		return
	}
	t := domain.Target{ID: a.editingID, Name: a.form.Name, Host: a.form.Host, Port: uint16(port), Protocol: domain.Protocol(a.form.Protocol), Framing: domain.Framing(a.form.Framing), LocalIP: strings.TrimSpace(a.form.LocalIP), ConnectTimeout: connect, WriteTimeout: write, Default: a.form.Default}
	saved, err := a.db.Upsert(t)
	if err != nil {
		a.status = err.Error()
		return
	}
	if a.selectedID == "" || saved.Default {
		a.selectedID = saved.ID
	}
	a.editingID = ""
	a.form = blankTargetForm()
	a.status = "目标已保存"
}
func (a *application) editTarget(t domain.Target) {
	a.editingID = t.ID
	a.form = targetForm{Name: t.Name, Host: t.Host, Port: strconv.Itoa(int(t.Port)), Protocol: string(t.Protocol), Framing: string(t.Framing), LocalIP: t.LocalIP,
		ConnectTimeoutMS: strconv.FormatInt(t.ConnectTimeout.Milliseconds(), 10), WriteTimeoutMS: strconv.FormatInt(t.WriteTimeout.Milliseconds(), 10), Default: t.Default}
	if a.form.Framing == "" {
		a.form.Framing = "octet-counting"
	}
}

const titleHeight = float32(52)

func (a *application) view(c *ui.Context) {
	// 每次视图重新生成时都使用固定的浅色主题，拒绝跟随系统暗色模式。
	theme := ui.LightTheme()
	theme.Background = ui.Hex("#FAFBFD")
	theme.Surface = ui.Hex("#F3F4F8")
	theme.Accent = ui.Hex("#D84370")
	theme.AccentHover = ui.Hex("#C73565")
	theme.Radius = 10
	c.SetTheme(theme)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		ui.Box(c).Width(1).Background(ui.Hex("#E1E4EB"))
		ui.Column(c).Grow(1).MinWidth(0).Background(theme.Background).Children(func() {
			a.toolbar(c)
			ui.Scroll(c).Grow(1).Padding(22, 30, 28).Gap(18).Children(func() { a.pageView(c) })
		})
	})
}
func (a *application) toolbar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(titleHeight).Shrink(0).Padding(0, c.TitleBar().Right+22, 0, 26).AlignItems(ui.Center).Gap(12).DragWindow().Children(func() {
		ui.Text(c, "Syslog Tester").FontSize(15).Bold().Grow(1)
		ui.Text(c, "UDP / TCP").FontSize(12).TextColor(t.TextMuted)
		b := ui.Row(c).Padding(8, 12).Radius(18).Material(glass.Glass{Style: glass.Regular, Interactive: true}).Children(func() { ui.Text(c, "接收目标").FontSize(12) })
		if b.Clicked() {
			a.page = "targets"
		}
	})
	ui.Divider(c)
}
func (a *application) pageView(c *ui.Context) {
	switch a.page {
	case "single":
		a.singlePage(c)
	case "txt":
		a.txtPage(c)
	case "templates":
		a.templatesPage(c)
	case "targets":
		a.targetsPage(c)
	case "history":
		a.historyPage(c)
	default:
		a.page = "single"
		a.singlePage(c)
	}
}
