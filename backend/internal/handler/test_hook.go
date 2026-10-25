package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/pkg/middleware"
)

type TestHookHandler struct {
	hooks *biz.TestHookBiz
}

func NewTestHookHandler(hooks *biz.TestHookBiz) *TestHookHandler {
	return &TestHookHandler{hooks: hooks}
}

// testHookResponse never carries the token — only Create's response does,
// and only once (see createTestHookResponse).
type testHookResponse struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspaceId"`
	Name            string     `json:"name"`
	TestImage       string     `json:"testImage"`
	TestCommand     []string   `json:"testCommand"`
	ImagePullSecret string     `json:"imagePullSecret,omitempty"`
	Revoked         bool       `json:"revoked"`
	LastTriggeredAt *time.Time `json:"lastTriggeredAt,omitempty"`
}

func toTestHookResponse(h *biz.TestHook) testHookResponse {
	return testHookResponse{
		ID: h.ID, WorkspaceID: h.WorkspaceID, Name: h.Name,
		TestImage: h.TestImage, TestCommand: h.TestCommand, ImagePullSecret: h.ImagePullSecret,
		Revoked: h.RevokedAt != nil, LastTriggeredAt: h.LastTriggeredAt,
	}
}

type createTestHookResponse struct {
	testHookResponse
	Token      string `json:"token"`
	TriggerURL string `json:"triggerUrl"`
}

type testRunResponse struct {
	ID           string     `json:"id"`
	TestHookID   string     `json:"testHookId"`
	WorkspaceID  string     `json:"workspaceId"`
	CommitSHA    string     `json:"commitSha,omitempty"`
	ImageTag     string     `json:"imageTag,omitempty"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}

func toTestRunResponse(r *biz.TestRun) testRunResponse {
	return testRunResponse{
		ID: r.ID, TestHookID: r.TestHookID, WorkspaceID: r.WorkspaceID,
		CommitSHA: r.CommitSHA, ImageTag: r.ImageTag, Status: string(r.Status),
		ErrorMessage: r.ErrorMessage, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
	}
}

func triggerURL(c *gin.Context, token string) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/v1/hooks/%s/trigger", scheme, c.Request.Host, token)
}

type createTestHookRequest struct {
	Name            string   `json:"name" binding:"required"`
	TestImage       string   `json:"testImage" binding:"required"`
	TestCommand     []string `json:"testCommand"`
	ImagePullSecret string   `json:"imagePullSecret"`
}

func (h *TestHookHandler) Create(c *gin.Context) {
	var req createTestHookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hook, token, err := h.hooks.Create(c.Request.Context(), c.Param("id"), c.GetString(middleware.CtxUserID), req.Name, req.TestImage, req.TestCommand, req.ImagePullSecret)
	if err != nil {
		writeTestHookError(c, err)
		return
	}
	c.JSON(http.StatusCreated, createTestHookResponse{
		testHookResponse: toTestHookResponse(hook),
		Token:            token,
		TriggerURL:       triggerURL(c, token),
	})
}

func (h *TestHookHandler) List(c *gin.Context) {
	list, err := h.hooks.ListByWorkspace(c.Request.Context(), c.Param("id"), callerTenantID(c))
	if err != nil {
		writeTestHookError(c, err)
		return
	}
	resp := make([]testHookResponse, 0, len(list))
	for _, hook := range list {
		resp = append(resp, toTestHookResponse(hook))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *TestHookHandler) Revoke(c *gin.Context) {
	err := h.hooks.Revoke(c.Request.Context(), c.Param("id"), c.Param("hookId"), c.GetString(middleware.CtxUserID))
	if err != nil {
		writeTestHookError(c, err)
		return
	}
	c.Status(http.StatusOK)
}

func (h *TestHookHandler) ListRuns(c *gin.Context) {
	list, err := h.hooks.ListRuns(c.Request.Context(), c.Param("hookId"), callerTenantID(c))
	if err != nil {
		writeTestHookError(c, err)
		return
	}
	resp := make([]testRunResponse, 0, len(list))
	for _, run := range list {
		resp = append(resp, toTestRunResponse(run))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *TestHookHandler) RunLogs(c *gin.Context) {
	logs, err := h.hooks.RunLogs(c.Request.Context(), c.Param("runId"), callerTenantID(c))
	if err != nil {
		writeTestHookError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

type triggerTestHookRequest struct {
	CommitSHA string `json:"commitSha"`
	ImageTag  string `json:"imageTag"`
}

// Trigger is the only route in this codebase deliberately registered
// outside authMw/Casbin — see router.go. Its security is the opaque
// high-entropy :token (hashed at rest), not a session. Both "no such token"
// and "revoked" map to the same generic 404 below (see TestHookBiz.Trigger's
// comment) so a leaked/guessed token can't be used to probe revocation
// status via the response.
func (h *TestHookHandler) Trigger(c *gin.Context) {
	var req triggerTestHookRequest
	_ = c.ShouldBindJSON(&req) // commitSha/imageTag are optional metadata; a missing/empty body shouldn't block the trigger

	run, err := h.hooks.Trigger(c.Request.Context(), c.Param("token"), req.CommitSHA, req.ImageTag)
	if err != nil {
		if errors.Is(err, biz.ErrTestHookNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"runId": run.ID, "status": run.Status})
}

func writeTestHookError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, biz.ErrNotWorkspaceOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, biz.ErrTestHookNotFound), errors.Is(err, biz.ErrTestRunNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
