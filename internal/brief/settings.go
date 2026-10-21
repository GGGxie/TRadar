package brief

import (
	"context"
	"strings"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

func GetSettings(ctx context.Context, q catalog.Querier) (Settings, error) {
	var settings Settings
	err := q.QueryRowContext(ctx, `SELECT revision,is_enabled,time_zone,local_time,max_items FROM brief_settings WHERE singleton=1`).Scan(
		&settings.Revision, &settings.IsEnabled, &settings.TimeZone, &settings.LocalTime, &settings.MaxItems)
	return settings, err
}

func UpdateSettings(ctx context.Context, q catalog.Querier, expected int, patch SettingsPatch) (Settings, error) {
	if patch.IsEnabled == nil && patch.TimeZone == nil && patch.LocalTime == nil && patch.MaxItems == nil {
		return Settings{}, fault.Field("body", "至少提供一个修改字段")
	}
	settings, err := GetSettings(ctx, q)
	if err != nil {
		return Settings{}, err
	}
	if settings.Revision != expected {
		return Settings{}, fault.New("VERSION_CONFLICT", "日报设置已被修改，请加载最新版本")
	}
	if patch.IsEnabled != nil {
		settings.IsEnabled = *patch.IsEnabled
	}
	if patch.TimeZone != nil {
		settings.TimeZone = strings.TrimSpace(*patch.TimeZone)
	}
	if patch.LocalTime != nil {
		settings.LocalTime = strings.TrimSpace(*patch.LocalTime)
	}
	if patch.MaxItems != nil {
		settings.MaxItems = *patch.MaxItems
	}
	if _, err = time.LoadLocation(settings.TimeZone); err != nil || settings.TimeZone == "" {
		return Settings{}, fault.Field("timeZone", "需要有效的 IANA 时区")
	}
	parsed, err := time.Parse("15:04", settings.LocalTime)
	if err != nil || parsed.Format("15:04") != settings.LocalTime {
		return Settings{}, fault.Field("localTime", "需要 HH:mm 时间")
	}
	if settings.MaxItems < 1 || settings.MaxItems > 20 {
		return Settings{}, fault.Field("maxItems", "需要 1–20 的整数")
	}
	settings.Revision++
	result, err := q.ExecContext(ctx, `UPDATE brief_settings SET revision=?,is_enabled=?,time_zone=?,local_time=?,max_items=? WHERE singleton=1 AND revision=?`,
		settings.Revision, settings.IsEnabled, settings.TimeZone, settings.LocalTime, settings.MaxItems, expected)
	if err != nil {
		return Settings{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Settings{}, err
	}
	if changed != 1 {
		return Settings{}, fault.New("VERSION_CONFLICT", "日报设置已被修改，请加载最新版本")
	}
	return settings, nil
}
