package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	TOKENKeyPrefix              = "LINUXDO_INVITE_TOKEN:"
	PendingEmailPrefix          = "LINUXDO_INVITE_PENDING:"
	PendingDBPrefix             = "pending:"
	ApplyLockKey                = "LINUXDO_INVITE_APPLY_LOCK"
	ClickCountKey               = "LINUXDO_INVITE_CLICK_COUNT"
	LimiterInviteClickGlobalKey = "limiter:INVITE_CLICK:global"
	TokenExpireMinutes          = 30
	ReleaseCheckIntervalMinutes = 31
	LockTimeoutSeconds          = 5
)

var emailPattern = regexp.MustCompile(`^\d+@qq\.com$`)

type InviteService struct {
	db     *gorm.DB
	redis  *RedisClient
	email  *EmailSender
	config *Config
}

func NewInviteService(db *gorm.DB, redis *RedisClient, email *EmailSender, cfg *Config) *InviteService {
	return &InviteService{db: db, redis: redis, email: email, config: cfg}
}

// HasStock 是否有库存
func (s *InviteService) HasStock() bool {
	var count int64
	s.db.Model(&LinuxdoInviteCode{}).Where("email IS NULL").Count(&count)
	return count > 0
}

// Apply 申请邀请码
func (s *InviteService) Apply(email string) error {
	email = strings.ToLower(email)
	if !emailPattern.MatchString(email) {
		return fmt.Errorf("仅支持数字QQ邮箱（如 123456@qq.com）")
	}

	// 按邮箱粒度加锁，防止同一邮箱并发绕过检查
	lockKey := ApplyLockKey + ":" + email
	lockValue := s.redis.TryLock(lockKey, LockTimeoutSeconds)
	if lockValue == "" {
		return fmt.Errorf("系统繁忙，请稍后重试")
	}
	defer s.redis.Unlock(lockKey, lockValue)

	var claimed int64
	s.db.Model(&LinuxdoInviteCode{}).Where("email = ?", email).Count(&claimed)
	if claimed > 0 {
		return fmt.Errorf("该邮箱已领取过邀请码")
	}

	pendingKey := PendingEmailPrefix + email
	if s.redis.Exists(pendingKey) {
		return fmt.Errorf("您已申请过，请查收邮件")
	}

	var code LinuxdoInviteCode
	if err := s.db.Where("email IS NULL").Order("id ASC").Limit(1).Find(&code).Error; err != nil || code.ID == 0 {
		return fmt.Errorf("邀请码已发完，请关注后续活动")
	}

	token := randomID()
	pendingEmail := PendingDBPrefix + token

	// 乐观更新：确保 email 仍为 NULL；并发冲突时重试其他码
	now := time.Now()
	var allocated bool
	for range 3 {
		result := s.db.Model(&LinuxdoInviteCode{}).
			Where("id = ? AND email IS NULL", code.ID).
			Updates(map[string]any{"email": pendingEmail, "claimed_at": now})
		if result.RowsAffected > 0 {
			allocated = true
			break
		}
		// 当前码已被占，重新查一条
		code = LinuxdoInviteCode{}
		if err := s.db.Where("email IS NULL").Order("id ASC").Limit(1).Find(&code).Error; err != nil || code.ID == 0 {
			break
		}
	}
	if !allocated {
		return fmt.Errorf("邀请码已发完，请关注后续活动")
	}

	if err := s.redis.Set(TOKENKeyPrefix+token, email, TokenExpireMinutes*time.Minute); err != nil {
		// Redis 写入失败，回滚数据库
		s.db.Model(&LinuxdoInviteCode{}).Where("id = ?", code.ID).Updates(map[string]any{"email": nil, "claimed_at": nil})
		return fmt.Errorf("系统繁忙，请稍后重试")
	}
	if err := s.redis.Set(pendingKey, token, TokenExpireMinutes*time.Minute); err != nil {
		// 回滚：清理已写入的 token 和数据库
		_ = s.redis.Del(TOKENKeyPrefix + token)
		s.db.Model(&LinuxdoInviteCode{}).Where("id = ?", code.ID).Updates(map[string]any{"email": nil, "claimed_at": nil})
		return fmt.Errorf("系统繁忙，请稍后重试")
	}

	// 同步发邮件，失败则回滚 DB+Redis
	if err := s.sendVerifyEmail(email, token); err != nil {
		log.Printf("邀请邮件发送失败，回滚: email=%s, err=%v", email, err)
		_ = s.redis.Del(pendingKey)
		_ = s.redis.Del(TOKENKeyPrefix + token)
		s.db.Model(&LinuxdoInviteCode{}).Where("id = ?", code.ID).Updates(map[string]any{"email": nil, "claimed_at": nil})
		return fmt.Errorf("邮件发送失败，请稍后重试")
	}

	log.Printf("邀请码申请: email=%s, token=%s, codeId=%d", email, token, code.ID)
	return nil
}

