package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

type fixtureConfig struct {
	FullOnlyScope    string `json:"full_only_scope"`
	TemporaryScope   string `json:"temporary_scope"`
	ReadOnlyTask     string `json:"read_only_task"`
	RateLimitedScope string `json:"rate_limited_scope"`
	PartialScope     string `json:"partial_scope"`
	DetailError      string `json:"detail_error"`
	RemovedField     string `json:"removed_field"`
	WriteResult      string `json:"write_result"`
	LoginDelayMS     int    `json:"login_delay_ms"`
	BrowserURL       string `json:"browser_url"`
	LoginResult      string `json:"login_result"`
	Account          string `json:"account"`
	DeniedScope      string `json:"denied_scope"`
	FailSecondPage   string `json:"fail_second_page"`
	UnverifiedScope  string `json:"unverified_scope"`
	EmptyScope       string `json:"empty_scope"`
}

const fixtureEndpoint = "fixture.local"

func fixtureHandler(ctx context.Context, req protocol.Request, notify protocol.Notify) (any, *protocol.RPCError) {
	config := fixtureConfig{LoginDelayMS: 50, Account: "fixture-account"}
	data, err := os.ReadFile(filepath.Join(os.Getenv("APILOT_CONNECTOR_DATA_DIR"), "fixture.json"))
	if (err == nil && json.Unmarshal(data, &config) != nil) || (err != nil && !errors.Is(err, os.ErrNotExist)) {
		return nil, protocol.Problem(protocol.InvalidConfig, "", "Invalid local fixture configuration")
	}
	switch req.Method {
	case protocol.Login:
		var p protocol.LoginParams
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		if p.EndpointRef != "" && p.EndpointRef != fixtureEndpoint {
			return nil, protocol.Problem(protocol.InvalidConfig, "endpoint", "Unknown fixture endpoint")
		}
		verification := config.BrowserURL
		if verification == "" {
			verification = "https://localhost:8421/fixture-login"
		}
		target, err := url.Parse(verification)
		if err != nil || target.Scheme != "https" || target.User != nil || target.Path != "/fixture-login" ||
			(target.Hostname() != "localhost" && !net.ParseIP(target.Hostname()).IsLoopback()) {
			return nil, protocol.Problem(protocol.InvalidConfig, "", "Fixture verification URL must use loopback HTTPS")
		}
		if config.LoginResult == "manual" {
			if !validApprovalID(req.ID) {
				return nil, protocol.Problem(protocol.InvalidConfig, "", "Fixture approval request ID is invalid")
			}
			dir := os.Getenv("APILOT_CONNECTOR_DATA_DIR")
			pending := filepath.Join(dir, "pending-"+req.ID)
			if err := os.WriteFile(pending, nil, 0600); err != nil {
				return nil, protocol.Problem(protocol.Temporary, "account", "Fixture login is unavailable")
			}
			defer os.Remove(pending)
			approval := filepath.Join(dir, "approved-"+req.ID)
			defer os.Remove(approval)
			target.RawQuery = url.Values{"id": {req.ID}}.Encode()
			if err := notify("open_browser", protocol.BrowserNotification{RequestID: req.ID, URL: target.String(), UserCode: "FIXT-URE1"}); err != nil {
				return nil, protocol.Problem(protocol.Temporary, "account", "Login notification failed")
			}
			tick := time.NewTicker(50 * time.Millisecond)
			defer tick.Stop()
			for {
				if _, err := os.Stat(approval); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					return nil, protocol.Problem(protocol.Temporary, "account", "Fixture login interrupted")
				case <-tick.C:
				}
			}
		} else if err := notify("open_browser", protocol.BrowserNotification{RequestID: req.ID, URL: target.String(), UserCode: "FIXT-URE1"}); err != nil {
			return nil, protocol.Problem(protocol.Temporary, "", "Login notification failed")
		}
		timer := time.NewTimer(time.Duration(config.LoginDelayMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, protocol.Problem(protocol.Temporary, "account", "Fixture login interrupted")
		case <-timer.C:
		}
		switch config.LoginResult {
		case "expired":
			return nil, protocol.ExpiredAuthorization("Fixture authorization expired")
		case "denied":
			return nil, protocol.Problem(protocol.AccessDenied, "account", "Fixture authorization declined")
		}
		if p.AccountRef != "" && p.AccountRef != config.Account {
			return nil, protocol.Problem(protocol.AccessDenied, "account", "Original fixture account is required")
		}
		return protocol.Account{AccountRef: config.Account, EndpointRef: fixtureEndpoint, DisplayName: "Fixture Account"}, nil
	case protocol.Options:
		var p protocol.OptionsParams
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		if problem := config.checkAccount(p.AccountRef, p.EndpointRef); problem != nil {
			return nil, problem
		}
		if p.Field != "project" {
			return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Unknown fixture option field")
		}
		result := protocol.ConfigOptions{Items: []protocol.ConfigOption{}}
		switch {
		case p.Search != "":
			for _, name := range []string{"PAY", "OPS"} {
				if strings.Contains(name, strings.ToUpper(p.Search)) {
					result.Items = append(result.Items, protocol.ConfigOption{Value: name, Label: name})
				}
			}
		case p.Page == "":
			result.Items = append(result.Items, protocol.ConfigOption{Value: "PAY", Label: "PAY"})
			result.NextPage = "ops"
		case p.Page == "ops":
			result.Items = append(result.Items, protocol.ConfigOption{Value: "OPS", Label: "OPS"})
		default:
			return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Unknown fixture option page")
		}
		return result, nil
	case protocol.Validate:
		var p protocol.ValidateParams
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		if problem := config.checkAccount(p.AccountRef, p.EndpointRef); problem != nil {
			return nil, problem
		}
		var scope struct {
			Project string `json:"project"`
		}
		var endpointConfig map[string]json.RawMessage
		if json.Unmarshal(p.EndpointConfig, &endpointConfig) != nil || endpointConfig == nil || len(endpointConfig) != 0 ||
			(p.Mode != "static" && p.Mode != "access") ||
			json.Unmarshal(p.ScopeConfig, &scope) != nil || (scope.Project != "PAY" && scope.Project != "OPS") || p.ScopeSchemaVersion != 1 || p.EndpointSchemaVersion != 1 {
			return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Choose PAY or OPS")
		}
		return protocol.ValidatedScope{ScopeRef: scope.Project, EndpointRef: fixtureEndpoint, Label: scope.Project}, nil
	case protocol.EditMetadata:
		var p protocol.DetailParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		if err := config.checkScope(p.ScopeContext); err != nil {
			return nil, err
		}
		if p.SourceID == "" {
			return protocol.EditMetadataResult{Fields: []protocol.FieldDefinition{}, Actions: []protocol.StateAction{}}, nil
		}
		if p.SourceID != "1" && p.SourceID != "2" {
			return nil, protocol.Problem(protocol.AccessDenied, "object", "Fixture task unavailable")
		}
		if config.ReadOnlyTask == p.ScopeRef+"-"+p.SourceID {
			return protocol.EditMetadataResult{Fields: []protocol.FieldDefinition{}, Actions: []protocol.StateAction{}}, nil
		}
		return fixtureEditMetadata(fixtureCurrent(p.ScopeRef, p.SourceID)), nil
	case protocol.Mutate, protocol.Reconcile:
		var p protocol.MutationParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		return config.mutation(ctx, p, req.Method == protocol.Reconcile)
	case protocol.FieldsDescribe:
		var p protocol.ScopeContext
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		if problem := config.checkScope(p); problem != nil {
			return nil, problem
		}
		catalog := fixtureCatalog()
		if config.RemovedField != "" {
			catalog.Fields = slices.DeleteFunc(catalog.Fields, func(f protocol.FieldDefinition) bool { return f.ID == config.RemovedField })
			for id, rule := range catalog.Defaults {
				if rule.Field == config.RemovedField {
					delete(catalog.Defaults, id)
				}
			}
		}
		return catalog, nil
	case protocol.TaskDetail:
		var p protocol.DetailParams
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		return config.detail(p)
	case protocol.Sync:
		var p protocol.SyncParams
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		return config.sync(p)
	case protocol.Resolve:
		var p protocol.ResolveParams
		if problem := protocol.DecodeParams(req, &p); problem != nil {
			return nil, problem
		}
		if problem := config.checkScope(p.ScopeContext); problem != nil {
			return nil, problem
		}
		result := protocol.ResolveResult{Items: []protocol.Resolution{}, Usage: protocol.Usage{Requests: 1}}
		for _, id := range p.SourceIDs {
			resolved := protocol.Resolution{SourceID: id, Status: "out_of_scope"}
			if p.ScopeRef == config.UnverifiedScope {
				resolved.Status = "unverified"
			}
			if p.ScopeRef != config.EmptyScope && (id == "1" || id == "2") {
				record := fixtureSyncRecord(p.ScopeRef, id, p.Fields)
				resolved.Status, resolved.Record = "present", &record
				result.Usage.Received++
			}
			result.Items = append(result.Items, resolved)
		}
		return result, nil
	default:
		return nil, protocol.Problem(protocol.Unsupported, "", "Method not supported")
	}
}

