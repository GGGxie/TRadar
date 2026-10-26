package research

import (
	"context"
	"time"

	"tradar/internal/catalog"
)

// Rhythm 是认知库展示的研究节奏：建议值来自研究偏好，进度只统计有时间戳的事实，
// 不把阶段编辑次数当成已完成的研究（SPEC-research-workspace）。
type Rhythm struct {
	TimeZone              string `json:"timeZone"`
	DayStart              string `json:"dayStart"`
	WeekStart             string `json:"weekStart"`
	MonthStart            string `json:"monthStart"`
	DailySaved            int    `json:"dailySaved"`
	WeeklyDeep            int    `json:"weeklyDeep"`
	MonthlyPractice       int    `json:"monthlyPractice"`
	DailySaveTarget       int    `json:"dailySaveTarget"`
	WeeklyDeepTarget      int    `json:"weeklyDeepTarget"`
	MonthlyPracticeTarget int    `json:"monthlyPracticeTarget"`
}

// GetRhythm 统计所选时区的今天、本周（周一开始）和本月：
// 今日收藏 = 今天变为收藏且仍在收藏的项目；本周深研 = 本周创建过 DEEP 分析版本的项目；
// 本月实践 = 本月创建过实践记录的项目。
func GetRhythm(ctx context.Context, q catalog.Querier, now time.Time, location *time.Location) (Rhythm, error) {
	preferences, err := GetPreferences(ctx, q)
	if err != nil {
		return Rhythm{}, err
	}
	local := now.In(location)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	week := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	rhythm := Rhythm{
		TimeZone: location.String(), DayStart: catalog.Timestamp(day), WeekStart: catalog.Timestamp(week), MonthStart: catalog.Timestamp(month),
		DailySaveTarget: preferences.DailySaveTarget, WeeklyDeepTarget: preferences.WeeklyDeepTarget, MonthlyPracticeTarget: preferences.MonthlyPracticeTarget,
	}
	until := catalog.Timestamp(now)
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM research_entries WHERE is_starred=1 AND starred_at>=? AND starred_at<=?`, rhythm.DayStart, until).Scan(&rhythm.DailySaved); err != nil {
		return Rhythm{}, err
	}
	// 规则模板一键生成，不代表做过深入研究；只统计人工或模型参与的六维版本。
	if err = q.QueryRowContext(ctx, `SELECT count(DISTINCT project_id) FROM analyses WHERE level='DEEP' AND origin<>'RULES' AND created_at>=? AND created_at<=?`, rhythm.WeekStart, until).Scan(&rhythm.WeeklyDeep); err != nil {
		return Rhythm{}, err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(DISTINCT project_id) FROM experiments WHERE created_at>=? AND created_at<=?`, rhythm.MonthStart, until).Scan(&rhythm.MonthlyPractice); err != nil {
		return Rhythm{}, err
	}
	return rhythm, nil
}
