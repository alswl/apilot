package services

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/dal"
)

func TestWorkbenchTaskQueryFullSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := dal.Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{store: db}
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	local := common.WorkbenchConnection{ID: "local", Kind: common.WorkbenchLocal, Name: "Local", Version: 1}
	github := common.WorkbenchConnection{ID: "repo", Kind: common.WorkbenchGitHubRepository, Name: "GitHub", Version: 1}
	if err := db.SaveWorkbenchGitHub(github, 0, common.WorkbenchGitHubProgress{
		ConnectionID: github.ID, RepositoryID: 10, AccountID: 20, AccountLogin: "synthetic", CredentialRef: "fixture", AccessState: "available",
	}); err != nil {
		t.Fatal(err)
	}

	estimate := []string{"999999999999.123456", "-999999999999.123455", "0", ""}
	var localTasks, githubTasks []common.WorkbenchTask
	for i := range 205 {
		id := fmt.Sprintf("task-%03d", i)
		task := common.WorkbenchTask{ID: id, SourceKey: id, NativeID: "1", Title: "Same title", State: "open",
			ConnectionID: local.ID, Kind: local.Kind, Labels: []string{"project/lbkit"},
			LabelValues: map[string]string{"estimate": estimate[i%4], "area": " Desktop "}, DueDate: "2026-03-08"}
		if i%4 == 3 {
			delete(task.LabelValues, "area")
			delete(task.LabelValues, "estimate")
			task.State = "in_progress"
		}
		if i%4 < 2 {
			task.ConnectionID, task.Kind = github.ID, github.Kind
			task.Labels = append(task.Labels, "area/CLI")
			task.NativeFields = map[string]string{"repository": "fixture/lbkit", "unlisted": "unlisted value"}
			githubTasks = append(githubTasks, task)
		} else {
			localTasks = append(localTasks, task)
		}
	}
	if err := db.RecordWorkbench(local, nil, nil, localTasks, &now); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitWorkbenchGitHubSync(github, nil, nil, githubTasks, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkbenchLabels("task-003", []string{"workspace/urgent", "area/ignored"}); err != nil {
		t.Fatal(err)
	}
	query := common.WorkbenchTaskQuery{
		Timezone:   "America/New_York",
		Expression: `"project/lbkit" in task.labels && task.status in ["open", "in_progress"] && has(task.due) && task.due >= week_start && task.due < next_week_start`,
		Fields: []common.WorkbenchQueryField{
			{ID: "area-id", Key: "area", Name: "Field", Type: "single_select", Mappings: map[string]common.WorkbenchQueryMapping{
				"local": {Kind: "key_value_label", Key: "area"}, "repo": {Kind: "label_prefix", Key: "area/"},
			}},
			{ID: "estimate-id", Key: "estimate", Name: "Field", Type: "number", Mappings: map[string]common.WorkbenchQueryMapping{
				"local": {Kind: "key_value_label", Key: "estimate"}, "repo": {Kind: "key_value_label", Key: "estimate"},
			}},
		},
		Display: common.WorkbenchQueryDisplay{Layout: "board", SwimlaneBy: "area-id", SliceBy: "task.source_id", SumBy: "estimate-id"},
		Limit:   100,
	}
	read := func(query common.WorkbenchTaskQuery) common.WorkbenchTaskQueryResult {
		t.Helper()
		result, err := service.queryWorkbenchTasks(ctx, query, clock)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := read(query)
	if first.QueryTotal != 205 || first.SliceTotal != 205 || len(first.Items) != 100 || len(first.Columns) != 5 || len(first.Cells) != 3 {
		t.Fatalf("incomplete aggregate: %d/%d, columns=%+v cells=%+v", first.SliceTotal, first.QueryTotal, first.Columns, first.Cells)
	}
	if column := first.Columns[0]; column.Count != 154 || column.Valid != 154 || column.Sum == nil || *column.Sum != "999999999999.123507" {
		t.Fatalf("sum lost precision: %+v", column)
	}
	if column := first.Columns[1]; column.Count != 51 || column.Sum != nil || column.Valid != 0 {
		t.Fatalf("missing sum: %+v", column)
	}
	if first.Lanes[len(first.Lanes)-1].Count != 51 || !first.Lanes[len(first.Lanes)-1].Missing {
		t.Fatalf("missing lane: %+v", first.Lanes)
	}

	continuation := query
	continuation.RequiredRevision, continuation.AsOfDate, continuation.PageToken = first.Revision, first.AsOfDate, first.PageToken
	seen := make(map[string]bool)
	for offset := 0; offset < 205; offset += 100 {
		continuation.Offset = offset
		page := read(continuation)
		if len(page.Items) != min(100, 205-offset) || !reflect.DeepEqual(page.Columns, first.Columns) || !reflect.DeepEqual(page.Slices, first.Slices) {
			t.Fatalf("page %d lost full totals", offset)
		}
		for _, row := range page.Items {
			if seen[row.Task.ID] || row.Task.Title != "Same title" || row.Task.ConnectionVersion != 1 {
				t.Fatalf("bad task identity or version: %+v", row.Task)
			}
			seen[row.Task.ID] = true
		}
	}
	if len(seen) != 205 {
		t.Fatalf("seen %d IDs", len(seen))
	}
	assertStale := func(input common.WorkbenchTaskQuery) {
		t.Helper()
		if _, err := service.queryWorkbenchTasks(ctx, input, clock); err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
			t.Fatalf("want stale continuation, got %v", err)
		}
	}
	changed := continuation
	changed.Expression = "true"
	assertStale(changed)
	now = now.AddDate(0, 0, 1)
	assertStale(continuation)
	now = now.AddDate(0, 0, -1)
	localID := "local"
	sliced := query
	sliced.SliceValue = &localID
	sliced.Display.Fields = []string{"estimate-id"}
	selection := read(sliced)
	if selection.QueryTotal != 205 || selection.SliceTotal != 102 || selection.Columns[0].Sum == nil ||
		*selection.Columns[0].Sum != "0" || selection.Columns[1].Sum != nil || len(selection.Items[0].Fields) != 2 {
		t.Fatalf("slice or field projection: %d/%d %+v", selection.SliceTotal, selection.QueryTotal, selection.Columns)
	}
	if value := selection.Items[0].Fields["estimate-id"]; value.Number == nil || *value.Number != 0 {
		t.Fatalf("zero value missing: %+v", value)
	}

	for _, descending := range []bool{false, true} {
		list := query
		list.Display = common.WorkbenchQueryDisplay{SortBy: "estimate-id", Descending: descending}
		list.Limit = 1
		result := read(list)
		want := "task-001"
		if descending {
			want = "task-000"
		}
		if result.Items[0].Task.ID != want {
			t.Fatalf("descending %t: first task is %s, want %s", descending, result.Items[0].Task.ID, want)
		}
		list.Offset, list.RequiredRevision, list.AsOfDate, list.PageToken = 204, result.Revision, result.AsOfDate, result.PageToken
		if value := read(list).Items[0].Fields["estimate-id"]; value.Reason != "missing" {
			t.Fatalf("descending %t: missing value did not sort last", descending)
		}
	}

	filter := query
	filter.Expression = `custom.area == "cli" || (has(custom.estimate) && custom.estimate == 0.0)`
	if result := read(filter); result.QueryTotal != 154 || !slices.Equal(result.FieldDependencies, []string{"area-id", "estimate-id"}) {
		t.Fatalf("CEL or dependencies: %d %v", result.QueryTotal, result.FieldDependencies)
	}
	filter.Expression = `!has(custom.area) && "workspace/urgent" in task.labels`
	if result := read(filter); result.QueryTotal != 1 || result.Items[0].Task.ID != "task-003" {
		t.Fatalf("aggregated labels or missing fields: %d", result.QueryTotal)
	}

	rename := github
	rename.Name, rename.Version = "Renamed", 2
	if err := db.SaveWorkbench(rename, false); err != nil {
		t.Fatal(err)
	}
	assertStale(continuation)
	boundaries := &queryBoundaryStore{Store: db}
	duringQuery := &Service{store: boundaries}
	warm, err := duringQuery.queryWorkbenchTasks(ctx, query, clock)
	if err != nil || warm.QueryTotal != 205 {
		t.Fatalf("could not warm projection: %d, %v", warm.QueryTotal, err)
	}
	warm.Items[0].Task.LabelValues["estimate"] = "999"
	delete(warm.Items[0].Fields, "estimate-id")
	again, err := duringQuery.queryWorkbenchTasks(ctx, query, clock)
	if err != nil || boundaries.fullReads != 1 || again.Items[0].Fields["estimate-id"].Number == nil ||
		again.Items[0].Task.LabelValues["estimate"] == "999" {
		t.Fatalf("cached response aliased caller values or reread the snapshot: reads=%d, err=%v", boundaries.fullReads, err)
	}
	boundaries.beforeBoundary = func() error { return db.InvalidateWorkbenchGitHubAccess(rename, 1) }
	failingQuery := query
	failingQuery.Expression = `native["absent"] == ""`
	if result, err := duringQuery.queryWorkbenchTasks(ctx, failingQuery, clock); err == nil || common.ProtocolError(err).Code != "STALE_REVISION" ||
		len(result.Items) != 0 || len(result.Sources) != 0 {
		t.Fatalf("permission changed during an invalid query: %v / %+v", err, result)
	}
	if boundaries.fullReads != 1 {
		t.Fatal("permission change did not exercise a warmed cache")
	}
	assertStale(continuation)
	visible := read(query)
	if visible.QueryTotal != 102 || len(visible.Slices) != 1 || visible.Slices[0].Key != "local" || len(visible.Sources) != 1 {
		t.Fatalf("revoked data included: %d, slices=%v sources=%v", visible.QueryTotal, visible.Slices, visible.Sources)
	}
	filter.SourceIDs = []string{"repo"}
	filter.Expression = "true"
	if result := read(filter); result.QueryTotal != 0 {
		t.Fatalf("explicit source constraint enlarged visibility: %d", result.QueryTotal)
	}
}