func (c fixtureConfig) checkAccount(account, endpoint string) *protocol.RPCError {
	if account != c.Account || endpoint != fixtureEndpoint {
		return protocol.Problem(protocol.AuthRequired, "account", "Fixture authorization is unavailable")
	}
	return nil
}

func (c fixtureConfig) checkScope(p protocol.ScopeContext) *protocol.RPCError {
	if problem := c.checkAccount(p.AccountRef, p.EndpointRef); problem != nil {
		return problem
	}
	if p.ScopeRef != "PAY" && p.ScopeRef != "OPS" {
		return protocol.Problem(protocol.InvalidConfig, "scope", "Unknown fixture scope")
	}
	if p.ScopeRef == c.DeniedScope {
		return protocol.Problem(protocol.AccessDenied, "scope", "Fixture scope access revoked")
	}
	return nil
}

func (c fixtureConfig) sync(p protocol.SyncParams) (any, *protocol.RPCError) {
	if problem := c.checkScope(p.ScopeContext); problem != nil {
		problem.Data.Usage = &protocol.Usage{Requests: 1}
		return nil, problem
	}
	if p.ScopeRef == c.TemporaryScope {
		return nil, protocol.Problem(protocol.Temporary, "account", "Fixture OAuth temporarily unavailable")
	}
	if p.ScopeRef == c.FullOnlyScope && p.Mode == "delta" {
		return nil, protocol.Problem(protocol.Unsupported, "scope", "Fixture supports only full sync")
	}
	if p.ScopeRef == c.RateLimitedScope {
		retry := time.Now().UTC().Add(30 * time.Second)
		problem := protocol.Problem(protocol.RateLimited, "scope", "Fixture scope rate limited")
		problem.Data.RetryAt = &retry
		problem.Data.RateLimitBucket = "fixture-read"
		problem.Data.Usage = &protocol.Usage{Requests: 1, RateLimits: 1}
		return nil, problem
	}
	if p.ScopeRef == c.PartialScope && p.Page != "" {
		return nil, protocol.Problem(protocol.Temporary, "scope", "Fixture read interrupted without complete usage")
	}
	if p.Mode != "full" && p.Mode != "delta" {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Unknown fixture sync mode")
	}
	checkpoint := "checkpoint-" + p.ScopeRef
	watermark := time.Now().UTC()
	var previous time.Time
	if p.Mode == "delta" {
		switch {
		case p.Checkpoint == checkpoint:
		case strings.HasPrefix(p.Checkpoint, checkpoint+"@"):
			parsed, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(p.Checkpoint, checkpoint+"@"))
			if err != nil {
				return nil, protocol.Problem(protocol.InvalidCheckpoint, "scope", "Fixture checkpoint is invalid")
			}
			previous = parsed
		default:
			return nil, protocol.Problem(protocol.InvalidCheckpoint, "scope", "Fixture checkpoint is invalid")
		}
	}
	usage := protocol.Usage{Requests: 1, Pages: 1}
	result := protocol.SyncPage{Records: []protocol.SourceRecord{}, Coverage: p.Mode, NextCheckpoint: checkpoint + "@" + watermark.Format(time.RFC3339Nano), Usage: usage}
	if p.ScopeRef == c.EmptyScope {
		if p.ScopeRef == c.FullOnlyScope {
			result.NextCheckpoint = ""
		}
		return result, nil
	}
	switch {
	case p.Mode == "delta":
		for _, id := range []string{"1", "2"} {
			record := fixtureSyncRecord(p.ScopeRef, id, p.Fields)
			if record.SourceVersion != "v1" && record.UpdatedAt.After(previous) && !record.UpdatedAt.After(watermark) {
				result.Records = append(result.Records, record)
			}
		}
	case p.Page == "":
		result.Records = append(result.Records, fixtureSyncRecord(p.ScopeRef, "1", p.Fields))
		result.NextPage, result.Coverage, result.NextCheckpoint = "second@"+watermark.Format(time.RFC3339Nano), "", ""
	case strings.HasPrefix(p.Page, "second@"):
		from, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(p.Page, "second@"))
		if err != nil {
			return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Unknown fixture sync page")
		}
		if p.ScopeRef == c.FailSecondPage {
			problem := protocol.Problem(protocol.Temporary, "scope", "Fixture page temporarily unavailable")
			problem.Data.Usage = &usage
			return nil, problem
		}
		result.NextCheckpoint = checkpoint + "@" + from.Format(time.RFC3339Nano)
		result.Records = append(result.Records, fixtureSyncRecord(p.ScopeRef, "2", p.Fields))
	default:
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Unknown fixture sync page")
	}
	if p.ScopeRef == c.FullOnlyScope {
		result.NextCheckpoint = ""
	}
	result.Usage.Received = len(result.Records)
	return result, nil
}

