package workbench

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/types"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"github.com/alswl/apilot/core/pkg/common"
)

func taskQueryCondition(field common.WorkbenchQueryFieldInfo, operation, raw string) (string, error) {
	factory := ast.NewExprFactory()
	var next int64
	id := func() int64 { next++; return next }
	ident := func(name string) ast.Expr { return factory.NewIdent(id(), name) }
	text := func(value string) ast.Expr { return factory.NewLiteral(id(), types.String(value)) }
	call := func(name string, args ...ast.Expr) ast.Expr { return factory.NewCall(id(), name, args...) }
	namespace := "custom"
	if strings.HasPrefix(field.ID, "task.") {
		namespace = "task"
	}
	var value, present ast.Expr
	switch {
	case strings.HasPrefix(field.ID, "native."):
		value = call(operators.Index, ident("native"), text(field.Key))
		present = call(operators.In, text(field.Key), ident("native"))
	case field.Type == "multi_select":
		value = factory.NewSelect(id(), ident(namespace), field.Key)
		present = call(operators.Greater, call("size", value), factory.NewLiteral(id(), types.Int(0)))
	default:
		value = factory.NewSelect(id(), ident(namespace), field.Key)
		present = factory.NewPresenceTest(id(), ident(namespace), field.Key)
	}
	var condition ast.Expr
	switch operation {
	case "有值":
		condition = present
	case "缺值":
		condition = call(operators.LogicalNot, present)
	case "本周", "未来七天", "逾期":
		switch operation {
		case "本周":
			condition = call(operators.LogicalAnd, call(operators.GreaterEquals, value, ident("week_start")), call(operators.Less, value, ident("next_week_start")))
		case "未来七天":
			condition = call(operators.LogicalAnd, call(operators.GreaterEquals, value, ident("today")), call(operators.Less, value, ident("seven_days_end")))
		default:
			condition = call(operators.Less, value, ident("today"))
		}
		condition = call(operators.LogicalAnd, present, condition)
	default:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "", fmt.Errorf("%s", tr("请输入条件值"))
		}
		constant := func(raw string) (ast.Expr, error) {
			switch field.Type {
			case "number":
				num, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsInf(num, 0) || math.IsNaN(num) {
					return nil, fmt.Errorf("%s", tr("请输入有效数字"))
				}
				return factory.NewLiteral(id(), types.Double(num)), nil
			case "integer":
				num, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					return nil, err
				}
				return factory.NewLiteral(id(), types.Int(num)), nil
			default:
				return text(raw), nil
			}
		}
		literal, err := constant(raw)
		if err != nil {
			return "", err
		}
		switch operation {
		case "包含":
			if field.Type == "multi_select" {
				condition = call(operators.In, literal, value)
			} else {
				condition = factory.NewMemberCall(id(), "contains", value, literal)
			}
		case "任选", "包含全部", "排除":
			var values []ast.Expr
			for part := range strings.SplitSeq(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					values = append(values, text(part))
				}
			}
			if len(values) == 0 {
				return "", fmt.Errorf("%s", tr("请输入条件值"))
			}
			if field.Type == "multi_select" {
				for _, member := range values {
					clause := call(operators.In, member, value)
					if operation == "排除" {
						clause = call(operators.LogicalNot, clause)
					}
					if condition == nil {
						condition = clause
					} else if operation == "任选" {
						condition = call(operators.LogicalOr, condition, clause)
					} else {
						condition = call(operators.LogicalAnd, condition, clause)
					}
				}
			} else {
				condition = call(operators.In, value, factory.NewList(id(), values, nil))
				if operation == "排除" {
					condition = call(operators.LogicalNot, condition)
				}
			}
		default:
			op := map[string]string{"等于": operators.Equals, "不等于": operators.NotEquals, "大于等于": operators.GreaterEquals, "小于": operators.Less, "大于": operators.Greater, "小于等于": operators.LessEquals}[operation]
			condition = call(op, value, literal)
		}
		condition = call(operators.LogicalAnd, present, condition)
	}
	expr, err := ast.ExprToProto(condition)
	if err != nil {
		return "", err
	}
	return cel.AstToString(cel.ParsedExprToAst(&exprpb.ParsedExpr{Expr: expr}))
}

func appendTaskQueryCondition(original, condition string) string {
	if strings.TrimSpace(original) == "" {
		return condition
	}
	return "(" + original + ") && (" + condition + ")"
}

func conditionOperations(field common.WorkbenchQueryFieldInfo) []string {
	if strings.HasPrefix(field.ID, "native.") {
		return []string{"等于", "包含", "有值", "缺值"}
	}
	switch field.Type {
	case "number", "integer":
		return []string{"等于", "不等于", "大于", "大于等于", "小于", "小于等于", "有值", "缺值"}
	case "date":
		return []string{"等于", "不等于", "大于", "大于等于", "小于", "小于等于", "本周", "未来七天", "逾期", "有值", "缺值"}
	case "multi_select":
		return []string{"包含", "任选", "包含全部", "排除", "有值", "缺值"}
	case "single_select", "source":
		return []string{"等于", "不等于", "任选", "排除", "有值", "缺值"}
	default:
		return []string{"包含", "等于", "有值", "缺值"}
	}
}

func (view *taskQueryView) removeCondition(index int) {
	view.Chips = slices.Delete(slices.Clone(view.Chips), index, index+1)
	view.Expression = view.ChipBase
	for _, chip := range view.Chips {
		view.Expression = appendTaskQueryCondition(view.Expression, chip.Expression)
	}
}

func (view *taskQueryView) addCondition(label, expression string) {
	if len(view.Chips) == 0 {
		view.ChipBase = view.Expression
	}
	view.Chips = append(slices.Clone(view.Chips), taskQueryChip{Label: label, Expression: expression})
	view.Expression = appendTaskQueryCondition(view.Expression, expression)
}

func (view *taskQueryView) editExpression(expression string) {
	view.Expression, view.ChipBase, view.Chips = expression, "", nil
}
