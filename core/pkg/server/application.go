package server

import (
	"context"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/dal"
	"github.com/alswl/apilot/core/pkg/services"
)

type Runtime struct {
	Service       *services.Service
	SocketPath    string
	server        *Server
	workbenchHTTP *workbenchHTTP
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	once          sync.Once
	err           error
}

func Open(dir string) (*Runtime, error) {
	s, e := OpenService(dir)
	if e != nil {
		return nil, e
	}
	socket := config.SocketPath(dir)
	server, e := Listen(s, socket)
	if e != nil {
		s.Close()
		return nil, e
	}
	workbenchHTTP, err := listenWorkbenchHTTP(s, config.HTTPSocketPath(dir))
	if err != nil {
		server.Close()
		s.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{Service: s, SocketPath: socket, server: server, workbenchHTTP: workbenchHTTP, cancel: cancel}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_ = s.Prune()
				timer.Reset(24 * time.Hour)
			}
		}
	}()
	return r, nil
}
func (r *Runtime) StopAdmission() { r.Service.StopAdmission(); r.cancel() }
func (r *Runtime) StopIPC()       { r.workbenchHTTP.close(); r.server.Close() }

func (r *Runtime) Close() error {
	r.once.Do(func() { r.StopAdmission(); r.StopIPC(); r.wg.Wait(); r.err = r.Service.Close() })
	return r.err
}

func OpenService(dir string) (*services.Service, error) {
	settings, err := config.LoadObsidianSync(dir)
	options := services.ObsidianSyncOptions{
		ScanInterval: settings.ScanInterval, ScanTimeout: settings.ScanTimeout,
		MaxRetries: settings.MaxRetries, ScanTimeoutConfigured: settings.ScanTimeoutConfigured,
	}
	if err != nil {
		options.Diagnostic = err.Error()
	}
	return services.NewWithWorkbenchOptions(dir, func(path string) (services.Store, error) { return dal.Open(path) }, services.BuiltinWorkbenchPluginRegistry(), options)
}
