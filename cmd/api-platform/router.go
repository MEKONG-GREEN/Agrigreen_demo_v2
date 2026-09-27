package main

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
)

// Deps là phụ thuộc của hai router (xem assembly.Deps).
type Deps = assembly.Deps

// NewRouter dựng Application API (assembly.NewRouter).
func NewRouter(d Deps) *gin.Engine { return assembly.NewRouter(d) }

// NewAdminRouter dựng Admin API, MFA bắt buộc (assembly.NewAdminRouter).
func NewAdminRouter(d Deps) *gin.Engine { return assembly.NewAdminRouter(d) }
