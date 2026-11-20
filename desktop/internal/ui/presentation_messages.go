package ui

func presentationMessage(code string) string {
	switch code {
	case "environmentVariablesMustUseKeyValue":
		return tr("environmentVariablesMustUseKeyValue")
	case "timeoutMustBePositiveForExample30M":
		return tr("timeoutMustBePositiveForExample30M")
	case "invalidCheckInterval":
		return tr("invalidCheckInterval")
	case "invalidTimeout":
		return tr("invalidTimeout")
	case "invalidDefaultTimeout":
		return tr("invalidDefaultTimeout")
	case "concurrentTasksMustBeAnInteger":
		return tr("concurrentTasksMustBeAnInteger")
	case "retentionDaysMustBeAnInteger":
		return tr("retentionDaysMustBeAnInteger")
	case "logLimitMustBeAnInteger":
		return tr("logLimitMustBeAnInteger")
	case "taskFailed":
		return tr("taskFailed")
	case "observerStateChanged":
		return tr("observerStateChanged")
	case "taskCompleted":
		return tr("taskCompleted")
	}
	return code
}
