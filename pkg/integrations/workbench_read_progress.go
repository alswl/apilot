package integrations

import (
	"context"

	"github.com/alswl/apilot/pkg/common"
)

type workbenchReadProgressKey struct{}

func WithWorkbenchReadProgress(ctx context.Context, report func(common.WorkbenchReadProgress)) context.Context {
	return context.WithValue(ctx, workbenchReadProgressKey{}, report)
}

func reportWorkbenchReadProgress(ctx context.Context, read, total int) {
	if report, ok := ctx.Value(workbenchReadProgressKey{}).(func(common.WorkbenchReadProgress)); ok {
		report(common.WorkbenchReadProgress{Read: read, Total: total})
	}
}
