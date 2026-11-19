package integrations

import (
	_ "embed"
	"encoding/json"

	"github.com/alswl/apilot/pkg/common"
)

//go:embed workbench_github_fixture.json
var workbenchGitHubFixture []byte

func ReadWorkbenchGitHubMock(c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	var tasks []common.WorkbenchTask
	if err := json.Unmarshal(workbenchGitHubFixture, &tasks); err != nil {
		return nil, err
	}
	for i := range tasks {
		tasks[i].SourceKey = c.Repository + "#" + tasks[i].NativeID
	}
	return tasks, nil
}
