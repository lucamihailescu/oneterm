package acl

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	"github.com/spf13/cast"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	redis "github.com/veops/oneterm/pkg/cache"
	"github.com/veops/oneterm/pkg/config"
	"github.com/veops/oneterm/pkg/logger"
	"github.com/veops/oneterm/pkg/remote"
)

const (
	kFmtResources = "resource-%s-%d"

	// roleResourcesTTL bounds the staleness of a role's resource list.
	// Short enough that revoked grants stop being honored quickly without
	// requiring explicit cache invalidation; long enough that a
	// page-of-assets render only pays the round-trip to the ACL service
	// once per minute per (role, resource type).
	roleResourcesTTL = time.Minute
)

func GetRoleResources(ctx context.Context, rid int, resourceTypeId string) (res []*Resource, err error) {
	cacheKey := fmt.Sprintf(kFmtResources, resourceTypeId, rid)
	if err = redis.Get(ctx, cacheKey, &res); err == nil {
		return
	}

	token, err := remote.GetAclToken(ctx)
	if err != nil {
		return
	}

	data := &ResourceResult{}
	url := fmt.Sprintf("%s/acl/roles/%d/resources", config.Cfg.Auth.Acl.Url, rid)
	resp, err := remote.RC.R().
		SetHeader("App-Access-Token", token).
		SetQueryParams(map[string]string{
			"app_id":           config.Cfg.Auth.Acl.AppId,
			"resource_type_id": resourceTypeId,
		}).
		SetResult(data).
		Get(url)

	if err = remote.HandleErr(err, resp, func(dt map[string]any) bool { return true }); err != nil {
		return
	}

	res = data.Resources

	if cacheErr := redis.SetEx(ctx, cacheKey, res, roleResourcesTTL); cacheErr != nil {
		logger.L().Debug("acl resource cache write failed",
			zap.String("key", cacheKey), zap.Error(cacheErr))
	}

	return
}

// InvalidateRoleResources removes the cached resource list for a (role,
// resource type) pair. Call after a grant or revoke so callers don't have
// to wait for the TTL.
func InvalidateRoleResources(ctx context.Context, rid int, resourceTypeId string) {
	if redis.RC == nil {
		return
	}
	if err := redis.RC.Del(ctx, fmt.Sprintf(kFmtResources, resourceTypeId, rid)).Err(); err != nil {
		logger.L().Debug("acl resource cache invalidate failed",
			zap.Int("rid", rid), zap.String("type", resourceTypeId), zap.Error(err))
	}
}

// invalidateAllRoleResources removes cached entries for every known resource
// type for a role. Used after grant/revoke when the resource type isn't
// directly known at the call site.
func invalidateAllRoleResources(ctx context.Context, rid int) {
	if redis.RC == nil {
		return
	}
	for _, t := range config.PermResource {
		_ = redis.RC.Del(ctx, fmt.Sprintf(kFmtResources, t, rid)).Err()
	}
	_ = redis.RC.Del(ctx, fmt.Sprintf(kFmtResources, "authorization", rid)).Err()
}

func GetRoleResourceIds(ctx context.Context, rid int, resourceTypeId string) (ids []int, err error) {
	res, err := GetRoleResources(ctx, rid, resourceTypeId)
	if err != nil {
		return
	}

	ids = lo.Map(res, func(r *Resource, _ int) int { return r.ResourceId })
	return
}

func HasPermission(ctx context.Context, rid int, resourceTypeName string, resourceId int, permission string) (res bool, err error) {
	token, err := remote.GetAclToken(ctx)
	if err != nil {
		return false, err
	}

	data := make(map[string]any)
	url := fmt.Sprintf("%s/acl/roles/has_perm", config.Cfg.Auth.Acl.Url)
	resp, err := remote.RC.R().
		SetHeader("App-Access-Token", token).
		SetQueryParams(map[string]string{
			"rid":                cast.ToString(rid),
			"resource_id":        cast.ToString(resourceId),
			"resource_type_name": resourceTypeName,
			"perm":               permission,
		}).
		SetResult(&data).
		Get(url)
	if err = remote.HandleErr(err, resp, func(dt map[string]any) bool { return true }); err != nil {
		return
	}

	if v, ok := data["result"]; ok {
		res = v.(bool)
	}

	return
}

func GrantRoleResource(ctx context.Context, uid int, roleId int, resourceId int, permissions []string) (err error) {
	token, err := remote.GetAclToken(ctx)
	if err != nil {
		return
	}

	url := fmt.Sprintf("%s/acl/roles/%d/resources/%d/grant", config.Cfg.Auth.Acl.Url, roleId, resourceId)
	resp, err := remote.RC.R().
		SetHeaders(map[string]string{
			"App-Access-Token": token,
			"X-User-Id":        cast.ToString(uid)}).
		SetBody(map[string]any{
			"perms": permissions,
		}).
		Post(url)
	err = remote.HandleErr(err, resp, func(dt map[string]any) bool { return true })
	if err == nil {
		invalidateAllRoleResources(ctx, roleId)
	}
	return
}

func RevokeRoleResource(ctx context.Context, uid int, roleId int, resourceId int, permissions []string) (err error) {
	token, err := remote.GetAclToken(ctx)
	if err != nil {
		return
	}

	url := fmt.Sprintf("%s/acl/roles/%d/resources/%d/revoke", config.Cfg.Auth.Acl.Url, roleId, resourceId)
	resp, err := remote.RC.R().
		SetHeaders(map[string]string{
			"App-Access-Token": token,
			"X-User-Id":        cast.ToString(uid)}).
		SetBody(map[string]any{
			"perms": permissions,
		}).
		Post(url)
	err = remote.HandleErr(err, resp, func(dt map[string]any) bool { return true })
	if err == nil {
		invalidateAllRoleResources(ctx, roleId)
	}
	return
}

func BatchGrantRoleResource(ctx context.Context, uid int, roleIds []int, resourceId int, permissions []string) (err error) {
	eg := &errgroup.Group{}
	for _, rid := range roleIds {
		localRid := rid
		eg.Go(func() error {
			return GrantRoleResource(ctx, uid, localRid, resourceId, permissions)
		})
	}
	err = eg.Wait()

	return
}

func BatchRevokeRoleResource(ctx context.Context, uid int, roleIds []int, resourceId int, permissions []string) (err error) {
	eg := &errgroup.Group{}
	for _, rid := range roleIds {
		localRid := rid
		eg.Go(func() error {
			return RevokeRoleResource(ctx, uid, localRid, resourceId, permissions)
		})
	}
	err = eg.Wait()

	return
}
