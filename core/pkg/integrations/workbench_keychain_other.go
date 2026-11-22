//go:build !darwin || !cgo

package integrations

import (
	"context"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchKeychain struct{}

// NewWorkbenchKeychain returns an unavailable store on unsupported platforms.
func NewWorkbenchKeychain(_ string) WorkbenchCredentialStore { return workbenchKeychain{} }

func (workbenchKeychain) Put(context.Context, string, string) error {
	return common.Err("SOURCE_UNAVAILABLE", "System credential storage is unavailable")
}
func (workbenchKeychain) Replace(context.Context, string, string) error {
	return common.Err("SOURCE_UNAVAILABLE", "System credential storage is unavailable")
}
func (workbenchKeychain) Get(context.Context, string) (string, error) {
	return "", common.Err("SOURCE_UNAVAILABLE", "System credential storage is unavailable")
}
func (workbenchKeychain) Delete(context.Context, string) error {
	return common.Err("SOURCE_UNAVAILABLE", "System credential storage is unavailable")
}
func (workbenchKeychain) List(context.Context) ([]string, error) {
	return nil, common.Err("SOURCE_UNAVAILABLE", "System credential storage is unavailable")
}
