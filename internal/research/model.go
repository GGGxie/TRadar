package research

type Entry struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"projectId"`
	IsStarred    bool     `json:"isStarred"`
	Tags         []string `json:"tags"`
	Stage        string   `json:"stage"`
	Decision     string   `json:"decision"`
	Rationale    string   `json:"rationale"`
	Notes        string   `json:"notes"`
	NextAction   string   `json:"nextAction"`
	NextActionAt *string  `json:"nextActionAt"`
	Revision     int      `json:"revision"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

type EntryPatch struct {
	IsStarred       *bool     `json:"isStarred"`
	Tags            *[]string `json:"tags"`
	Stage           *string   `json:"stage"`
	Decision        *string   `json:"decision"`
	Rationale       *string   `json:"rationale"`
	Notes           *string   `json:"notes"`
	NextAction      *string   `json:"nextAction"`
	NextActionAt    *string   `json:"nextActionAt"`
	NextActionAtSet bool      `json:"-"`
}

type EntryFilter struct {
	IsStarred      *bool
	Stage          string
	Decision       string
	Tag            string
	Query          string
	Page, PageSize int
}

type Preferences struct {
	Revision              int      `json:"revision"`
	FocusTopics           []string `json:"focusTopics"`
	DailySaveTarget       int      `json:"dailySaveTarget"`
	WeeklyDeepTarget      int      `json:"weeklyDeepTarget"`
	MonthlyPracticeTarget int      `json:"monthlyPracticeTarget"`
}

type PreferencesPatch struct {
	FocusTopics           *[]string `json:"focusTopics"`
	DailySaveTarget       *int      `json:"dailySaveTarget"`
	WeeklyDeepTarget      *int      `json:"weeklyDeepTarget"`
	MonthlyPracticeTarget *int      `json:"monthlyPracticeTarget"`
}
