package core

const (
	// maxQueryLength is the maximum length of a query string that will be
	// accepted. This is just a safety check to avoid outlandish queries.
	maxQueryLength = 512

	defaultPerPage = 30
	maxPerPage     = 100
)
