package events

// Application topics published on the bus. Automations subscribe to them by name, and
// the flow builder lists them through Catalog, so a topic is published only by the
// module that owns the fact and is described here exactly once.
const (
	TopicTaskCreated    = "task.created"
	TopicTaskStatus     = "task.status"
	TopicAgentWaiting   = "agent.waiting"
	TopicAgentFinished  = "agent.finished"
	TopicAgentFailed    = "agent.failed"
	TopicPROpened       = "pr.opened"
	TopicProjectCreated = "project.created"
	TopicBotMessage     = "bot.message"
	TopicAutomationRun  = "automation.run.finished"
	TopicGitHubChanged  = "github.changed"
	TopicMCPToolCalled  = "mcp.tool.called"
)

// TopicField is one key of a topic's Data, available to filters as event.data.<name>.
type TopicField struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // string | number | boolean
	Label string `json:"label"`
}

// Topic describes a published topic for the flow builder.
type Topic struct {
	Topic  string       `json:"topic"`
	Label  string       `json:"label"`
	Group  string       `json:"group"`
	Fields []TopicField `json:"fields"`
}

func str(name, label string) TopicField { return TopicField{Name: name, Type: "string", Label: label} }
func num(name, label string) TopicField { return TopicField{Name: name, Type: "number", Label: label} }
func flag(name, label string) TopicField {
	return TopicField{Name: name, Type: "boolean", Label: label}
}

// taskFields are the Data keys every task.* and agent.* topic carries.
var taskFields = []TopicField{
	str("id", "Task ID"), str("title", "Title"), str("kind", "Harness"), str("status", "Status"),
	str("projectId", "Project ID"), str("project", "Project folder"), str("branch", "Branch"),
	str("activity", "Current activity"), str("lastMessage", "Last agent message"), str("error", "Error"),
	str("prUrl", "Pull request URL"),
}

func withTask(extra ...TopicField) []TopicField {
	return append(append([]TopicField{}, taskFields...), extra...)
}

// Catalog lists every application topic, in display order.
func Catalog() []Topic {
	return []Topic{
		{Topic: TopicTaskCreated, Label: "Task created", Group: "Tasks", Fields: withTask(str("task", "Prompt"))},
		{Topic: TopicTaskStatus, Label: "Task status changed", Group: "Tasks", Fields: withTask(str("from", "Previous status"))},
		{Topic: TopicAgentWaiting, Label: "Agent waiting for you", Group: "Tasks", Fields: withTask(num("pending", "Pending approvals"), num("questions", "Open questions"))},
		{Topic: TopicAgentFinished, Label: "Agent finished", Group: "Tasks", Fields: withTask(str("result", "Result"))},
		{Topic: TopicAgentFailed, Label: "Agent failed", Group: "Tasks", Fields: withTask()},
		{Topic: TopicPROpened, Label: "Pull request opened", Group: "Tasks", Fields: withTask(num("prNumber", "Pull request number"))},
		{Topic: TopicProjectCreated, Label: "Project created", Group: "Projects", Fields: []TopicField{str("id", "Project ID"), str("name", "Name"), str("path", "Folder"), flag("git", "Git repository")}},
		{Topic: TopicBotMessage, Label: "Bot replied", Group: "Bots", Fields: []TopicField{str("botId", "Bot ID"), str("messageId", "Message ID"), str("text", "Reply text")}},
		{Topic: TopicAutomationRun, Label: "Automation finished", Group: "Automations", Fields: []TopicField{str("automationId", "Automation ID"), str("runId", "Run ID"), str("name", "Name"), str("status", "Status")}},
		{Topic: TopicGitHubChanged, Label: "GitHub account changed", Group: "Integrations", Fields: []TopicField{str("reason", "Reason")}},
		{Topic: TopicMCPToolCalled, Label: "MCP tool called", Group: "Integrations", Fields: []TopicField{str("serverId", "Server ID"), str("tool", "Tool"), flag("success", "Succeeded")}},
	}
}
