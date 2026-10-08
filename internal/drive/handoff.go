package drive

// ShareRequest identifies the exact reviewed content and selected recipient.
// No caller-supplied file contents or credentials are accepted.
type ShareRequest struct {
	ServerID          string `json:"serverId"`
	FileID            string `json:"fileId"`
	SnapshotHash      string `json:"snapshotHash"`
	SessionID         string `json:"sessionId"`
	RecipientRevision string `json:"recipientRevision"`
	Task              string `json:"task"`
}

type Recipient struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Model    string `json:"model"`
	Live     bool   `json:"live"`
	Revision string `json:"revision"`
}

type ShareResult struct {
	SessionID    string `json:"sessionId"`
	SnapshotHash string `json:"snapshotHash"`
}
