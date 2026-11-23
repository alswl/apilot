module github.com/alswl/apilot/desktop

go 1.27.3

require (
	github.com/alswl/apilot/core v0.0.0
	github.com/egoist/mygo v0.8.4-0.20261111042631-b4882ccc2592
	golang.org/x/sys v0.50.0
)

require (
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/alswl/apilot/connectors/protocol v0.0.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/danielgtaylor/huma/v2 v2.41.0 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-chi/chi/v5 v5.5.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/mattn/go-sqlite3 v1.14.53 // indirect
	github.com/oapi-codegen/runtime v1.9.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

replace (
	github.com/alswl/apilot/connectors/protocol => ../connectors/protocol
	github.com/alswl/apilot/core => ../core
)

tool github.com/egoist/mygo/cmd/mygo
