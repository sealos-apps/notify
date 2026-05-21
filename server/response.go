package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type apiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *apiError   `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	errorCodeBadRequest         = "BAD_REQUEST"
	errorCodeUnauthorized       = "UNAUTHORIZED"
	errorCodeNotFound           = "NOT_FOUND"
	errorCodeInternalServer     = "INTERNAL_SERVER_ERROR"
	errorCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

func respondOK(c *gin.Context, status int, data interface{}) {
	c.JSON(status, apiResponse{
		Success: true,
		Data:    data,
	})
}

func respondNoData(c *gin.Context, status int) {
	c.JSON(status, apiResponse{
		Success: true,
	})
}

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, apiResponse{
		Success: false,
		Error: &apiError{
			Code:    code,
			Message: message,
		},
	})
}

func abortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, apiResponse{
		Success: false,
		Error: &apiError{
			Code:    code,
			Message: message,
		},
	})
}

func internalServerError(c *gin.Context) {
	respondError(c, http.StatusInternalServerError, errorCodeInternalServer, "internal server error")
}
