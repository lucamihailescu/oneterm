package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/veops/oneterm/internal/model"
	dbpkg "github.com/veops/oneterm/pkg/db"
)

// IAccessRequestRepository wraps DB access for the access_request table.
// All read methods use small, indexed queries; the (asset_id, account_id) and
// (status, expires_at) composite indexes on the model cover the hot paths
// (connect-time lookup and the JIT sweeper scan).
type IAccessRequestRepository interface {
	Create(ctx context.Context, r *model.AccessRequest) error
	GetById(ctx context.Context, id int) (*model.AccessRequest, error)
	Update(ctx context.Context, r *model.AccessRequest) error

	// FindActiveFor returns the most recent approved request whose time window
	// covers `now` for the given (uid, assetId, accountId). nil + nil means
	// "no active grant" — distinct from a real DB error.
	FindActiveFor(ctx context.Context, uid, assetId, accountId int, now time.Time) (*model.AccessRequest, error)

	// FindExpiredApproved returns approved requests whose ExpiresAt has passed
	// and whose status hasn't yet been flipped to "expired" by the sweeper.
	FindExpiredApproved(ctx context.Context, now time.Time) ([]*model.AccessRequest, error)

	List(ctx context.Context, filter AccessRequestFilter) ([]*model.AccessRequest, int64, error)
}

// AccessRequestFilter configures List(). Zero-valued fields are ignored.
type AccessRequestFilter struct {
	RequesterUid int
	AssetId      int
	AccountId    int
	Status       string
	PageIndex    int
	PageSize     int
}

type accessRequestRepository struct{ db *gorm.DB }

func NewAccessRequestRepository() IAccessRequestRepository {
	return &accessRequestRepository{db: dbpkg.DB}
}

func (r *accessRequestRepository) Create(ctx context.Context, ar *model.AccessRequest) error {
	return r.db.WithContext(ctx).Create(ar).Error
}

func (r *accessRequestRepository) GetById(ctx context.Context, id int) (*model.AccessRequest, error) {
	var ar model.AccessRequest
	if err := r.db.WithContext(ctx).First(&ar, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ar, nil
}

func (r *accessRequestRepository) Update(ctx context.Context, ar *model.AccessRequest) error {
	return r.db.WithContext(ctx).Save(ar).Error
}

func (r *accessRequestRepository) FindActiveFor(ctx context.Context, uid, assetId, accountId int, now time.Time) (*model.AccessRequest, error) {
	var ar model.AccessRequest
	err := r.db.WithContext(ctx).
		Where("requester_uid = ? AND asset_id = ? AND account_id = ?", uid, assetId, accountId).
		Where("status = ?", model.AccessRequestApproved).
		Where("(valid_from IS NULL OR valid_from <= ?)", now).
		Where("(expires_at IS NULL OR expires_at > ?)", now).
		Order("resolved_at DESC").
		First(&ar).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ar, nil
}

func (r *accessRequestRepository) FindExpiredApproved(ctx context.Context, now time.Time) ([]*model.AccessRequest, error) {
	var rows []*model.AccessRequest
	if err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?",
			model.AccessRequestApproved, now).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *accessRequestRepository) List(ctx context.Context, f AccessRequestFilter) ([]*model.AccessRequest, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AccessRequest{})
	if f.RequesterUid != 0 {
		q = q.Where("requester_uid = ?", f.RequesterUid)
	}
	if f.AssetId != 0 {
		q = q.Where("asset_id = ?", f.AssetId)
	}
	if f.AccountId != 0 {
		q = q.Where("account_id = ?", f.AccountId)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count access requests: %w", err)
	}

	if f.PageSize > 0 {
		offset := (f.PageIndex - 1) * f.PageSize
		if offset < 0 {
			offset = 0
		}
		q = q.Limit(f.PageSize).Offset(offset)
	}

	var rows []*model.AccessRequest
	if err := q.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
