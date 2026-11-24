package domain

import "time"

// PlanLifetime is how long a plan can be executed after it was built (spec 6.8, rule 2).
const PlanLifetime = 10 * time.Minute

// The media manager steps of a plan (spec 6.7).
const (
	StepRemoveMovie        = "remove-movie"         // remove the movie with its files
	StepRemoveSeries       = "remove-series"        // remove the series with its files, specials included
	StepDeleteEpisodeFiles = "delete-episode-files" // delete these episode files; the series stays
	StepUnmonitorEpisodes  = "unmonitor-episodes"   // unmonitor these episodes
	StepUnmonitorSeasons   = "unmonitor-seasons"    // unmonitor these seasons
)

// Why a torrent associated with a plan item stays (spec 6.10).
const (
	KeptQueued       = "queued"        // it is in a media manager's queue
	KeptBacksLibrary = "backs-library" // it backs a library file that stays
)

// Plan is the exact set of operations a deletion will perform, built from live data and
// shown for confirmation (spec 6.7, 6.8, 6.10).
type Plan struct {
	ID        string       `json:"id"`
	CreatedAt time.Time    `json:"createdAt"`
	ExpiresAt time.Time    `json:"expiresAt"`
	Items     []PlanItem   `json:"items"`   // in the order they are executed
	Skipped   []SkippedRow `json:"skipped"` // selected rows that are not deleted, with the reason
}

// Expired reports whether the plan can no longer be executed at now.
func (p Plan) Expired(now time.Time) bool { return !now.Before(p.ExpiresAt) }

// PlanItem is what is deleted together: one row, or rows that are tied (spec 6.3). Its
// media manager steps come first, then its torrents (spec 6.8, rule 3).
type PlanItem struct {
	Rows     []PlanRow     `json:"rows"`
	Steps    []ManagerStep `json:"steps"`    // empty for a torrent row
	Torrents []PlanTorrent `json:"torrents"` // removed after the steps, each once per plan
	Kept     []KeptTorrent `json:"kept"`     // associated with the rows, but they stay
}

// PlanRow is one row of an item, as the planner's own read produced it.
type PlanRow struct {
	Row       Candidate `json:"row"`
	Added     bool      `json:"added,omitempty"`     // not selected: tied to a selected row
	AddedWith *RowRef   `json:"addedWith,omitempty"` // the selected row it was added with
	Shares    []string  `json:"shares,omitempty"`    // IDs of the torrents it shares with other rows of the item; none means a shared library file
	NotInScan bool      `json:"notInScan,omitempty"` // the latest scan had no such row: the user has not seen it listed
}

// ManagerStep is one operation in a media manager. ItemID is the movie or the series in
// that manager.
type ManagerStep struct {
	Manager      string `json:"manager"` // "movies" or "series"
	Action       string `json:"action"`  // one of the Step constants
	ItemID       int    `json:"itemId"`
	FileIDs      []int  `json:"fileIds,omitempty"`      // delete-episode-files
	EpisodeIDs   []int  `json:"episodeIds,omitempty"`   // unmonitor-episodes
	Seasons      []int  `json:"seasons,omitempty"`      // unmonitor-seasons
	AddExclusion bool   `json:"addExclusion,omitempty"` // remove-movie and remove-series: add to the import exclusion list
}

// PlanTorrent is a torrent a plan removes.
type PlanTorrent struct {
	Torrent      TorrentInfo `json:"torrent"`
	KeepData     bool        `json:"keepData,omitempty"`     // remove the entry only: another torrent that stays uses the data, or the path is unknown
	LinkedBySize bool        `json:"linkedBySize,omitempty"` // only its file sizes tie it to the item (spec 6.6)
}

// KeptTorrent is a torrent associated with an item's rows that the plan does not remove.
type KeptTorrent struct {
	Torrent TorrentInfo `json:"torrent"`
	Reason  string      `json:"reason"` // KeptQueued or KeptBacksLibrary
}

// SkippedRow is a selected row the plan does not delete.
type SkippedRow struct {
	Row    RowRef `json:"row"`
	Reason string `json:"reason"`
}
