package workbench

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const nowReviewInterval = 2 * time.Minute

type nowReviewConnection struct {
	version          int64
	access, login    string
	paused, disabled bool
}

type nowReviews struct {
	connections map[string]nowReviewConnection
	cancel      context.CancelFunc
	timer       *time.Timer
	request     uint64
	result      common.WorkbenchGitHubReviewRequests
	problem     string
	openFailed  string
}

func (m *mockup) clearNowReviews() {
	reviews := &m.nowReviews
	if reviews.cancel != nil {
		reviews.cancel()
		reviews.cancel = nil
	}
	if reviews.timer != nil {
		reviews.timer.Stop()
		reviews.timer = nil
	}
	reviews.request++
	reviews.result = common.WorkbenchGitHubReviewRequests{}
	reviews.problem, reviews.openFailed = "", ""
}

func (m *mockup) observeNowReviewConnections(snapshots []common.WorkbenchSnapshot) {
	current := make(map[string]nowReviewConnection)
	for _, snapshot := range snapshots {
		if snapshot.Kind == common.WorkbenchGitHubRepository || snapshot.Kind == managedGitHub {
			current[snapshot.ID] = nowReviewConnection{
				version: snapshot.Version, access: snapshot.AccessState, login: snapshot.AccountLogin,
				paused: snapshot.Paused, disabled: snapshot.PluginDisabled,
			}
		}
	}
	previous := m.nowReviews.connections
	m.nowReviews.connections = current
	if previous != nil && !maps.Equal(previous, current) {
		m.clearNowReviews()
		m.invalidateLiveWindow()
	}
}

func (m *mockup) loadNowReviews() {
	if m.live == nil || m.win == nil || m.win.IsDestroyed() || m.liveDisconnected ||
		m.page != nowPage || m.nowLibraryOpen {
		return
	}
	m.clearNowReviews()
	reviews := &m.nowReviews
	request := reviews.request
	ctx, cancel := context.WithCancel(context.Background())
	reviews.cancel = cancel
	remote := m.live
	go func() {
		result, err := remote.WorkbenchGitHubReviewRequests(ctx)
		mygo.RunOnMain(func() {
			if reviews.request != request {
				return
			}
			cancel()
			reviews.cancel = nil
			reviews.result = result
			if err != nil {
				reviews.problem = common.ProtocolError(err).Code
			}
			reviews.timer = time.AfterFunc(nowReviewInterval, func() {
				mygo.RunOnMain(func() {
					if reviews.request == request {
						m.loadNowReviews()
					}
				})
			})
			m.invalidateLiveWindow()
		})
	}()
	m.invalidateLiveWindow()
}

var sampleNowReviews = []common.WorkbenchGitHubReviewRequest{
	{Repository: "example/payments", Number: 42, Title: "补充退款回调的重试测试"},
	{Repository: "example/checkout", Number: 42, Title: "在结账页显示退款进度"},
}

func (m *mockup) nowReviewRequests(c *ui.Context, openSource func(string) error) {
	reviews := &m.nowReviews
	items := reviews.result.Items
	if m.live == nil {
		if m.framework {
			return
		}
		items = sampleNowReviews
	} else if reviews.cancel == nil && reviews.timer == nil {
		m.loadNowReviews()
	}

	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c.Key("header")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			components.Heading(c, tr("待我评审的 GitHub PR")).Grow(1)
			if m.live != nil && reviews.cancel == nil && !m.liveDisconnected && reviews.problem != "UNAUTHORIZED" {
				if components.IconButton(c.Key("refresh"), "arrow.clockwise", tr("刷新评审请求"), false).Clicked() {
					m.loadNowReviews()
					items = nil
				}
			}
		})
		switch {
		case m.liveDisconnected:
			components.Muted(c, tr("正在重新连接 Apilot…"))
		case reviews.cancel != nil:
			components.Muted(c, tr("正在读取 GitHub 评审请求…"))
		case reviews.problem == "UNAUTHORIZED":
			m.connectionGuidance(c.Key("authorize"), tr("连接仓库或重新授权后，即可查看评审请求。"))
		case reviews.problem == "FORBIDDEN" || reviews.problem == "NOT_FOUND":
			m.connectionGuidance(c.Key("permissions"), tr("请检查仓库访问权限和 Pull requests 读取权限。"))
		case reviews.problem == "RATE_LIMITED":
			components.Muted(c, tr("GitHub 请求已达上限，请稍后重试。"))
		case reviews.problem == "CONFLICT":
			components.Muted(c, tr("请确认已连接的 GitHub 仓库使用同一账号，然后刷新。"))
		case reviews.problem != "":
			components.Muted(c, tr("无法读取 GitHub 评审请求。"))
		case reviews.result.Incomplete:
			components.Muted(c, tr("部分仓库的评审请求暂不可用。"))
		case len(items) == 0:
			components.Muted(c, tr("已连接的仓库没有待你评审的 PR。"))
		}
		for _, item := range items {
			m.nowReviewRow(c, item, openSource)
		}
		ui.Divider(c.Key("divider"))
	})
}

func (m *mockup) nowReviewRow(c *ui.Context, item common.WorkbenchGitHubReviewRequest, openSource func(string) error) {
	number := fmt.Sprint(item.Number)
	link := githubSourceURL(item.Repository, "pull", number)
	if link == "" {
		return
	}
	location := item.Repository + " #" + number
	title := m.sourceText(item.Title)
	key := c.Key(link)
	ui.Column(key).FillWidth().Gap(4).Children(func() {
		label := fmt.Sprintf(tr("在 GitHub 中打开 %s · %s"), location, title)
		button := ui.ButtonBase(key.Key("open")).Label(label).Tooltip(label).
			FillWidth().MinHeight(44).Padding(4, 0).Justify(ui.Start)
		button.Children(func() {
			ui.Row(key.Key("content")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Column(key.Key("text")).Grow(1).MinWidth(0).Gap(4).Children(func() {
					components.DataText(c, title).Underline()
					components.Muted(c, location)
				})
				components.Symbol(key, "arrow.up.right", c.Theme().TextMuted)
			})
		})
		if button.Clicked() {
			m.nowReviews.openFailed = ""
			if openSource(link) != nil {
				m.nowReviews.openFailed = link
			}
		}
		if m.nowReviews.openFailed == link {
			components.Muted(key.Key("error"), tr("无法打开 GitHub"))
		}
	})
}
