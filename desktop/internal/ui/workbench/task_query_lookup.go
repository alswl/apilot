package workbench

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alswl/apilot/core/pkg/integrations"
)

const (
	taskLookupStableID = "stable-id"
	taskLookupIssue    = "repository-number"
)

func (view *taskQueryView) setGitHubLookup(mode, stableID, repository, number string) error {
	switch mode {
	case taskLookupStableID:
		stableID = strings.TrimSpace(stableID)
		if stableID == "" {
			return fmt.Errorf("%s", tr("请输入稳定来源 ID"))
		}
		view.StableSourceID, view.IssueRepository, view.IssueNumber = stableID, "", ""
	case taskLookupIssue:
		repository, number = strings.TrimSpace(repository), strings.TrimSpace(number)
		if _, ok := integrations.GitHubRepositoryParts(repository); !ok {
			return fmt.Errorf("%s", tr("仓库格式应为 owner/repository"))
		}
		parsed, err := strconv.ParseUint(number, 10, 64)
		if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != number {
			return fmt.Errorf("%s", tr("Issue 编号必须是正整数"))
		}
		view.StableSourceID, view.IssueRepository, view.IssueNumber = "", repository, number
	default:
		return fmt.Errorf("%s", tr("请选择查找方式"))
	}
	return nil
}

func (view *taskQueryView) clearGitHubLookup() {
	view.StableSourceID, view.IssueRepository, view.IssueNumber = "", "", ""
}

func taskQueryLookupSummary(view taskQueryView) string {
	switch {
	case view.StableSourceID != "":
		return fmt.Sprintf(tr("Issue ID %s"), view.StableSourceID)
	case view.IssueRepository != "" && view.IssueNumber != "":
		return fmt.Sprintf("%s #%s", view.IssueRepository, view.IssueNumber)
	default:
		return ""
	}
}