// Verify 验证领取邀请码
func (s *InviteService) Verify(token string) (string, error) {
	tokenKey := TOKENKeyPrefix + token
	email := s.redis.Get(tokenKey)
	if email == "" {
		return "", fmt.Errorf("链接已失效或无效，请重新申请")
	}

	pendingEmail := PendingDBPrefix + token
	var code LinuxdoInviteCode
	if err := s.db.Where("email = ?", pendingEmail).First(&code).Error; err != nil {
		return "", fmt.Errorf("邀请码不存在，请重新申请")
	}

	now := time.Now()
	result := s.db.Model(&LinuxdoInviteCode{}).
		Where("id = ? AND email = ?", code.ID, pendingEmail).
		Updates(map[string]any{
			"email":      email,
			"claimed_at": now,
		})
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("领取失败，请重试")
	}

	_ = s.redis.Del(tokenKey)
	_ = s.redis.Del(PendingEmailPrefix + email)

	log.Printf("邀请码领取成功: email=%s, code=%s", email, code.Code)
	return code.Code, nil
}

// GetRecentRecords 最近领取记录
func (s *InviteService) GetRecentRecords(limit int) *LinuxDoInviteRecordsVO {
	limit = max(1, min(limit, 100))

	var records []LinuxdoInviteCode
	s.db.Where("claimed_at IS NOT NULL AND email NOT LIKE ?", PendingDBPrefix+"%").
		Order("claimed_at DESC").
		Limit(limit).
		Find(&records)

	var total int64
	s.db.Model(&LinuxdoInviteCode{}).
		Where("claimed_at IS NOT NULL AND email NOT LIKE ?", PendingDBPrefix+"%").
		Count(&total)

	list := make([]LinuxDoInviteRecordVO, 0, len(records))
	for _, r := range records {
		list = append(list, LinuxDoInviteRecordVO{
			MaskedIdentifier: maskIdentifier(r.Email),
			ClaimedAt:        r.ClaimedAt,
		})
	}

	return &LinuxDoInviteRecordsVO{List: list, Total: total}
}

// Click 点击计数
func (s *InviteService) Click() {
	s.redis.Incr(ClickCountKey, 1)
}

// GetClickCount 获取点击数
func (s *InviteService) GetClickCount() int64 {
	v := s.redis.Get(ClickCountKey)
	if v == "" {
		return 0
	}
	var n int64
	fmt.Sscanf(v, "%d", &n)
	return n
}

// UploadCodes 管理员上传邀请码（去重插入）
func (s *InviteService) UploadCodes(codes []string) (successCount int, skipCount int, err error) {
	// 先规范化输入
	cleaned := make([]string, 0, len(codes))
	seen := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			skipCount++
			continue
		}
		seen[c] = struct{}{}
		cleaned = append(cleaned, c)
	}

	if len(cleaned) == 0 {
		return 0, skipCount, nil
	}

	// 查询已存在的 code
	var existing []string
	s.db.Model(&LinuxdoInviteCode{}).Where("code IN ?", cleaned).Pluck("code", &existing)
	existSet := make(map[string]struct{}, len(existing))
	for _, c := range existing {
		existSet[c] = struct{}{}
	}

	var toInsert []LinuxdoInviteCode
	for _, c := range cleaned {
		if _, ok := existSet[c]; ok {
			skipCount++
			continue
		}
		toInsert = append(toInsert, LinuxdoInviteCode{Code: c})
	}

	if len(toInsert) == 0 {
		return 0, skipCount, nil
	}

	if err := s.db.CreateInBatches(toInsert, 100).Error; err != nil {
		return 0, skipCount, err
	}
	return len(toInsert), skipCount, nil
}

// GetInviteLinks 查询未领取和 pending 状态的邀请码（已领取的不返回）
func (s *InviteService) GetInviteLinks() []InviteLinkVO {
	var codes []LinuxdoInviteCode
	// 只查未领取(email IS NULL)和 pending(email LIKE 'pending:%')
	s.db.Where("email IS NULL OR email LIKE ?", PendingDBPrefix+"%").
		Order("id ASC").Find(&codes)

	var result []InviteLinkVO
	for _, c := range codes {
		if c.Email == nil {
			result = append(result, InviteLinkVO{
				Code:      c.Code,
				Status:    "available",
				CreatedAt: c.CreatedAt,
			})
		} else if strings.HasPrefix(*c.Email, PendingDBPrefix) {
			token := strings.TrimPrefix(*c.Email, PendingDBPrefix)
			email := s.redis.Get(TOKENKeyPrefix + token)
			result = append(result, InviteLinkVO{
				Code:      c.Code,
				Status:    "pending",
				Email:     email,
				CreatedAt: c.CreatedAt,
			})
		}
	}
	return result
}

