package research

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"tradar/internal/catalog"
	"tradar/internal/fault"
)

func GetPreferences(ctx context.Context, q catalog.Querier) (Preferences, error) {
	return scanPreferences(q.QueryRowContext(ctx, `SELECT revision,focus_topics_json,daily_save_target,weekly_deep_target,monthly_practice_target FROM research_preferences WHERE singleton=1`))
}

func UpdatePreferences(ctx context.Context, q catalog.Querier, expected int, patch PreferencesPatch) (Preferences, error) {
	if patch.FocusTopics == nil && patch.DailySaveTarget == nil && patch.WeeklyDeepTarget == nil && patch.MonthlyPracticeTarget == nil {
		return Preferences{}, fault.Field("body", "至少提供一个修改字段")
	}
	preferences, err := GetPreferences(ctx, q)
	if err != nil {
		return Preferences{}, err
	}
	if preferences.Revision != expected {
		return Preferences{}, fault.New("VERSION_CONFLICT", "研究偏好已被修改，请加载最新版本")
	}
	if patch.FocusTopics != nil {
		preferences.FocusTopics = append([]string{}, (*patch.FocusTopics)...)
	}
	if patch.DailySaveTarget != nil {
		preferences.DailySaveTarget = *patch.DailySaveTarget
	}
	if patch.WeeklyDeepTarget != nil {
		preferences.WeeklyDeepTarget = *patch.WeeklyDeepTarget
	}
	if patch.MonthlyPracticeTarget != nil {
		preferences.MonthlyPracticeTarget = *patch.MonthlyPracticeTarget
	}
	if err = validatePreferences(&preferences); err != nil {
		return Preferences{}, err
	}
	preferences.Revision++
	topics, _ := json.Marshal(preferences.FocusTopics)
	result, err := q.ExecContext(ctx, `UPDATE research_preferences SET revision=?,focus_topics_json=?,daily_save_target=?,weekly_deep_target=?,monthly_practice_target=? WHERE singleton=1 AND revision=?`, preferences.Revision, string(topics), preferences.DailySaveTarget, preferences.WeeklyDeepTarget, preferences.MonthlyPracticeTarget, expected)
	if err != nil {
		return Preferences{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Preferences{}, err
	}
	if count == 0 {
		return Preferences{}, fault.New("VERSION_CONFLICT", "研究偏好已被修改，请加载最新版本")
	}
	return preferences, nil
}

func validatePreferences(preferences *Preferences) error {
	topics, err := normalizeFocusTopics(preferences.FocusTopics)
	if err != nil {
		return err
	}
	preferences.FocusTopics = topics
	for field, value := range map[string]int{
		"dailySaveTarget":       preferences.DailySaveTarget,
		"weeklyDeepTarget":      preferences.WeeklyDeepTarget,
		"monthlyPracticeTarget": preferences.MonthlyPracticeTarget,
	} {
		if value < 0 || value > 100 {
			return fault.Field(field, "需要 0–100 的整数")
		}
	}
	return nil
}

func normalizeFocusTopics(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, fault.Field("focusTopics", "关注方向最多 20 项")
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = norm.NFC.String(strings.TrimSpace(value))
		if n := utf8.RuneCountInString(value); n < 1 || n > 50 {
			return nil, fault.Field("focusTopics", "每项关注方向需要 1–50 字")
		}
		duplicate := false
		for _, existing := range result {
			if strings.EqualFold(existing, value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, value)
		}
	}
	return result, nil
}

func scanPreferences(row interface{ Scan(...any) error }) (Preferences, error) {
	var preferences Preferences
	var topics string
	if err := row.Scan(&preferences.Revision, &topics, &preferences.DailySaveTarget, &preferences.WeeklyDeepTarget, &preferences.MonthlyPracticeTarget); err != nil {
		return Preferences{}, err
	}
	if err := json.Unmarshal([]byte(topics), &preferences.FocusTopics); err != nil {
		return Preferences{}, err
	}
	if preferences.FocusTopics == nil {
		preferences.FocusTopics = []string{}
	}
	return preferences, nil
}