func TestWorkbenchTaskQueryCrossSourceLabelsAndExactGitHubIDs(t *testing.T) {
	ctx := context.Background()
	db, err := dal.Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{store: db}
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	obsidian := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Name: "Vault", Version: 1}
	if err := db.CommitWorkbenchImport(obsidian, nil, nil, []common.WorkbenchTask{{ID: "obs-task", SourceKey: "obs-key", NativeID: "obs-key", ConnectionID: "vault", Kind: common.WorkbenchObsidianTasks, Title: "超时 release", State: "open", Labels: []string{"#release"}}}, &now); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkbenchLabels("obs-task", []string{"release"}); err != nil {
		t.Fatal(err)
	}
	githubA := common.WorkbenchConnection{ID: "repo-a", Kind: common.WorkbenchGitHubRepository, Name: "Repo A", Repository: "owner/a", Version: 1}
	githubB := common.WorkbenchConnection{ID: "repo-b", Kind: common.WorkbenchGitHubRepository, Name: "Repo B", Repository: "owner/b", Version: 1}
	for i, connection := range []common.WorkbenchConnection{githubA, githubB} {
		if err := db.SaveWorkbenchGitHub(connection, 0, common.WorkbenchGitHubProgress{ConnectionID: connection.ID, RepositoryID: int64(10 + i), AccountID: 20, AccountLogin: "fixture", CredentialRef: "fixture", AccessState: "available"}); err != nil {
			t.Fatal(err)
		}
	}
	githubTasks := []common.WorkbenchTask{
		{ID: "issue-17", SourceKey: "stable-17", NativeID: "17", ConnectionID: "repo-a", Kind: githubA.Kind, Title: "修复登录重试", State: "open", Labels: []string{"release"}, NativeFields: map[string]string{"repository": "owner/a"}},
		{ID: "issue-170", SourceKey: "stable-170", NativeID: "170", ConnectionID: "repo-a", Kind: githubA.Kind, Title: "Issue 170", State: "open", NativeFields: map[string]string{"repository": "owner/a"}},
		{ID: "issue-other-17", SourceKey: "stable-other-17", NativeID: "17", ConnectionID: "repo-b", Kind: githubB.Kind, Title: "Issue 17 elsewhere", State: "open", NativeFields: map[string]string{"repository": "owner/b"}},
		{ID: "issue-title-17", SourceKey: "stable-title-17", NativeID: "18", ConnectionID: "repo-a", Kind: githubA.Kind, Title: "Release 17 in title", State: "open", NativeFields: map[string]string{"repository": "owner/a"}},
		{ID: "issue-closed", SourceKey: "stable-closed", NativeID: "19", ConnectionID: "repo-a", Kind: githubA.Kind, Title: "Closed release", State: "done", Labels: []string{"release"}, NativeFields: map[string]string{"repository": "owner/a"}},
	}
	for _, entry := range []struct {
		connection common.WorkbenchConnection
		tasks      []common.WorkbenchTask
	}{{githubA, []common.WorkbenchTask{githubTasks[0], githubTasks[1], githubTasks[3], githubTasks[4]}}, {githubB, []common.WorkbenchTask{githubTasks[2]}}} {
		if err := db.RecordWorkbench(entry.connection, nil, nil, entry.tasks, &now); err != nil {
			t.Fatal(err)
		}
	}
	query := func(input common.WorkbenchTaskQuery) common.WorkbenchTaskQueryResult {
		t.Helper()
		input.Timezone = "UTC"
		result, err := service.QueryWorkbenchTasks(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	fresh := common.WorkbenchTask{ID: "fresh", Kind: common.WorkbenchObsidianTasks, Labels: []string{"#release"}}
	freshProjection := projectWorkbenchQueryTask(fresh, obsidian, nil, nil, time.UTC)
	if !slices.Contains(freshProjection.item.Fields["task.labels"].Values, "release") || fresh.Labels[0] != "#release" {
		t.Fatalf("fresh projection changed source label or missed shared value: %+v", freshProjection.item.Fields["task.labels"])
	}
	label := query(common.WorkbenchTaskQuery{Expression: `"release" in task.labels`, States: []string{"open"}})
	if label.QueryTotal != 2 {
		ids := make([]string, len(label.Items))
		for i, item := range label.Items {
			ids[i] = item.Task.ID
		}
		t.Fatalf("shared label query got %d tasks: %v", label.QueryTotal, ids)
	}
	matchedIDs := make(map[string]bool, len(label.Items))
	for _, item := range label.Items {
		matchedIDs[item.Task.ID] = true
		if item.Task.ID == "obs-task" && (!slices.Contains(item.Task.Labels, "#release") || len(item.Task.WorkbenchLabels) != 1 || item.Task.WorkbenchLabels[0].Name != "release") {
			t.Fatalf("query changed source values or workspace association: %+v", item.Task)
		}
	}
	if !matchedIDs["obs-task"] || !matchedIDs["issue-17"] {
		t.Fatalf("shared label query did not return one task per source: %v", matchedIDs)
	}
	keyword := query(common.WorkbenchTaskQuery{Expression: `"release" in task.labels`, Keyword: "#release", States: []string{"open"}})
	if keyword.QueryTotal != 1 || keyword.Items[0].Task.ID != "obs-task" {
		t.Fatalf("literal keyword should preserve original source value: %+v", keyword.Items)
	}
	keyword = query(common.WorkbenchTaskQuery{Expression: `"release" in task.labels`, Keyword: "release", States: []string{"open"}})
	if keyword.QueryTotal != 2 {
		t.Fatalf("literal keyword and label filter failed to combine: %d", keyword.QueryTotal)
	}
	keyword = query(common.WorkbenchTaskQuery{Expression: `"release" in task.labels`, Keyword: "超时", States: []string{"open"}})
	if keyword.QueryTotal != 1 || keyword.Items[0].Task.ID != "obs-task" {
		t.Fatalf("title search did not narrow the combined source query: %+v", keyword.Items)
	}
	if restored := query(common.WorkbenchTaskQuery{Expression: `"release" in task.labels`, States: []string{"open"}}); restored.QueryTotal != 2 {
		t.Fatalf("clearing keyword did not restore the shared-label result: %d", restored.QueryTotal)
	}
	if empty := query(common.WorkbenchTaskQuery{Expression: `"release" in task.labels`, Keyword: "never matches", States: []string{"open"}}); empty.QueryTotal != 0 {
		t.Fatalf("non-matching keyword returned %d tasks", empty.QueryTotal)
	}
	exact := query(common.WorkbenchTaskQuery{IssueRepository: "owner/a", IssueNumber: "17"})
	if exact.QueryTotal != 1 || exact.Items[0].Task.NativeID != "17" || exact.Items[0].Task.ConnectionID != "repo-a" {
		t.Fatalf("repo-scoped exact number returned %+v", exact.Items)
	}
	stable := query(common.WorkbenchTaskQuery{StableSourceID: "stable-170"})
	if stable.QueryTotal != 1 || stable.Items[0].Task.NativeID != "170" {
		t.Fatalf("stable ID match returned %+v", stable.Items)
	}
	text := query(common.WorkbenchTaskQuery{Keyword: "17"})
	if text.QueryTotal != 4 {
		t.Fatalf("literal text search should match issue number substrings and title; got %d", text.QueryTotal)
	}
	combined := query(common.WorkbenchTaskQuery{IssueRepository: "owner/a", IssueNumber: "17", Keyword: "title"})
	if combined.QueryTotal != 0 {
		t.Fatalf("exact number and keyword constraints did not combine: %d", combined.QueryTotal)
	}
}

type queryBoundaryStore struct {
	*dal.Store
	beforeBoundary           func() error
	boundaryReads, fullReads int
}

func (s *queryBoundaryStore) WorkbenchQuerySnapshot(ctx context.Context, boundaryOnly bool) (int64, []common.WorkbenchQueryScope, []common.WorkbenchTask, error) {
	if boundaryOnly {
		s.boundaryReads++
		if s.boundaryReads%2 == 0 && s.beforeBoundary != nil {
			if err := s.beforeBoundary(); err != nil {
				return 0, nil, nil, err
			}
		}
	} else {
		s.fullReads++
	}
	return s.Store.WorkbenchQuerySnapshot(ctx, boundaryOnly)
}

func TestWorkbenchTaskQueryCELCalendarAndDiagnostics(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 9, 3, 59, 59, 0, time.UTC)
	before, after := workbenchQueryCalendar(now, zone), workbenchQueryCalendar(now.Add(time.Second), zone)
	if before["today"] != "2026-03-08" || before["week_start"] != "2026-03-02" ||
		after["today"] != "2026-03-09" || after["next_week_start"] != "2026-03-16" || after["seven_days_end"] != "2026-03-16" {
		t.Fatalf("calendar rollover across DST: %v / %v", before, after)
	}
	if date := queryConvert("date", []string{"2026-03-09T03:59:59Z"}, zone); date.Text == nil || *date.Text != before["today"] {
		t.Fatalf("timestamp projected to wrong calendar day: %+v", date)
	}

	cache := workbenchQueryProgramCache{}
	fields := []common.WorkbenchQueryField{{ID: "estimate-id", Key: "estimate", Name: "Estimate", Type: "number",
		Mappings: map[string]common.WorkbenchQueryMapping{"repo": {Kind: "native", Key: "points"}}}}
	task := common.WorkbenchTask{ID: "test", Kind: "managed-fixture", ConnectionID: "repo", Title: "quoted \"字\\\n🐈",
		State: "open", Labels: []string{"urgent"}, NativeFields: map[string]string{"native.points": `{"type":"number","value":0}`}}
	source := common.WorkbenchConnection{ID: "repo", Name: "Managed"}
	row := projectWorkbenchQueryTask(task, source, workbenchQueryNative(task, WorkbenchConnector{}), fields, zone)
	expression := `task.labels.exists(label, label == "urgent") && has(custom.estimate) && custom.estimate == 0.0 && task.source_id == "repo" && native["points"] == "0" && task.title == ` + fmt.Sprintf("%q", task.Title)
	program, err := cache.compile(expression, fields)
	if err != nil {
		t.Fatal(err)
	}
	if match, err := program.match(context.Background(), row, fields, before); err != nil || !match {
		t.Fatalf("standard macro and typed presence: %t %v", match, err)
	}
	fields[0].Name = "Renamed"
	reused, err := cache.compile(expression, fields)
	if err != nil || reused.task != program.task {
		t.Fatalf("display rename should reuse program: %v", err)
	}
	fields[0].Type = "text"
	if _, err := cache.compile(expression, fields); err == nil {
		t.Fatal("field type change used stale program")
	}
	if _, err := cache.compile("true &&\n custom.undefined", fields); err == nil || common.ProtocolError(err).Details["line"] != 2 {
		t.Fatalf("missing source diagnostics: %v", err)
	}
	if _, err := cache.compile("task.title", fields); err == nil || !strings.Contains(err.Error(), "bool") {
		t.Fatalf("accepted non-boolean CEL: %v", err)
	}
	fields[0].Type = "number"
	for _, expression := range []string{`native["absent"] == ""`, `task.labels.all(a, task.labels.all(b, a == b || a != b))`} {
		program, err := cache.compile(expression, fields)
		if err != nil {
			t.Fatal(err)
		}
		row.item.Fields["task.labels"] = queryRawList(slices.Repeat([]string{"tag"}, 150))
		if _, err := program.match(context.Background(), row, fields, before); err == nil || common.ProtocolError(err).Code != "INVALID_ARGUMENT" {
			t.Fatalf("expected runtime or cost error for %s: %v", expression, err)
		}
	}
	for _, test := range []struct {
		kind string
		raw  []string
		want string
	}{
		{"single_select", []string{"CLI", "Desktop"}, "conflict"},
		{"number", []string{"1.0000001"}, "invalid"},
		{"number", []string{"1000000000000"}, "invalid"},
		{"date", []string{"2026-02-29"}, "invalid"},
		{"text", []string{" "}, "missing"},
	} {
		if value := queryConvert(test.kind, test.raw, zone); value.Reason != test.want {
			t.Errorf("%v: got %q", test, value.Reason)
		}
	}
	if value := queryConvert("multi_select", []string{" CLI ", "cli", "Desktop"}, zone); !slices.Equal(value.Keys, []string{"cli", "desktop"}) ||
		!slices.Equal(value.Values, []string{"CLI", "Desktop"}) {
		t.Fatalf("option keys and display text: %+v", value)
	}
}

