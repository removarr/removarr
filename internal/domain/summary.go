package domain

// ListSummary is the outcome of one list in a scan.
type ListSummary struct {
	Candidates int    `json:"candidates"`
	Protected  int    `json:"protected"`
	Unmatched  int    `json:"unmatched"`
	Waiting    int    `json:"waiting"`         // rows held back because a torrent they share also holds something that stays
	Error      string `json:"error,omitempty"` // set when the list could not be produced
}

// ScanSummary is what the header shows about a scan (spec 5.4).
type ScanSummary struct {
	Lists       map[ListKind]ListSummary `json:"lists"`
	RootFolders []RootFolder             `json:"rootFolders"`
}
