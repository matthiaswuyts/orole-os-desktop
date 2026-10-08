/** Frontend mirrors for structured Event.data payloads in internal/agents/chat_types.go. */
export interface SourceReference {
  id: string;
  title: string;
  url: string;
  snippet: string;
}

export interface FileData {
  id: string;
  path: string;
  name: string;
  mime: string;
  status: string;
  summary: string;
}

export interface ImageStatusData {
  id: string;
  status: string;
  prompt: string;
  error: string;
}

export interface SubagentData {
  id: string;
  name: string;
  status: string;
  task: string;
  message: string;
  toolCallId: string;
}

export interface GuardrailData {
  id: string;
  kind: string;
  message: string;
  source: string;
}
