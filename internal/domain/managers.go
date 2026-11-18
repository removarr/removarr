package domain

import "time"

// MediaFile is a file in a media manager's library.
type MediaFile struct {
	ID   int
	Size int64
}

// Movie is a movie in the movie manager.
type Movie struct {
	ID    int
	Title string
	Year  int
	IDs   ExternalIDs
	Tags  []string   // tag labels
	File  *MediaFile // nil when the movie has no file
}

// Episode is an episode in the series manager.
type Episode struct {
	ID      int
	Season  int // 0 holds specials
	Number  int
	AirDate *time.Time
	File    *MediaFile // nil when there is no file; the episodes of a multi-episode file share one File.ID
}

// Series is a series in the series manager.
type Series struct {
	ID       int
	Title    string
	Year     int
	IDs      ExternalIDs
	Tags     []string
	Ended    bool
	Episodes []Episode
}

// Links is what a media manager knows about downloads. Download IDs are lower case.
type Links struct {
	// Grabbed maps a download ID to the IDs of the movies or episodes it was grabbed for.
	Grabbed map[string][]int
	// FileSource maps a library file ID to the ID of the download it was imported from.
	FileSource map[int]string
	// Queue holds the IDs of the downloads in the manager's queue.
	Queue map[string]bool
}

// RootFolder is a library folder and its free space, as a media manager reports it.
type RootFolder struct {
	Path      string `json:"path"`
	FreeBytes int64  `json:"freeBytes"`
}

// MovieSnapshot is everything one scan reads from the movie manager.
type MovieSnapshot struct {
	Movies      []Movie
	Links       Links
	RootFolders []RootFolder
}

// SeriesSnapshot is everything one scan reads from the series manager.
type SeriesSnapshot struct {
	Series      []Series
	Links       Links
	RootFolders []RootFolder
}
