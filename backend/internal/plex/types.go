// Package plex implements a compatibility subset of the Plex Media Server
// API. Plex has no official public spec (it's reverse-engineered by the
// self-hosting community); field names and paths here are taken from Plex's
// own published Go SDK (github.com/LukeHagar/plexgo), which is generated
// from Plex's real OpenAPI definitions.
//
// Plex is structurally different from Jellyfin/Emby in one important way:
// official Plex apps discover servers through plex.tv's cloud (sign in to a
// real Plex account, then plex.tv tells the app which servers that account
// can reach). Vorn is not a Plex-registered server and can't be, so official
// mobile/TV Plex apps cannot be pointed at Vorn out of the box. What's
// implemented here is the *local* PMS-facing protocol -- identity, a
// sign_in.json-shaped auth shim, library sections, item browsing, and direct
// file streaming -- for use by tools/clients that support manually
// configuring a Plex-protocol server + token (common in the self-hosting
// community's tooling) rather than only ever going through plex.tv.
//
// Out of scope for this pass: Plex's transcode-decision/session protocol
// (Vorn always advertises direct play), hubs, search, collections, and any
// actual plex.tv integration.
package plex

// MediaContainer is the root wrapper of every Plex API response. Only one of
// Directory/Metadata is populated per response, depending on endpoint.
type MediaContainer struct {
	Size              int         `json:"size"`
	TotalSize         int         `json:"totalSize,omitempty"`
	Offset            int         `json:"offset"`
	AllowSync         bool        `json:"allowSync"`
	Identifier        string      `json:"identifier,omitempty"`
	MachineIdentifier string      `json:"machineIdentifier,omitempty"`
	Claimed           *bool       `json:"claimed,omitempty"`
	Version           string      `json:"version,omitempty"`
	Directory         []Directory `json:"Directory,omitempty"`
	Metadata          []Metadata  `json:"Metadata,omitempty"`
}

type mediaContainerEnvelope struct {
	MediaContainer MediaContainer `json:"MediaContainer"`
}

// Envelope wraps a MediaContainer the way every Plex JSON response does.
func Envelope(mc MediaContainer) any {
	return mediaContainerEnvelope{MediaContainer: mc}
}

// Directory represents a library section in /library/sections listings.
type Directory struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Type     string `json:"type"` // "movie" | "show"
	Agent    string `json:"agent,omitempty"`
	Language string `json:"language,omitempty"`
	UUID     string `json:"uuid,omitempty"`
}

// Metadata is Plex's single universal item type for movies, shows, seasons,
// and episodes, distinguished by Type.
type Metadata struct {
	RatingKey             string  `json:"ratingKey"`
	Key                   string  `json:"key"`
	ParentRatingKey       string  `json:"parentRatingKey,omitempty"`
	GrandparentRatingKey  string  `json:"grandparentRatingKey,omitempty"`
	Title                 string  `json:"title"`
	ParentTitle           string  `json:"parentTitle,omitempty"`
	GrandparentTitle      string  `json:"grandparentTitle,omitempty"`
	Type                  string  `json:"type"` // "movie" | "show" | "season" | "episode"
	Summary               string  `json:"summary,omitempty"`
	Index                 *int    `json:"index,omitempty"`       // episode number
	ParentIndex           *int    `json:"parentIndex,omitempty"` // season number
	Year                  *int    `json:"year,omitempty"`
	OriginallyAvailableAt string  `json:"originallyAvailableAt,omitempty"` // YYYY-MM-DD
	AddedAt               int64   `json:"addedAt,omitempty"`               // unix seconds
	Duration              int64   `json:"duration,omitempty"`              // milliseconds
	ViewOffset            int64   `json:"viewOffset,omitempty"`            // milliseconds
	ViewCount             int     `json:"viewCount,omitempty"`
	Thumb                 string  `json:"thumb,omitempty"`
	Art                   string  `json:"art,omitempty"`
	Media                 []Media `json:"Media,omitempty"`
}

type Media struct {
	ID         int    `json:"id"`
	Duration   int64  `json:"duration,omitempty"` // milliseconds
	Container  string `json:"container,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Part       []Part `json:"Part"`
}

type Part struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	Duration  int64  `json:"duration,omitempty"`
	Container string `json:"container,omitempty"`
	File      string `json:"file,omitempty"`
}

// SignInUser mimics the shape of plex.tv's users/sign_in.json response
// closely enough for token-oriented tooling: the field clients actually rely
// on is authToken.
type SignInUser struct {
	ID        int    `json:"id"`
	UUID      string `json:"uuid"`
	Username  string `json:"username"`
	Title     string `json:"title"`
	AuthToken string `json:"authToken"`
}

type SignInResponse struct {
	User SignInUser `json:"user"`
}
