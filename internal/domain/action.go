package domain

import "time"

// Outcomes of an executed item (spec 6.11).
const (
	OutcomeDone   = "done"
	OutcomeFailed = "failed"
	OutcomeDryRun = "dry-run"
)

// Results of one step (spec 6.11).
const (
	ResultDone    = "done"
	ResultFailed  = "failed"
	ResultSkipped = "skipped" // not sent: an earlier step of the item failed
	ResultGone    = "gone"    // the torrent was no longer in the download client
	ResultDryRun  = "dry-run" // dry run: it would have been sent
)

// Action is the history record of one executed plan item (spec 5.5, 6.11).
type Action struct {
	ID      int64        `json:"id"`
	PlanID  string       `json:"planId"`
	At      time.Time    `json:"at"`
	DryRun  bool         `json:"dryRun"`
	Outcome string       `json:"outcome"` // one of the Outcome constants
	Size    int64        `json:"size"`    // the sizes of its rows together
	Rows    []ActionRow  `json:"rows"`
	Steps   []StepResult `json:"steps"` // media manager steps first, then torrents, in the order they ran
}

// ActionRow is one row of an executed item.
type ActionRow struct {
	List   ListKind `json:"list"`
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Season int      `json:"season,omitempty"`
	Size   int64    `json:"size"`
	Added  bool     `json:"added,omitempty"` // not selected: tied to a selected row
}

// StepResult is one step of an executed item: a media manager step or a torrent.
type StepResult struct {
	Manager  *ManagerStep `json:"manager,omitempty"`
	Torrent  *TorrentInfo `json:"torrent,omitempty"`
	KeepData bool         `json:"keepData,omitempty"` // torrent: removed, or would be, without its data
	Result   string       `json:"result"`             // one of the Result constants
	Error    string       `json:"error,omitempty"`
}