// GetAllCodes 分页查询所有邀请码（管理员用，含已领取）
func (s *InviteService) GetAllCodes(page, size int) *AdminCodesVO {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var total int64
	s.db.Model(&LinuxdoInviteCode{}).Count(&total)

	var codes []LinuxdoInviteCode
	s.db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&codes)

	list := make([]InviteLinkVO, 0, len(codes))
	for _, c := range codes {
		vo := InviteLinkVO{Code: c.Code, CreatedAt: c.CreatedAt, ClaimedAt: c.ClaimedAt}
		switch {
		case c.Email == nil:
			vo.Status = "available"
		case strings.HasPrefix(*c.Email, PendingDBPrefix):
			// pending 状态，尝试从 Redis 获取真实邮箱
			vo.Status = "pending"
			token := strings.TrimPrefix(*c.Email, PendingDBPrefix)
			vo.Email = s.redis.Get(TOKENKeyPrefix + token)
		default:
			vo.Status = "claimed"
			vo.Email = *c.Email
		}
		list = append(list, vo)
	}

	return &AdminCodesVO{List: list, Total: total, Page: page, Size: size}
}

// StartPendingReleaseWorker 启动后台轮询，定时释放过期的 pending 邀请码
func (s *InviteService) StartPendingReleaseWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("pending 释放 worker 已停止")
				return
			case <-ticker.C:
				s.releaseExpiredPendingCodes()
			}
		}
	}()
}

func (s *InviteService) releaseExpiredPendingCodes() {
	// 用 claimed_at（锁定时间）判断过期，而非 created_at（上传时间）
	cutoff := time.Now().Add(-ReleaseCheckIntervalMinutes * time.Minute)
	result := s.db.Model(&LinuxdoInviteCode{}).
		Where("email LIKE ? AND claimed_at < ?", PendingDBPrefix+"%", cutoff).
		Updates(map[string]any{"email": nil, "claimed_at": nil})
	if result.Error != nil {
		log.Printf("释放过期 pending 邀请码失败: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		log.Printf("释放过期 pending 邀请码: %d 条", result.RowsAffected)
	}
}

func (s *InviteService) sendVerifyEmail(email, token string) error {
	verifyURL := s.config.FrontURL + "?token=" + token
	subject := "LinuxDo 邀请码领取"
	content := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
body { margin: 0; padding: 0; background: #faf8f5; font-family: 'Noto Sans SC', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; }
@media only screen and (max-width: 520px) {
.card-wrap { padding: 20px 12px !important; }
.card-table { border-radius: 12px !important; }
.card-header { padding: 24px 24px !important; }
.card-body { padding: 28px 24px !important; }
.card-footer { padding: 16px 24px !important; }
.card-title { font-size: 20px !important; }
.btn { width: 100%% !important; padding: 14px 24px !important; }
}
</style>
</head>
<body>
<table class="card-wrap" width="100%%" cellpadding="0" cellspacing="0" style="background:#faf8f5;padding:48px 20px;">
<tr><td align="center">
<table class="card-table" width="100%%" cellpadding="0" cellspacing="0" style="background:#fff;border:1px solid #e8e2d9;border-radius:16px;overflow:hidden;box-shadow:0 1px 3px rgba(44,36,22,0.04),0 8px 24px rgba(44,36,22,0.06);width:100%%;max-width:460px;">
<tr><td class="card-header" style="background:#d4622a;padding:32px 40px;text-align:center;">
<h1 class="card-title" style="margin:0;color:#fff;font-size:22px;font-weight:600;letter-spacing:1px;">LinuxDo 邀请码</h1>
</td></tr>
<tr><td class="card-body" style="padding:40px;text-align:center;">
<p style="margin:0 0 10px;color:#2c2416;font-size:15px;line-height:1.7;">您好</p>
<p style="margin:0 0 28px;color:#7a7060;font-size:14px;line-height:1.7;">点击下方按钮，领取你的邀请码</p>
<div style="margin:24px 0;">
<a class="btn" href="%s" style="display:inline-block;background:#d4622a;color:#fff;text-decoration:none;padding:14px 36px;border-radius:10px;font-size:15px;font-weight:500;max-width:240px;">领取邀请码 →</a>
</div>
<p style="margin:24px 0 0;color:#b0a898;font-size:13px;line-height:1.6;">链接有效期 %d 分钟 · 如非本人操作请忽略此邮件</p>
</td></tr>
<tr><td class="card-footer" style="background:#faf8f5;padding:20px 40px;border-top:1px solid #e8e2d9;">
<p style="margin:0;color:#b0a898;font-size:12px;text-align:center;">mawai · linux.do</p>
<p style="margin:6px 0 0;font-size:12px;text-align:center;"><a href="https://github.com/mamawai/linuxdo-invitecode" style="color:#b0a898;text-decoration:underline;">GitHub</a> · 如果有帮助，欢迎给个 ⭐ Star</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`, verifyURL, TokenExpireMinutes)

	if err := s.email.Send(email, subject, content); err != nil {
		return err
	}
	log.Printf("邀请邮件发送成功: email=%s", email)
	return nil
}

func maskIdentifier(email *string) string {
	if email == nil || *email == "" {
		return "***"
	}
	s := *email
	at := strings.Index(s, "@")
	if at < 0 {
		return "***"
	}
	local := s[:at]
	var masked string
	if len(local) <= 3 {
		masked = local + strings.Repeat("*", 3-len(local))
	} else {
		masked = local[:3] + "***"
	}
	return masked + s[at:]
}
