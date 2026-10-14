package jarvis

// GateInput is the bot's effective permission state; the daemon resolves global fallbacks before calling CheckGate.
type GateInput struct {
	ToolAccess   string // effective: bot override or global (chat.ToolAccess*)
	TaskMode     string // effective code-task permission mode: manual | auto | yolo
	BotYolo      bool   // agents.botYolo
	YoloDisabled bool   // agents.yoloDisabled
	CodeTasks    bool   // BotConfig.CodeTasks
}

type GateError struct{ Reason string }

func (e *GateError) Error() string { return e.Reason }

// CheckGate allows Jarvis only for bots that already act without asking: phone calls have no approval prompt.
func CheckGate(in GateInput) error {
	switch {
	case in.ToolAccess != "all":
		return &GateError{"Jarvis needs this bot's tool access set to All."}
	case !in.CodeTasks:
		return &GateError{"Jarvis needs code tasks turned on for this bot."}
	case in.TaskMode != "yolo":
		return &GateError{"Jarvis needs this bot's code tasks set to YOLO."}
	case in.YoloDisabled:
		return &GateError{"Jarvis needs YOLO mode allowed in Task permissions."}
	case !in.BotYolo:
		return &GateError{"Jarvis needs \"Bots may start YOLO tasks\" turned on in Bot code tasks."}
	}
	return nil
}
