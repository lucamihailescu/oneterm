package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

// AccessRequest represents a "four-eyes" approval ticket plus the just-in-time
// access window earned once approved. The same row drives both flows:
//
//   - C1 (approval): a session against an asset with require_approval=true is
//     blocked at connect time until status=Approved.
//   - C3 (JIT): once approved, the request grants access only between
//     ValidFrom and ExpiresAt, and a background sweeper closes any active
//     session whose request has expired.
//
// A request is matched at connect time by (RequesterUid, AssetId, AccountId).
// Multiple historical requests are kept for audit; the current one is the
// most-recently-approved row whose time window covers now.
type AccessRequest struct {
	Id           int    `json:"id" gorm:"column:id;primarykey;autoIncrement"`
	RequesterUid int    `json:"requester_uid" gorm:"column:requester_uid;index:idx_ar_requester,priority:1;not null"`
	AssetId      int    `json:"asset_id" gorm:"column:asset_id;index:idx_ar_asset_account,priority:1;not null"`
	AccountId    int    `json:"account_id" gorm:"column:account_id;index:idx_ar_asset_account,priority:2;not null"`
	Reason       string `json:"reason" gorm:"column:reason;type:text"`

	// Status is the lifecycle of the ticket: pending → approved | rejected | expired.
	Status string `json:"status" gorm:"column:status;size:32;index:idx_ar_status_expires,priority:1;not null;default:pending"`

	// ApproverUid is set when an authorized user resolves the ticket.
	ApproverUid    int        `json:"approver_uid" gorm:"column:approver_uid"`
	ApproverNote   string     `json:"approver_note" gorm:"column:approver_note;type:text"`
	ResolvedAt     *time.Time `json:"resolved_at" gorm:"column:resolved_at"`

	// Time window during which an approved ticket grants access. ValidFrom is
	// usually the approval time; ExpiresAt is mandatory and bounds the JIT
	// window. The sweeper enforces ExpiresAt by force-closing live sessions
	// once it has passed.
	ValidFrom *time.Time `json:"valid_from" gorm:"column:valid_from"`
	ExpiresAt *time.Time `json:"expires_at" gorm:"column:expires_at;index:idx_ar_status_expires,priority:2"`

	// RequestedTTLSeconds is the duration the requester asked for. Approvers
	// may grant a shorter window by overriding ExpiresAt at approval time.
	RequestedTTLSeconds int `json:"requested_ttl_seconds" gorm:"column:requested_ttl_seconds;not null;default:7200"`

	CreatedAt time.Time             `json:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time             `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt soft_delete.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

func (AccessRequest) TableName() string { return "access_request" }

// AccessRequest status constants.
const (
	AccessRequestPending  = "pending"
	AccessRequestApproved = "approved"
	AccessRequestRejected = "rejected"
	// AccessRequestExpired is a terminal status set by the JIT sweeper after
	// ExpiresAt passes for a previously-approved ticket. Distinct from
	// "rejected" so audit can tell denied-by-human from auto-expired.
	AccessRequestExpired = "expired"
)

// IsActive reports whether an approved request currently grants access.
// "now" is passed in so callers can use a deterministic clock in tests.
func (r *AccessRequest) IsActive(now time.Time) bool {
	if r == nil || r.Status != AccessRequestApproved {
		return false
	}
	if r.ValidFrom != nil && now.Before(*r.ValidFrom) {
		return false
	}
	if r.ExpiresAt != nil && !now.Before(*r.ExpiresAt) {
		return false
	}
	return true
}

var DefaultAccessRequest = &AccessRequest{}
