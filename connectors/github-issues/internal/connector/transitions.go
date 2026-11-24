package connector

import "github.com/alswl/apilot/connectors/protocol"

func githubActions(issue githubIssue) []protocol.StateAction {
	switch issue.State {
	case "open":
		return []protocol.StateAction{{ID: "close", Label: "Close as completed"}, {ID: "close_not_planned", Label: "Close as not planned"}}
	case "closed":
		return []protocol.StateAction{{ID: "reopen", Label: "Reopen"}}
	}
	return nil
}

func githubTransition(action string) (state, reason string) {
	switch action {
	case "close":
		return "closed", "completed"
	case "close_not_planned":
		return "closed", "not_planned"
	case "reopen":
		return "open", ""
	}
	return "", ""
}
