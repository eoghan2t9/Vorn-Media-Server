// Package jellyfin implements a compatibility subset of the Jellyfin REST
// API (https://api.jellyfin.org/): enough of its documented surface for a
// real Jellyfin client (official apps, Infuse, Findroid, jellyfin-web) to
// discover the server, authenticate, browse libraries, fetch images, and
// play back media directly against Vorn's own catalog and streaming paths.
//
// In scope: server discovery, AuthenticateByName, library views, item
// browsing/detail, poster/backdrop images, PlaybackInfo (direct play only),
// video streaming, and play-progress reporting. Out of scope for this pass:
// user/library management (Vorn has its own admin API for that), search,
// collections/playlists/favorites, and Jellyfin's HLS transcode-session
// protocol -- transcoding still happens through Vorn's own player, just not
// through a Jellyfin-shaped session negotiation.
package jellyfin

// TicksPerSecond is Jellyfin's tick unit: 100 nanoseconds.
const TicksPerSecond = 10_000_000

func SecondsToTicks(seconds float64) int64 {
	if seconds <= 0 {
		return 0
	}
	return int64(seconds * TicksPerSecond)
}

func TicksToSeconds(ticks int64) float64 {
	return float64(ticks) / TicksPerSecond
}

type PublicSystemInfo struct {
	LocalAddress           string `json:"LocalAddress"`
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	OperatingSystem        string `json:"OperatingSystem"`
	Id                     string `json:"Id"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
}

type UserPolicy struct {
	IsAdministrator  bool `json:"IsAdministrator"`
	IsDisabled       bool `json:"IsDisabled"`
	IsHidden         bool `json:"IsHidden"`
	EnableAllFolders bool `json:"EnableAllFolders"`
}

type UserDto struct {
	Id                    string     `json:"Id"`
	Name                  string     `json:"Name"`
	ServerId              string     `json:"ServerId"`
	HasPassword           bool       `json:"HasPassword"`
	HasConfiguredPassword bool       `json:"HasConfiguredPassword"`
	Policy                UserPolicy `json:"Policy"`
}

type SessionInfo struct {
	Id         string `json:"Id"`
	UserId     string `json:"UserId"`
	UserName   string `json:"UserName"`
	Client     string `json:"Client,omitempty"`
	DeviceName string `json:"DeviceName,omitempty"`
	DeviceId   string `json:"DeviceId,omitempty"`
}

type AuthenticationResult struct {
	User        UserDto     `json:"User"`
	SessionInfo SessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerId    string      `json:"ServerId"`
}

// QueryResult wraps any list response the way every Jellyfin list endpoint
// does, so clients can page (Vorn doesn't paginate internally, so
// StartIndex is always 0 and TotalRecordCount == len(Items)).
type QueryResult[T any] struct {
	Items            []T `json:"Items"`
	TotalRecordCount int `json:"TotalRecordCount"`
	StartIndex       int `json:"StartIndex"`
}

type UserItemData struct {
	PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
	Played                bool  `json:"Played"`
}

// BaseItemDto is Jellyfin's single universal item type -- libraries, movies,
// series, seasons, and episodes are all instances of it, distinguished by
// Type/CollectionType. Only the fields Vorn actually populates are included;
// unknown fields are simply absent rather than null/zero-valued noise.
type BaseItemDto struct {
	Id                string            `json:"Id"`
	ServerId          string            `json:"ServerId,omitempty"`
	Name              string            `json:"Name"`
	SortName          string            `json:"SortName,omitempty"`
	Overview          string            `json:"Overview,omitempty"`
	Type              string            `json:"Type"`
	IsFolder          bool              `json:"IsFolder"`
	ParentId          string            `json:"ParentId,omitempty"`
	CollectionType    string            `json:"CollectionType,omitempty"`
	MediaType         string            `json:"MediaType,omitempty"`
	ProductionYear    *int              `json:"ProductionYear,omitempty"`
	PremiereDate      string            `json:"PremiereDate,omitempty"`
	DateCreated       string            `json:"DateCreated,omitempty"`
	RunTimeTicks      *int64            `json:"RunTimeTicks,omitempty"`
	IndexNumber       *int              `json:"IndexNumber,omitempty"`
	ParentIndexNumber *int              `json:"ParentIndexNumber,omitempty"`
	SeriesName        string            `json:"SeriesName,omitempty"`
	ImageTags         map[string]string `json:"ImageTags,omitempty"`
	BackdropImageTags []string          `json:"BackdropImageTags,omitempty"`
	UserData          *UserItemData     `json:"UserData,omitempty"`
}

type MediaStream struct {
	Type      string `json:"Type"`
	Codec     string `json:"Codec,omitempty"`
	Index     int    `json:"Index"`
	Width     int    `json:"Width,omitempty"`
	Height    int    `json:"Height,omitempty"`
	IsDefault bool   `json:"IsDefault"`
}

// MediaSourceInfo always advertises direct play/stream and never a
// Jellyfin-protocol transcode: Vorn has its own transcode pipeline, and
// replicating Jellyfin's HLS session negotiation is out of scope for this
// compatibility pass (see the package doc comment).
type MediaSourceInfo struct {
	Id                   string        `json:"Id"`
	Protocol             string        `json:"Protocol"`
	Container            string        `json:"Container,omitempty"`
	Name                 string        `json:"Name,omitempty"`
	IsRemote             bool          `json:"IsRemote"`
	RunTimeTicks         *int64        `json:"RunTimeTicks,omitempty"`
	SupportsDirectPlay   bool          `json:"SupportsDirectPlay"`
	SupportsDirectStream bool          `json:"SupportsDirectStream"`
	SupportsTranscoding  bool          `json:"SupportsTranscoding"`
	DirectStreamUrl      string        `json:"DirectStreamUrl,omitempty"`
	MediaStreams         []MediaStream `json:"MediaStreams,omitempty"`
}

type PlaybackInfoResponse struct {
	MediaSources  []MediaSourceInfo `json:"MediaSources"`
	PlaySessionId string            `json:"PlaySessionId"`
}
