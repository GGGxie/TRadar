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
	// StarredAt 是最近一次变为收藏的时刻；#48 之前的收藏为 null。
	StarredAt *string `json:"starredAt"`
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

type Experiment struct {
	ID                string   `json:"id"`
	ProjectID         string   `json:"projectId"`
	Title             string   `json:"title"`
	Hypothesis        string   `json:"hypothesis"`
	Method            string   `json:"method"`
	Status            string   `json:"status"`
	TimeBudgetMinutes int      `json:"timeBudgetMinutes"`
	ActualMinutes     *int     `json:"actualMinutes"`
	Result            string   `json:"result"`
	Outcome           *string  `json:"outcome"`
	EvidenceIDs       []string `json:"evidenceIds"`
	ArtifactURLs      []string `json:"artifactUrls"`
	NextAction        string   `json:"nextAction"`
	Revision          int      `json:"revision"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

type ExperimentPatch struct {
	Title             *string   `json:"title"`
	Hypothesis        *string   `json:"hypothesis"`
	Method            *string   `json:"method"`
	Status            *string   `json:"status"`
	TimeBudgetMinutes *int      `json:"timeBudgetMinutes"`
	ActualMinutes     *int      `json:"actualMinutes"`
	ActualMinutesSet  bool      `json:"-"`
	Result            *string   `json:"result"`
	Outcome           *string   `json:"outcome"`
	OutcomeSet        bool      `json:"-"`
	EvidenceIDs       *[]string `json:"evidenceIds"`
	ArtifactURLs      *[]string `json:"artifactUrls"`
	NextAction        *string   `json:"nextAction"`
}

type ExperimentFilter struct {
	Status         string
	Page, PageSize int
}
