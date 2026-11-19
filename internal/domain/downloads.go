package domain

import "time"

// Download is one item in the download client.
type Download struct {
	ID          string // lower-case identifier; the info hash for torrents
	Name        string
	Size        int64
	State       string // the client's own state name
	Category    string
	Tags        []string
	ContentPath string
	AddedAt     time.Time
	Ratio       *float64       // nil for clients that do not seed
	SeedingTime *time.Duration // nil for clients that do not seed
	Tracker     string         // tracker host name; empty when unknown
}

// DownloadFile is one file inside a download, as the download client reports it.
type DownloadFile struct {
	Name string // path of the file inside the download
	Size int64  // size in bytes
}
