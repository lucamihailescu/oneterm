package controller

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"

	"github.com/veops/oneterm/internal/repository"
	"github.com/veops/oneterm/internal/service"
	myErrors "github.com/veops/oneterm/pkg/errors"
)

var accessRequestService = service.NewAccessRequestService()

// CreateAccessRequestBody is the JSON payload for POST /access-request.
type CreateAccessRequestBody struct {
	AssetId    int    `json:"asset_id" binding:"required"`
	AccountId  int    `json:"account_id" binding:"required"`
	Reason     string `json:"reason"`
	TTLSeconds int    `json:"ttl_seconds"`
}

// ApprovalBody is the JSON payload for approve / reject endpoints. Both
// re-use the same shape; "ttl_seconds" is only honored on approve.
type ApprovalBody struct {
	Note       string `json:"note"`
	TTLSeconds int    `json:"ttl_seconds"`
}

// CreateAccessRequest godoc
//
//	@Tags		access-request
//	@Summary	Open a new access request (C1)
//	@Param		body	body	CreateAccessRequestBody	true	"request"
//	@Success	200		{object}	HttpResponse
//	@Router		/access-request [post]
func (c *Controller) CreateAccessRequest(ctx *gin.Context) {
	var body CreateAccessRequestBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.AbortWithError(http.StatusBadRequest, &myErrors.ApiError{Code: myErrors.ErrInvalidArgument, Data: map[string]any{"err": err.Error()}})
		return
	}

	ttl := time.Duration(body.TTLSeconds) * time.Second
	ar, err := accessRequestService.CreateRequest(ctx, body.AssetId, body.AccountId, body.Reason, ttl)
	if err != nil {
		ctx.AbortWithError(http.StatusBadRequest, &myErrors.ApiError{Code: myErrors.ErrInvalidArgument, Data: map[string]any{"err": err.Error()}})
		return
	}
	ctx.JSON(http.StatusOK, NewHttpResponseWithData(ar))
}

// ApproveAccessRequest godoc
//
//	@Tags		access-request
//	@Summary	Approve a pending access request (C1) and start its JIT window (C3)
//	@Param		id		path	int				true	"access request id"
//	@Param		body	body	ApprovalBody	false	"override TTL / note"
//	@Success	200		{object}	HttpResponse
//	@Router		/access-request/{id}/approve [post]
func (c *Controller) ApproveAccessRequest(ctx *gin.Context) {
	id := cast.ToInt(ctx.Param("id"))
	if id == 0 {
		ctx.AbortWithError(http.StatusBadRequest, &myErrors.ApiError{Code: myErrors.ErrInvalidArgument, Data: map[string]any{"err": "invalid id"}})
		return
	}

	var body ApprovalBody
	_ = ctx.ShouldBindJSON(&body) // body is optional

	ttl := time.Duration(body.TTLSeconds) * time.Second
	ar, err := accessRequestService.Approve(ctx, id, ttl, body.Note)
	if err != nil {
		ctx.AbortWithError(http.StatusBadRequest, &myErrors.ApiError{Code: myErrors.ErrInvalidArgument, Data: map[string]any{"err": err.Error()}})
		return
	}
	ctx.JSON(http.StatusOK, NewHttpResponseWithData(ar))
}

// RejectAccessRequest godoc
//
//	@Tags		access-request
//	@Summary	Reject a pending access request (C1)
//	@Param		id		path	int				true	"access request id"
//	@Param		body	body	ApprovalBody	false	"reason note"
//	@Success	200		{object}	HttpResponse
//	@Router		/access-request/{id}/reject [post]
func (c *Controller) RejectAccessRequest(ctx *gin.Context) {
	id := cast.ToInt(ctx.Param("id"))
	if id == 0 {
		ctx.AbortWithError(http.StatusBadRequest, &myErrors.ApiError{Code: myErrors.ErrInvalidArgument, Data: map[string]any{"err": "invalid id"}})
		return
	}

	var body ApprovalBody
	_ = ctx.ShouldBindJSON(&body)

	ar, err := accessRequestService.Reject(ctx, id, body.Note)
	if err != nil {
		ctx.AbortWithError(http.StatusBadRequest, &myErrors.ApiError{Code: myErrors.ErrInvalidArgument, Data: map[string]any{"err": err.Error()}})
		return
	}
	ctx.JSON(http.StatusOK, NewHttpResponseWithData(ar))
}

// GetAccessRequests godoc
//
//	@Tags		access-request
//	@Summary	List access requests (admins see all, users see their own)
//	@Param		status		query	string	false	"pending|approved|rejected|expired"
//	@Param		asset_id	query	int		false	"asset id"
//	@Param		account_id	query	int		false	"account id"
//	@Param		page_index	query	int		false	"page (1-based)"
//	@Param		page_size	query	int		false	"page size"
//	@Success	200			{object}	HttpResponse{data=ListData}
//	@Router		/access-request [get]
func (c *Controller) GetAccessRequests(ctx *gin.Context) {
	filter := repository.AccessRequestFilter{
		AssetId:   cast.ToInt(ctx.Query("asset_id")),
		AccountId: cast.ToInt(ctx.Query("account_id")),
		Status:    ctx.Query("status"),
		PageIndex: cast.ToInt(ctx.DefaultQuery("page_index", "1")),
		PageSize:  cast.ToInt(ctx.DefaultQuery("page_size", "20")),
	}

	rows, total, err := accessRequestService.List(ctx, filter)
	if err != nil {
		ctx.AbortWithError(http.StatusInternalServerError, &myErrors.ApiError{Code: myErrors.ErrInternal, Data: map[string]any{"err": err.Error()}})
		return
	}

	list := make([]any, len(rows))
	for i, r := range rows {
		list[i] = r
	}
	ctx.JSON(http.StatusOK, NewHttpResponseWithData(ListData{Count: total, List: list}))
}