func TestWorkbenchTaskQueryFixedCalendarBoundaries(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, beforeUTC, afterUTC string
		before, after             [5]string
	}{
		{"Sunday to Monday after spring forward", "2026-03-09T03:59:59Z", "2026-03-09T04:00:00Z",
			[5]string{"2026-03-08", "2026-03-09", "2026-03-02", "2026-03-09", "2026-03-15"},
			[5]string{"2026-03-09", "2026-03-10", "2026-03-09", "2026-03-16", "2026-03-16"}},
		{"spring forward", "2026-03-08T06:59:59Z", "2026-03-08T07:00:00Z",
			[5]string{"2026-03-08", "2026-03-09", "2026-03-02", "2026-03-09", "2026-03-15"},
			[5]string{"2026-03-08", "2026-03-09", "2026-03-02", "2026-03-09", "2026-03-15"}},
		{"fall back", "2026-11-01T05:59:59Z", "2026-11-01T06:00:00Z",
			[5]string{"2026-11-01", "2026-11-02", "2026-10-26", "2026-11-02", "2026-11-08"},
			[5]string{"2026-11-01", "2026-11-02", "2026-10-26", "2026-11-02", "2026-11-08"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parse := func(value string) time.Time {
				result, err := time.Parse(time.RFC3339, value)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			assertCalendar := func(instant time.Time, want [5]string) {
				t.Helper()
				got := workbenchQueryCalendar(instant, zone)
				for i, name := range workbenchQueryCalendarNames {
					if got[name] != want[i] {
						t.Errorf("%s at %s = %v, want %s", name, instant, got[name], want[i])
					}
				}
			}
			before, after := parse(tc.beforeUTC), parse(tc.afterUTC)
			assertCalendar(before, tc.before)
			assertCalendar(after, tc.after)
			db, err := dal.Open(filepath.Join(t.TempDir(), "query.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			service := &Service{store: db}
			calls := 0
			result, err := service.queryWorkbenchTasks(t.Context(),
				common.WorkbenchTaskQuery{Timezone: zone.String(), Expression: "true"}, func() time.Time {
					calls++
					if calls == 1 {
						return before
					}
					return after
				})
			if calls != 2 {
				t.Fatalf("clock called %d times, want query start and final validation", calls)
			}
			if tc.before[0] != tc.after[0] {
				if err == nil || common.ProtocolError(err).Code != "STALE_REVISION" ||
					!reflect.DeepEqual(result, common.WorkbenchTaskQueryResult{}) {
					t.Fatalf("old-date result published: %+v, %v", result, err)
				}
			} else if err != nil || result.AsOfDate != tc.after[0] || result.PageToken == "" {
				t.Fatalf("same-date DST change invalidated query: %+v, %v", result, err)
			}
		})
	}
}
