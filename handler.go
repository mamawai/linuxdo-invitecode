package main

import (
	"crypto/subtle"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type InviteHandler struct {
	svc    *InviteService
	redis  *RedisClient
	config *Config
}

func NewInviteHandler(svc *InviteService, redis *RedisClient, cfg *Config) *InviteHandler {
	return &InviteHandler{svc: svc, redis: redis, config: cfg}
}

func (h *InviteHandler) Register(r *gin.Engine) {
	api := r.Group("/api/invite")
	{
		api.GET("/status", h.Status)
		api.POST("/apply", h.Apply)
		api.GET("/verify", h.Verify)
		api.GET("/records", h.Records)
		api.POST("/click", h.Click)
		api.GET("/clicks", h.Clicks)
		api.POST("/upload", h.Upload)
		api.GET("/links", h.Links)

		admin := api.Group("/admin")
		{
			admin.POST("/verify", h.AdminVerify)
			admin.GET("/codes", h.AdminCodes)
		}
	}
}

// Status 库存状态（按 IP 限流：每秒 1 次，容量 1，key 过期 10min）
func (h *InviteHandler) Status(c *gin.Context) {
	ip := c.ClientIP()
	if !h.redis.TokenBucketAllow("limiter:invite_status:"+ip, 30.0/60.0, 30) {
		c.JSON(http.StatusOK, Fail("请求过于频繁，请稍后再试"))
		return
	}
	c.JSON(http.StatusOK, OK(h.svc.HasStock()))
}

// Apply 申请邀请码（按 IP 限流：每 10s 1 次，容量 6，key 过期 10min）
func (h *InviteHandler) Apply(c *gin.Context) {
	ip := c.ClientIP()
	if !h.redis.TokenBucketAllow("limiter:invite_apply:"+ip, 6.0/60.0, 6) {
		c.JSON(http.StatusOK, Fail("请求过于频繁，请稍后再试"))
		return
	}
	email := c.PostForm("email")
	if email == "" {
		c.JSON(http.StatusOK, Fail("邮箱不能为空"))
		return
	}
	if err := h.svc.Apply(email); err != nil {
		c.JSON(http.StatusOK, Fail(err.Error()))
		return
	}
	c.JSON(http.StatusOK, OK(true))
}

// Verify 验证领取
func (h *InviteHandler) Verify(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusOK, Fail("token 不能为空"))
		return
	}
	code, err := h.svc.Verify(token)
	if err != nil {
		c.JSON(http.StatusOK, Fail(err.Error()))
		return
	}
	c.JSON(http.StatusOK, OK(code))
}

// Records 最近领取记录
func (h *InviteHandler) Records(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	c.JSON(http.StatusOK, OK(h.svc.GetRecentRecords(limit)))
}

// Click 点击领取需求（全局限流：每分钟30次，容量30）
func (h *InviteHandler) Click(c *gin.Context) {
	if !h.redis.TokenBucketAllow(LimiterInviteClickGlobalKey, 30.0/60.0, 30) {
		c.JSON(http.StatusOK, Fail("请求过于频繁，请稍后再试"))
		return
	}
	h.svc.Click()
	c.JSON(http.StatusOK, OKVoid())
}

// Clicks 获取点击数
func (h *InviteHandler) Clicks(c *gin.Context) {
	c.JSON(http.StatusOK, OK(h.svc.GetClickCount()))
}

// Links 管理员查询邀请码状态（只返回未领取和 pending 的）
func (h *InviteHandler) Links(c *gin.Context) {
	adminKey := c.GetHeader("X-Admin-Key")
	if subtle.ConstantTimeCompare([]byte(adminKey), []byte(h.config.AdminKey)) != 1 {
		c.JSON(http.StatusOK, Fail("密钥错误"))
		return
	}
	c.JSON(http.StatusOK, OK(h.svc.GetInviteLinks()))
}

// Upload 管理员上传邀请码
func (h *InviteHandler) Upload(c *gin.Context) {
	adminKey := c.GetHeader("X-Admin-Key")
	if subtle.ConstantTimeCompare([]byte(adminKey), []byte(h.config.AdminKey)) != 1 {
		c.JSON(http.StatusOK, Fail("密钥错误"))
		return
	}

	var req UploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, Fail("参数错误: "+err.Error()))
		return
	}

	success, skip, err := h.svc.UploadCodes(req.Codes)
	if err != nil {
		c.JSON(http.StatusOK, Fail("上传失败: "+err.Error()))
		return
	}
	c.JSON(http.StatusOK, OK(map[string]any{
		"success": success,
		"skip":    skip,
	}))
}

// AdminVerify 管理员密钥验证（前端验证通过后跳转管理页面）
func (h *InviteHandler) AdminVerify(c *gin.Context) {
	var req struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, Fail("请输入密钥"))
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Key), []byte(h.config.AdminKey)) != 1 {
		c.JSON(http.StatusOK, Fail("密钥错误"))
		return
	}
	c.JSON(http.StatusOK, OKVoid())
}

// AdminCodes 管理员分页查询所有邀请码
func (h *InviteHandler) AdminCodes(c *gin.Context) {
	adminKey := c.GetHeader("X-Admin-Key")
	if subtle.ConstantTimeCompare([]byte(adminKey), []byte(h.config.AdminKey)) != 1 {
		c.JSON(http.StatusOK, Fail("密钥错误"))
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	c.JSON(http.StatusOK, OK(h.svc.GetAllCodes(page, size)))
}
