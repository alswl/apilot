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
	duringQuery := &Service{store: &queryBoundaryStore{db, func() error {
		return db.InvalidateWorkbenchGitHubAccess(rename, 1)
	}}}
	failingQuery := query
	failingQuery.Expression = `native["absent"] == ""`
	if result, err := duringQuery.queryWorkbenchTasks(ctx, failingQuery, clock); err == nil || common.ProtocolError(err).Code != "STALE_REVISION" ||
		len(result.Items) != 0 || len(result.Sources) != 0 {
		t.Fatalf("permission changed during an invalid query: %v / %+v", err, result)
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

type queryBoundaryStore struct {
	*dal.Store
	beforeBoundary func() error
}

func (s *queryBoundaryStore) WorkbenchQuerySnapshot(ctx context.Context, boundaryOnly bool) (int64, []common.WorkbenchQueryScope, []common.WorkbenchTask, error) {
	if boundaryOnly {
		if err := s.beforeBoundary(); err != nil {
			return 0, nil, nil, err
		}
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
