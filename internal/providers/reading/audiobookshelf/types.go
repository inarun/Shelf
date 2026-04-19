package audiobookshelf

// User is the slice of /api/me the sync layer needs. AB's full response
// contains additional fields (permissions, bookmarks, etc.) — unmapped
// keys are tolerated by encoding/json's default behavior.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Type     string `json:"type"`
}

// ItemsInProgressResponse wraps the libraryItems array returned by
// /api/me/items-in-progress.
type ItemsInProgressResponse struct {
	LibraryItems []LibraryItem `json:"libraryItems"`
}

// LibraryItem captures the fields of an in-progress entry needed for
// timeline matching and progress display.
type LibraryItem struct {
	ID          string       `json:"id"`
	LibraryID   string       `json:"libraryId"`
	Progress    float64      `json:"progress"`
	CurrentTime float64      `json:"currentTime"`
	IsFinished  bool         `json:"isFinished"`
	LastUpdate  int64        `json:"lastUpdate"` // ms since epoch
	Media       LibraryMedia `json:"media"`
}

// LibraryMedia wraps the media sub-object. Audiobookshelf nests
// metadata one layer deeper than flat providers do; keeping the wrapper
// lets the call-site access .Media.Metadata.Title naturally and leaves
// room for .Media.NumTracks, etc., in later sessions.
type LibraryMedia struct {
	Metadata LibraryMediaMetadata `json:"metadata"`
}

// LibraryMediaMetadata mirrors AB's mediaMetadata block. authorName is
// a comma-separated display string; callers that need structured author
// arrays should split on ", " and trim.
type LibraryMediaMetadata struct {
	Title      string `json:"title"`
	AuthorName string `json:"authorName"`
	ASIN       string `json:"asin"`
	ISBN       string `json:"isbn"`
}

// ListeningSessionsResponse wraps a paginated list of sessions.
type ListeningSessionsResponse struct {
	Sessions     []ListeningSession `json:"sessions"`
	Total        int                `json:"total"`
	NumPages     int                `json:"numPages"`
	ItemsPerPage int                `json:"itemsPerPage"`
	CurrentPage  int                `json:"currentPage"`
}

// ListeningSession describes a single play session. StartedAt and
// UpdatedAt are AB's ms-since-epoch timestamps; callers convert via
// time.UnixMilli.
type ListeningSession struct {
	ID            string  `json:"id"`
	UserID        string  `json:"userId"`
	LibraryItemID string  `json:"libraryItemId"`
	DisplayTitle  string  `json:"displayTitle"`
	DisplayAuthor string  `json:"displayAuthor"`
	Duration      float64 `json:"duration"`
	TimeListening float64 `json:"timeListening"`
	StartedAt     int64   `json:"startedAt"`
	UpdatedAt     int64   `json:"updatedAt"`
}
