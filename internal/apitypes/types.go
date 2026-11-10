// Package apitypes holds the JSON shapes the HTTP API speaks. It depends on
// nothing inside the board so a client can vendor it on its own.
package apitypes

import "time"

type Identity struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Project     string `json:"project"`
	TmuxSession string `json:"tmux_session,omitempty"`
	API         string `json:"api"`
}

type Ticket struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Prompt        string    `json:"prompt"`
	Status        string    `json:"status"`
	Priority      string    `json:"priority"`
	Agent         string    `json:"agent,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	BranchMode    string    `json:"branch_mode"`
	WorktreePath  string    `json:"worktree_path,omitempty"`
	PRURL         string    `json:"pr_url,omitempty"`
	Tags          []string  `json:"tags"`
	DependsOn     []string  `json:"depends_on"`
	AgentActive   bool      `json:"agent_active"`
	Activity      string    `json:"activity,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	ResumeCommand string    `json:"resume_command,omitempty"`
	Repo          string    `json:"repo,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Column struct {
	Status  string   `json:"status"`
	Tickets []Ticket `json:"tickets"`
}

type Board struct {
	Columns []Column `json:"columns"`
	Waiting int      `json:"waiting"`
}

type CreateTicket struct {
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Priority string `json:"priority"`
	Agent    string `json:"agent"`
	// Repo is a path inside the repository the ticket belongs to; empty
	// keeps it on this board.
	Repo string `json:"repo,omitempty"`
}

// UpdateTicket is a partial edit: only fields present in the JSON change.
type UpdateTicket struct {
	Title     *string   `json:"title"`
	Prompt    *string   `json:"prompt"`
	Priority  *string   `json:"priority"`
	Agent     *string   `json:"agent"`
	Branch    *string   `json:"branch"`
	Tags      *[]string `json:"tags"`
	DependsOn *[]string `json:"depends_on"`
}

type SetStatus struct {
	Status string `json:"status"`
}

type SetBranchMode struct {
	Mode string `json:"mode"`
}

// SetRepo moves a ticket to the board of the repository containing Repo, or
// back to this board when it is empty. The ticket comes back with a new id.
type SetRepo struct {
	Repo string `json:"repo"`
}

type StartRun struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
}

type Merge struct {
	Strategy string `json:"strategy"`
}

type MergeRequired struct {
	TicketID string `json:"ticket_id"`
	Branch   string `json:"branch"`
	Strategy string `json:"strategy"`
	PRURL    string `json:"pr_url,omitempty"`
}

type MergeResult struct {
	Outcome string `json:"outcome"`
	Branch  string `json:"branch,omitempty"`
	PRURL   string `json:"pr_url,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type FileChange struct {
	Path      string `json:"path"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	Binary    bool   `json:"binary,omitempty"`
	Untracked bool   `json:"untracked,omitempty"`
}

type TicketDiff struct {
	TicketID string       `json:"ticket_id"`
	Base     string       `json:"base,omitempty"`
	Added    int          `json:"added"`
	Deleted  int          `json:"deleted"`
	Files    []FileChange `json:"files"`
	Patch    string       `json:"patch"`
}

type Session struct {
	ID        string    `json:"id"`
	TicketID  string    `json:"ticket_id,omitempty"`
	Agent     string    `json:"agent"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Window    string    `json:"window,omitempty"`
	WindowID  string    `json:"window_id,omitempty"`
	PaneID    string    `json:"pane_id,omitempty"`
	Activity  string    `json:"activity,omitempty"`
	ToolName  string    `json:"tool_name,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Decidable bool      `json:"decidable"`
}

type Input struct {
	Text string `json:"text"`
}

type Resolve struct {
	Decision string `json:"decision"`
}

type Screen struct {
	SessionID string `json:"session_id"`
	Lines     int    `json:"lines"`
	Content   string `json:"content"`
}

type Error struct {
	Error         string         `json:"error"`
	MergeRequired *MergeRequired `json:"merge_required,omitempty"`
}

// Event is one entry on the /events stream. Seq is monotonic per process so
// a client reconnects with ?after=<seq> and misses nothing still buffered.
type Event struct {
	Seq  uint64      `json:"seq"`
	Type string      `json:"type"`
	At   time.Time   `json:"at"`
	Data interface{} `json:"data,omitempty"`
}
