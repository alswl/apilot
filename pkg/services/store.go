package services

import (
	"time"

	"github.com/alswl/apilot/pkg/common"
)

type Store interface {
	Close() error
	SaveRun(r common.Run) error
	Runs() ([]common.Run, error)
	DeleteRun(id string) error
	SaveTask(t common.Task) error
	Tasks() ([]common.Task, error)
	DeleteTask(id string) error
	Observers() ([]common.Observer, error)
	SaveObserver(v common.Observer) error
	DeleteObserver(id string) error
	SaveEvent(v common.AttentionEvent) error
	Events() ([]common.AttentionEvent, error)
	DeleteEvent(id string) error
	AllowNotification(key string, now time.Time) (bool, error)
	Settings() (common.Settings, error)
	SaveSettings(v common.Settings) error
}
