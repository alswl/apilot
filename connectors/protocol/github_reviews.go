package protocol

// GitHubReviewRequests is the bundled GitHub connector's read-only capability.
const GitHubReviewRequests = "github.review_requests"

type GitHubReviewRequest struct {
	Repository string `json:"repository"`
	Number     int64  `json:"number"`
	Title      string `json:"title"`
}

type GitHubReviewRequestsResult struct {
	Items []GitHubReviewRequest `json:"items"`
}
