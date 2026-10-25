package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
)

// AlertingHandler backs the System Admin alert-receiver editor — where
// firing alerts get routed (Slack webhook / generic webhook), via the
// cluster's AlertmanagerConfig CRD instead of hand-editing it.
type AlertingHandler struct {
	alerting *biz.AlertingBiz
}

func NewAlertingHandler(alerting *biz.AlertingBiz) *AlertingHandler {
	return &AlertingHandler{alerting: alerting}
}

type alertReceiverDTO struct {
	Name   string `json:"name" binding:"required"`
	Type   string `json:"type" binding:"required"`
	Target string `json:"target" binding:"required"`
}

func (h *AlertingHandler) Get(c *gin.Context) {
	receivers, err := h.alerting.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]alertReceiverDTO, 0, len(receivers))
	for _, r := range receivers {
		resp = append(resp, alertReceiverDTO{Name: r.Name, Type: string(r.Type), Target: r.Target})
	}
	c.JSON(http.StatusOK, resp)
}

func (h *AlertingHandler) Set(c *gin.Context) {
	var req []alertReceiverDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	receivers := make([]biz.AlertReceiver, 0, len(req))
	for _, r := range req {
		receivers = append(receivers, biz.AlertReceiver{Name: r.Name, Type: biz.AlertReceiverType(r.Type), Target: r.Target})
	}
	if err := h.alerting.Set(c.Request.Context(), receivers); err != nil {
		if errors.Is(err, biz.ErrInvalidAlertReceiverType) || errors.Is(err, biz.ErrAlertReceiverNameRequired) || errors.Is(err, biz.ErrAlertReceiverTargetRequired) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