func fixtureRecord(scope, id string, fields []string) protocol.SourceRecord {
	record := protocol.SourceRecord{
		SourceID: id, SourceVersion: "v1", Number: scope + "-" + id, Title: fmt.Sprintf("%s task %s", scope, id),
		State: "open", StateID: "todo", StateLabel: "To do", UpdatedAt: time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC),
	}
	record.Fields = map[string]protocol.FieldValue{
		"title":       protocol.Value(protocol.FieldText, record.Title),
		"description": protocol.Value(protocol.FieldRichText, "Fixture task **description**"),
		"assignees":   protocol.Value(protocol.FieldAccounts, []protocol.Option{{ID: "alice", Label: "Alice"}}),
		"priority":    protocol.Value(protocol.FieldNumber, 2),
		"due_date":    protocol.Value(protocol.FieldDate, nil),
		"promised":    protocol.Value(protocol.FieldDate, "2026-12-01"),
		"labels":      protocol.Value(protocol.FieldLabels, []string{"fixture"}),
		"status":      protocol.Value(protocol.FieldText, "todo"),
	}
	if id == "1" {
		record.Fields["due_date"] = protocol.Value(protocol.FieldDate, "2026-12-15")
	}
	if len(fields) > 0 {
		for key := range record.Fields {
			if key == "status" || key == "title" {
				continue
			}
			if !slices.Contains(fields, key) {
				delete(record.Fields, key)
			}
		}
	}
	return record
}

func fixtureSyncRecord(scope, id string, fields []string) protocol.SourceRecord {
	record := fixtureCurrent(scope, id)
	if len(fields) > 0 {
		for key := range record.Fields {
			if key != "status" && key != "title" && !slices.Contains(fields, key) {
				delete(record.Fields, key)
			}
		}
	}
	return record
}
