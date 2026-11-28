package workbench

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestNowReviewsOpenMatchingGitHubSource(t *testing.T) {
	oldLanguage := englishUI.Load()
	t.Cleanup(func() { englishUI.Store(oldLanguage) })
	for _, english := range []bool{false, true} {
		englishUI.Store(english)
		m := newMockup()
		size := [2]int{1190, 730}
		if english {
			size = [2]int{680, 580}
		}
		main := ui.NewTester(m.view, size[0], size[1])
		if !main.HasText(tr("待我评审的 GitHub PR")) {
			t.Fatal("the sample Now entry lacks the review list")
		}
		var opened string
		reviews := ui.NewTester(func(c *ui.Context) {
			m.nowReviewRequests(c, func(link string) error {
				opened = link
				return errors.New("fixture browser error")
			})
		}, size[0], size[1])
		for _, item := range sampleNowReviews {
			label := fmt.Sprintf(tr("在 GitHub 中打开 %s · %s"), item.Repository+" #42", tr(item.Title))
			click(t, reviews, label)
			if opened != "https://github.com/"+item.Repository+"/pull/42" {
				t.Fatalf("opened incorrect PR: %s", opened)
			}
			if !reviews.HasText(tr("无法打开 GitHub")) {
				t.Fatal("missing external action failure")
			}
		}
		if len(m.nowSession().turns) != 0 || m.page != nowPage || len(m.objects) != len(newMockup().objects) {
			t.Fatal("opening a PR changed the conversation or task view")
		}
	}
}

func TestNowReviewsShowLiveStatesAndClearOnScopeChange(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	render := func() *ui.Tester {
		return ui.NewTester(func(c *ui.Context) {
			m.nowReviewRequests(c, func(string) error { return nil })
		}, 680, 580)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.nowReviews.cancel = cancel
	if !render().HasText(tr("正在读取 GitHub 评审请求…")) {
		t.Fatal("missing loading status")
	}

	m.clearNowReviews()
	m.nowReviews.problem = "UNAUTHORIZED"
	preview := render()
	if !preview.HasText(tr("连接仓库或重新授权后，即可查看评审请求。")) {
		t.Fatal("missing authorization status")
	}
	click(t, preview, tr("查看连接器"))
	if m.page != connectionsPage {
		t.Fatal("authorization action did not open Connections")
	}
	m.navigate(nowPage)
	if !render().HasText(tr("已连接的仓库没有待你评审的 PR。")) {
		t.Fatal("missing empty state")
	}
	m.nowReviews.problem = "RATE_LIMITED"
	if !render().HasText(tr("GitHub 请求已达上限，请稍后重试。")) {
		t.Fatal("missing rate limit state")
	}
	m.nowReviews.problem = ""
	m.nowReviews.result = common.WorkbenchGitHubReviewRequests{Items: sampleNowReviews[:1], Incomplete: true}
	preview = render()
	if !preview.HasText(tr("部分仓库的评审请求暂不可用。")) ||
		!preview.HasText("example/payments #42") || preview.HasText("example/checkout #42") {
		t.Fatal("live Now must use only actual response rows and mark partial results")
	}
	scope := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{
		ID: "repo", Kind: managedGitHub, Version: 1,
	}}
	m.observeNowReviewConnections([]common.WorkbenchSnapshot{scope})
	ctx, m.nowReviews.cancel = context.WithCancel(context.Background())
	m.nowReviews.timer = time.AfterFunc(time.Hour, func() {})
	scope.AccessState = "unavailable"
	m.observeNowReviewConnections([]common.WorkbenchSnapshot{scope})
	if ctx.Err() == nil || m.nowReviews.cancel != nil || m.nowReviews.timer != nil ||
		len(m.nowReviews.result.Items) != 0 || render().HasText("example/payments #42") {
		t.Fatal("permission change retained old review data or an in-flight request")
	}
}
