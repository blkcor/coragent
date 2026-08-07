package action

import "time"

// PreparedPatch is the validated, side-effect-free result of the patch tool's
// Prepare phase. It carries content identity and a display diff for approval
// and never touches disk.
type PreparedPatch struct {
	RequestID      string
	Path           string
	Target         string
	SourceSHA256   string
	ExpectedSHA256 string
	Diff           string
	DiffDigest     string
	IsSensitive    bool
	CreatedAt      time.Time
}

// PatchID returns the RequestID from Patch when non-nil, otherwise the
// empty string. This is the stable identifier for execute/approval flows.
func (p Prepared) PatchID() string {
	if p.Patch == nil {
		return ""
	}
	return p.Patch.RequestID
}
