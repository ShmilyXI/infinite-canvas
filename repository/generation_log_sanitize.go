package repository

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
)

const inlineImageStringLimit = 64 * 1024

var inlineImagePrefix = []byte("data:image/")

// SanitizeGenerationLogPayload 清除 payload 里内嵌的超长 base64 图片字符串（data:image/ 开头），
// 其余字段原样保留；不含内嵌图片时零开销直接返回原文。
func SanitizeGenerationLogPayload(raw []byte) []byte {
	if !bytes.Contains(raw, inlineImagePrefix) {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	next, err := json.Marshal(stripInlineImageStrings(value))
	if err != nil {
		return raw
	}
	return next
}

func stripInlineImageStrings(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			item[key] = stripInlineImageStrings(child)
		}
		return item
	case []any:
		for index, child := range item {
			item[index] = stripInlineImageStrings(child)
		}
		return item
	case string:
		if len(item) > inlineImageStringLimit && strings.HasPrefix(item, "data:image/") {
			return ""
		}
		return item
	default:
		return value
	}
}

// MigrateInlineImageGenerationLogs 启动时一次性清洗历史日志里的行内 base64，
// 避免历史列表接口返回数十 MB 的 payload。
func MigrateInlineImageGenerationLogs() error {
	db, err := DB()
	if err != nil {
		return err
	}
	return migrateInlineImageGenerationLogs(db)
}

// migrateInlineImageGenerationLogs 直接接收 *gorm.DB，
// 因为调用点在 DB() 的 sync.Once 初始化内部，不能再回调 DB()（会死锁）。
func migrateInlineImageGenerationLogs(db *gorm.DB) error {
	var logs []model.ImageGenerationLog
	if err := db.Where("length(payload_json) > ? AND payload_json LIKE ?", inlineImageStringLimit, "%data:image/%").Find(&logs).Error; err != nil {
		return err
	}
	for _, log := range logs {
		sanitized := SanitizeGenerationLogPayload([]byte(log.PayloadJSON))
		if len(sanitized) == len(log.PayloadJSON) {
			continue
		}
		if err := db.Model(&model.ImageGenerationLog{}).Where("id = ? AND user_id = ?", log.ID, log.UserID).Update("payload_json", string(sanitized)).Error; err != nil {
			return err
		}
	}
	return nil
}
