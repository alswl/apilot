package common

type WorkbenchGitHubReviewRequest struct {
	Repository string `json:"repository"`
	Number     int64  `json:"number"`
	Title      string `json:"title"`
}

type WorkbenchGitHubReviewRequests struct {
	Items      []WorkbenchGitHubReviewRequest `json:"items"`
	Incomplete bool                           `json:"incomplete"`
}
