package services

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	pb "google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/alswl/apilot/core/pkg/common"
)

const workbenchQueryEnvironment = "1"

var workbenchQueryCalendarNames = []string{"today", "tomorrow", "week_start", "next_week_start", "seven_days_end"}

type workbenchQueryProgram struct {
	program      cel.Program
	task, custom protoreflect.MessageDescriptor
	dependencies []string
}

type workbenchQueryProgramCache struct {
	mu       sync.Mutex
	programs map[string]workbenchQueryProgram
}

func (cache *workbenchQueryProgramCache) compile(expression string, fields []common.WorkbenchQueryField) (workbenchQueryProgram, error) {
	if strings.TrimSpace(expression) == "" {
		return workbenchQueryProgram{}, nil
	}
	directory := make([][3]string, len(fields))
	for i, field := range fields {
		directory[i] = [3]string{field.Key, field.Type, field.ID}
	}
	slices.SortFunc(directory, func(a, b [3]string) int { return strings.Compare(a[0], b[0]) })
	key, _ := json.Marshal([]any{workbenchQueryEnvironment, expression, directory})
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if program, ok := cache.programs[string(key)]; ok {
		return program, nil
	}
	program, err := compileWorkbenchQuery(expression, directory, fields)
	if err != nil {
		return program, err
	}
	if len(cache.programs) == 64 || cache.programs == nil {
		cache.programs = make(map[string]workbenchQueryProgram)
	}
	cache.programs[string(key)] = program
	return program, nil
}

func compileWorkbenchQuery(expression string, directory [][3]string, fields []common.WorkbenchQueryField) (workbenchQueryProgram, error) {
	task := &pb.DescriptorProto{Name: proto.String("Task")}
	for _, field := range workbenchQueryStandard {
		appendQueryProtoField(task, field.Key, field.Type)
	}
	custom := &pb.DescriptorProto{Name: proto.String("Custom")}
	for _, field := range directory {
		appendQueryProtoField(custom, field[0], field[1])
	}
	file, err := protodesc.NewFile(&pb.FileDescriptorProto{
		Name: proto.String("workbench_query.proto"), Package: proto.String("apilot.query"), Syntax: proto.String("proto2"),
		MessageType: []*pb.DescriptorProto{task, custom},
	}, nil)
	if err != nil {
		return workbenchQueryProgram{}, err
	}
	options := []cel.EnvOption{
		cel.TypeDescs(file),
		cel.Variable("task", cel.ObjectType("apilot.query.Task")),
		cel.Variable("custom", cel.ObjectType("apilot.query.Custom")),
		cel.Variable("native", cel.MapType(cel.StringType, cel.StringType)),
	}
	for _, name := range workbenchQueryCalendarNames {
		options = append(options, cel.Variable(name, cel.StringType))
	}
	env, err := cel.NewEnv(options...)
	if err != nil {
		return workbenchQueryProgram{}, err
	}
	checked, issues := env.Compile(expression)
	if issues.Err() != nil {
		diagnostic := common.Err("INVALID_ARGUMENT", issues.String())
		if entries := issues.Errors(); len(entries) > 0 {
			diagnostic.Details["line"] = entries[0].Location.Line()
			diagnostic.Details["column"] = entries[0].Location.Column() + 1
		}
		return workbenchQueryProgram{}, diagnostic
	}
	if checked.OutputType() != cel.BoolType {
		return workbenchQueryProgram{}, common.Err("INVALID_ARGUMENT", "CEL expression must return bool")
	}
	program, err := env.Program(checked, cel.CostLimit(10000), cel.InterruptCheckFrequency(64))
	if err != nil {
		return workbenchQueryProgram{}, common.Err("INVALID_ARGUMENT", "CEL planning failed: "+err.Error())
	}
	return workbenchQueryProgram{
		program: program, task: file.Messages().ByName("Task"), custom: file.Messages().ByName("Custom"),
		dependencies: workbenchQueryDependencies(checked.NativeRep(), fields),
	}, nil
}

func appendQueryProtoField(message *pb.DescriptorProto, name, kind string) {
	typ, label := pb.FieldDescriptorProto_TYPE_STRING, pb.FieldDescriptorProto_LABEL_OPTIONAL
	switch kind {
	case "multi_select":
		label = pb.FieldDescriptorProto_LABEL_REPEATED
	case "integer":
		typ = pb.FieldDescriptorProto_TYPE_INT64
	case "number":
		typ = pb.FieldDescriptorProto_TYPE_DOUBLE
	}
	message.Field = append(message.Field, &pb.FieldDescriptorProto{
		Name: proto.String(name), JsonName: proto.String(name), Number: proto.Int32(int32(len(message.Field) + 1)), Type: typ.Enum(), Label: label.Enum(),
	})
}

func workbenchQueryDependencies(tree *ast.AST, fields []common.WorkbenchQueryField) []string {
	selections := make(map[int64]string)
	objects := make(map[int64]bool)
	ast.PreOrderVisit(tree.Expr(), ast.NewExprVisitor(func(expr ast.Expr) {
		if expr.Kind() == ast.IdentKind && expr.AsIdent() == "custom" && tree.GetType(expr.ID()).TypeName() == "apilot.query.Custom" {
			objects[expr.ID()] = true
		}
		if expr.Kind() == ast.SelectKind {
			selectExpr := expr.AsSelect()
			selections[selectExpr.Operand().ID()] = selectExpr.FieldName()
		}
	}))
	keys := make(map[string]bool)
	all := false
	for id := range objects {
		key, selected := selections[id]
		all = all || !selected
		keys[key] = true
	}
	dependencies := []string{}
	for _, field := range fields {
		if all || keys[field.Key] {
			dependencies = append(dependencies, field.ID)
		}
	}
	slices.Sort(dependencies)
	return dependencies
}

func (p workbenchQueryProgram) match(ctx context.Context, task queryTask, fields []common.WorkbenchQueryField, input map[string]any) (bool, error) {
	if p.program == nil {
		return true, nil
	}
	taskMessage := dynamicpb.NewMessage(p.task)
	for _, field := range workbenchQueryStandard {
		setQueryProtoValue(taskMessage, field.Key, field.Type, task.item.Fields[field.ID])
	}
	customMessage := dynamicpb.NewMessage(p.custom)
	for _, field := range fields {
		setQueryProtoValue(customMessage, field.Key, field.Type, task.item.Fields[field.ID])
	}
	input["task"], input["custom"], input["native"] = taskMessage, customMessage, task.native
	value, _, err := p.program.ContextEval(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, common.Err("INVALID_ARGUMENT", "CEL evaluation failed: "+err.Error())
	}
	return value == types.True, nil
}

func setQueryProtoValue(message *dynamicpb.Message, name, kind string, value common.WorkbenchQueryValue) {
	if value.Reason != "" {
		return
	}
	field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
	switch kind {
	case "multi_select":
		list := message.Mutable(field).List()
		values := value.Keys
		if values == nil {
			values = value.Values
		}
		for _, item := range values {
			list.Append(protoreflect.ValueOfString(item))
		}
	case "number":
		message.Set(field, protoreflect.ValueOfFloat64(*value.Number))
	case "integer":
		integer, _ := strconv.ParseInt(value.Decimal, 10, 64)
		message.Set(field, protoreflect.ValueOfInt64(integer))
	default:
		text := *value.Text
		if kind == "single_select" || kind == "source" {
			text = value.Key
		}
		message.Set(field, protoreflect.ValueOfString(text))
	}
}
