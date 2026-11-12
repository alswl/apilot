package services

import "github.com/alswl/apilot/pkg/dal"

func openService(dir string) (*Service, error) {
	return New(dir, func(path string) (Store, error) { return dal.Open(path) })
}
