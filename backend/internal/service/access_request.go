package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/veops/oneterm/internal/acl"
	"github.com/veops/oneterm/internal/model"
	"github.com/veops/oneterm/internal/repository"
)

// AccessRequestService implements the C1 (approval) and C3 (just-in-time)
// workflows on top of the access_request table.
type AccessRequestService struct {
	repo repository.IAccessRequestRepository
}

func NewAccessRequestService() *AccessRequestService {
	return &AccessRequestService{repo: repository.NewAccessRequestRepository()}
}

// MaxRequestedTTL caps how long a single grant can last. Hard upper bound on
// JIT exposure for compliance.
const MaxRequestedTTL = 8 * time.Hour

// MinRequestedTTL prevents nonsense submissions and accidental zero-windows.
const MinRequestedTTL = 5 * time.Minute

// DefaultRequestedTTL is used when the requester does not specify a duration.
const DefaultRequestedTTL = 2 * time.Hour

// CreateRequest opens a new pending ticket for (assetId, accountId) on behalf
// of the current user. ttl is clamped to [MinRequestedTTL, MaxRequestedTTL].
func (s *AccessRequestService) CreateRequest(ctx context.Context, assetId, accountId int, reason string, ttl time.Duration) (*model.AccessRequest, error) {
	currentUser, err := acl.GetSessionFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if assetId == 0 || accountId == 0 {
		return nil, errors.New("asset_id and account_id are required")
	}
	if ttl <= 0 {
		ttl = DefaultRequestedTTL
	}
	if ttl < MinRequestedTTL {
		ttl = MinRequestedTTL
	}
	if ttl > MaxRequestedTTL {
		ttl = MaxRequestedTTL
	}

	ar := &model.AccessRequest{
		RequesterUid:        currentUser.GetUid(),
		AssetId:             assetId,
		AccountId:           accountId,
		Reason:              reason,
		Status:              model.AccessRequestPending,
		RequestedTTLSeconds: int(ttl / time.Second),
	}
	if err := s.repo.Create(ctx, ar); err != nil {
		return nil, fmt.Errorf("create access request: %w", err)
	}
	return ar, nil
}

// Approve transitions a pending ticket to approved and stamps the time window.
// approverTTL, if non-zero, overrides the requested TTL — approvers can grant
// less time than was asked for, never more than MaxRequestedTTL.
//
// Self-approval is rejected so the four-eyes invariant is enforced even if
// an admin happens to be the requester.
func (s *AccessRequestService) Approve(ctx context.Context, id int, approverTTL time.Duration, note string) (*model.AccessRequest, error) {
	currentUser, err := acl.GetSessionFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	ar, err := s.repo.GetById(ctx, id)
	if err != nil {
		return nil, err
	}
	if ar == nil {
		return nil, errors.New("access request not found")
	}
	if ar.Status != model.AccessRequestPending {
		return nil, fmt.Errorf("request %d is %s, only pending requests may be approved", id, ar.Status)
	}
	if ar.RequesterUid == currentUser.GetUid() {
		return nil, errors.New("self-approval is not allowed")
	}

	ttl := time.Duration(ar.RequestedTTLSeconds) * time.Second
	if approverTTL > 0 && approverTTL < ttl {
		ttl = approverTTL
	}
	if ttl > MaxRequestedTTL {
		ttl = MaxRequestedTTL
	}
	if ttl < MinRequestedTTL {
		ttl = MinRequestedTTL
	}

	now := time.Now()
	expires := now.Add(ttl)
	ar.Status = model.AccessRequestApproved
	ar.ApproverUid = currentUser.GetUid()
	ar.ApproverNote = note
	ar.ResolvedAt = &now
	ar.ValidFrom = &now
	ar.ExpiresAt = &expires

	if err := s.repo.Update(ctx, ar); err != nil {
		return nil, fmt.Errorf("approve access request: %w", err)
	}
	return ar, nil
}

// Reject closes a pending ticket without granting access.
func (s *AccessRequestService) Reject(ctx context.Context, id int, note string) (*model.AccessRequest, error) {
	currentUser, err := acl.GetSessionFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	ar, err := s.repo.GetById(ctx, id)
	if err != nil {
		return nil, err
	}
	if ar == nil {
		return nil, errors.New("access request not found")
	}
	if ar.Status != model.AccessRequestPending {
		return nil, fmt.Errorf("request %d is %s, only pending requests may be rejected", id, ar.Status)
	}
	if ar.RequesterUid == currentUser.GetUid() {
		return nil, errors.New("self-rejection is not allowed")
	}

	now := time.Now()
	ar.Status = model.AccessRequestRejected
	ar.ApproverUid = currentUser.GetUid()
	ar.ApproverNote = note
	ar.ResolvedAt = &now

	if err := s.repo.Update(ctx, ar); err != nil {
		return nil, fmt.Errorf("reject access request: %w", err)
	}
	return ar, nil
}

// HasActiveGrant is the connect-time check used by the SSH/RDP/SFTP entry
// points. Returns nil if access is currently authorized, an error otherwise.
func (s *AccessRequestService) HasActiveGrant(ctx context.Context, uid, assetId, accountId int) (*model.AccessRequest, error) {
	ar, err := s.repo.FindActiveFor(ctx, uid, assetId, accountId, time.Now())
	if err != nil {
		return nil, err
	}
	if ar == nil {
		return nil, errors.New("no active access grant; request and obtain approval first")
	}
	return ar, nil
}

// SweepExpired flips approved-but-past-expiry tickets to "expired" and returns
// them so the caller (the JIT sweeper goroutine) can also force-close any
// live sessions they were authorizing.
func (s *AccessRequestService) SweepExpired(ctx context.Context) ([]*model.AccessRequest, error) {
	now := time.Now()
	expired, err := s.repo.FindExpiredApproved(ctx, now)
	if err != nil {
		return nil, err
	}
	for _, ar := range expired {
		ar.Status = model.AccessRequestExpired
		_ = s.repo.Update(ctx, ar)
	}
	return expired, nil
}

// List returns access requests matching the filter, with total count for
// pagination. Non-admins see only their own requests.
func (s *AccessRequestService) List(ctx context.Context, f repository.AccessRequestFilter) ([]*model.AccessRequest, int64, error) {
	currentUser, err := acl.GetSessionFromCtx(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !acl.IsAdmin(currentUser) {
		f.RequesterUid = currentUser.GetUid()
	}
	return s.repo.List(ctx, f)
}
